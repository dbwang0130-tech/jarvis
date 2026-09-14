package mailbox

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"net/textproto"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
)

var wordDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

// decodeWord 解开 RFC 2047 编码的标题/发件人名，顺带支持 GBK 等国内常见字符集。
func decodeWord(s string) string {
	if s == "" || !strings.Contains(s, "=?") {
		return s
	}
	if out, err := wordDecoder.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// parseBulkHeaders 从 HEADER.FIELDS 的原始字节里读出群发信号。
func parseBulkHeaders(m *Message, raw []byte) {
	if len(raw) == 0 {
		return
	}
	// ReadMIMEHeader 需要以空行结尾，补一个保证最后一个头被读出。
	if !bytes.HasSuffix(raw, []byte("\n\n")) {
		raw = append(append([]byte{}, raw...), '\n')
	}
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	hdr, err := r.ReadMIMEHeader()
	if err != nil && len(hdr) == 0 {
		return
	}
	if hdr.Get("List-Unsubscribe") != "" {
		m.ListUnsubscribe = true
	}
	if v := hdr.Get("List-Id"); v != "" {
		m.ListID = decodeWord(v)
	}
	if v := hdr.Get("Auto-Submitted"); v != "" && !strings.EqualFold(v, "no") {
		m.AutoSubmitted = true
	}
	m.Precedence = hdr.Get("Precedence")
}

// pickTextPart 在正文结构里挑一个用来做摘要的段：优先 text/plain，其次 text/html。
// 返回的路径为空表示整封就是单段正文，取 BODY[TEXT] 即可。
func pickTextPart(bs imap.BodyStructure) (*imap.BodyStructureSinglePart, []int) {
	if sp, ok := bs.(*imap.BodyStructureSinglePart); ok {
		if strings.EqualFold(sp.Type, "text") {
			return sp, nil
		}
		return nil, nil
	}
	mp, ok := bs.(*imap.BodyStructureMultiPart)
	if !ok {
		return nil, nil
	}

	var plain, html *imap.BodyStructureSinglePart
	var plainPath, htmlPath []int

	var walk func(node imap.BodyStructure, prefix []int)
	walk = func(node imap.BodyStructure, prefix []int) {
		switch n := node.(type) {
		case *imap.BodyStructureMultiPart:
			for i, child := range n.Children {
				path := append(append([]int{}, prefix...), i+1)
				walk(child, path)
			}
		case *imap.BodyStructureSinglePart:
			if d := n.Disposition(); d != nil && strings.EqualFold(d.Value, "attachment") {
				return
			}
			if !strings.EqualFold(n.Type, "text") {
				return
			}
			switch {
			case strings.EqualFold(n.Subtype, "plain") && plain == nil:
				plain, plainPath = n, prefix
			case strings.EqualFold(n.Subtype, "html") && html == nil:
				html, htmlPath = n, prefix
			}
		}
	}
	walk(mp, nil)

	if plain != nil {
		return plain, plainPath
	}
	return html, htmlPath
}

// decodeText 把取回的原始片段还原成可读纯文本。
// 片段是按字节截断的，解码器遇到残缺结尾时保留已经解出的部分即可。
func decodeText(raw []byte, part *imap.BodyStructureSinglePart, max int) string {
	if len(raw) == 0 {
		return ""
	}
	data := raw
	if part != nil {
		switch strings.ToLower(part.Encoding) {
		case "base64":
			data = decodeBase64Prefix(raw)
		case "quoted-printable":
			var buf bytes.Buffer
			io.Copy(&buf, quotedprintable.NewReader(bytes.NewReader(raw)))
			data = buf.Bytes()
		}
	}

	// 非 UTF-8 的正文（国内邮箱常见 GB2312/GBK）转过来。
	if part != nil {
		if cs := part.Params["charset"]; cs != "" && !strings.EqualFold(cs, "utf-8") {
			if r, err := charset.Reader(cs, bytes.NewReader(data)); err == nil {
				if out, err := io.ReadAll(r); err == nil && len(out) > 0 {
					data = out
				}
			}
		}
	}

	text := string(data)
	if part != nil && strings.EqualFold(part.Subtype, "html") {
		text = stripHTML(text)
	}
	return clean(text, max)
}

// decodeBase64Prefix 解码一段可能被截断的 base64：去掉换行后按 4 字节对齐截断。
func decodeBase64Prefix(raw []byte) []byte {
	compact := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
			compact = append(compact, b)
		}
	}
	compact = compact[:len(compact)-len(compact)%4]
	out := make([]byte, base64.StdEncoding.DecodedLen(len(compact)))
	n, err := base64.StdEncoding.Decode(out, compact)
	if err != nil && n == 0 {
		return raw
	}
	return out[:n]
}

var (
	// Go 的 RE2 没有反向引用，三种标签分别写。
	reStyleScript = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>|<head[^>]*>.*?</head>`)
	reBreak       = regexp.MustCompile(`(?i)<br[^>]*>|</p>|</div>|</tr>|</h[1-6]>`)
	reTag         = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace       = regexp.MustCompile(`[ \t\x{00a0}]+`)
	reBlankLines  = regexp.MustCompile(`\n{2,}`)
)

// stripHTML 把 HTML 正文压成纯文本，够模型判断即可，不求还原排版。
func stripHTML(s string) string {
	s = reStyleScript.ReplaceAllString(s, " ")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, " ")
	return htmlUnescape(s)
}

var htmlEntities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	"&quot;", `"`, "&#39;", "'", "&apos;", "'", "&mdash;", "—", "&ndash;", "–",
	"&hellip;", "…", "&ldquo;", "“", "&rdquo;", "”", "&middot;", "·",
)

func htmlUnescape(s string) string { return htmlEntities.Replace(s) }

// clean 收敛空白并截到一个够用的长度，同时保证不在多字节字符中间切断。
func clean(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = reSpace.ReplaceAllString(s, " ")

	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	s = strings.Join(lines, "\n")
	s = reBlankLines.ReplaceAllString(s, "\n")
	s = strings.TrimSpace(s)

	if max > 0 && len(s) > max {
		s = s[:max]
		for !utf8.ValidString(s) && len(s) > 0 {
			s = s[:len(s)-1]
		}
		s += "…"
	}
	return s
}

// extractFromRawMIME 从整封邮件的原始字节里挖出可读正文。
// 服务器的 BODYSTRUCTURE 用不了时走这条路：自己走一遍 MIME 树。
// 传进来的字节通常是被截断的，解析中途出错时保留已经拿到的部分。
func extractFromRawMIME(raw []byte, max int) string {
	if len(raw) == 0 {
		return ""
	}
	ent, err := message.Read(bytes.NewReader(raw))
	if ent == nil {
		return ""
	}
	// 头读出来就够用了；正文被截断导致的错误不致命，继续解析已有部分。
	_ = err
	text, _ := extractEntity(ent, 0)
	return clean(text, max)
}

// maxMIMEDepth 防住畸形邮件里无限嵌套的 multipart。
const maxMIMEDepth = 8

// extractEntity 返回这个节点里的可读文本，以及它是不是 text/plain。
// text/plain 优先于 text/html。
func extractEntity(ent *message.Entity, depth int) (text string, isPlain bool) {
	if depth > maxMIMEDepth {
		return "", false
	}

	if mr := ent.MultipartReader(); mr != nil {
		var html string
		for {
			part, err := mr.NextPart()
			if err != nil {
				break // 截断或畸形，用已经取到的内容
			}
			got, plain := extractEntity(part, depth+1)
			if got == "" {
				continue
			}
			if plain {
				return got, true
			}
			if html == "" {
				html = got
			}
		}
		return html, false
	}

	ct, _, _ := ent.Header.ContentType()
	if !strings.HasPrefix(ct, "text/") {
		return "", false
	}
	if disp, _, _ := ent.Header.ContentDisposition(); strings.EqualFold(disp, "attachment") {
		return "", false
	}

	// Entity.Body 已经解过 transfer encoding 和字符集。
	b, _ := io.ReadAll(io.LimitReader(ent.Body, fullBytes))
	if len(b) == 0 {
		return "", false
	}
	s := string(b)
	if ct == "text/html" {
		s = stripHTML(s)
	}
	return s, ct == "text/plain"
}
