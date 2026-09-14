package mailbox

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/emersion/go-imap/v2"
)

func TestStripHTML(t *testing.T) {
	html := `<html><head><style>p{color:red}</style></head>
<body><p>第一段</p><div>第二段</div><script>alert(1)</script>
<a href="http://x">链接</a>&nbsp;&amp;结尾</body></html>`
	got := stripHTML(html)
	for _, want := range []string{"第一段", "第二段", "链接", "&结尾"} {
		if !strings.Contains(got, want) {
			t.Errorf("剥标签后丢了 %q，结果: %q", want, got)
		}
	}
	for _, bad := range []string{"color:red", "alert(1)", "<p>", "href"} {
		if strings.Contains(got, bad) {
			t.Errorf("剥标签后残留 %q，结果: %q", bad, got)
		}
	}
}

// 摘要是按字节截断取回的，base64 尾部几乎必然残缺，不能整段解码失败。
func TestDecodeBase64PrefixHandlesTruncation(t *testing.T) {
	full := base64.StdEncoding.EncodeToString([]byte("这是一封很长的中文邮件正文，用来验证截断处理。"))
	for _, cut := range []int{len(full), len(full) - 1, len(full) - 3, len(full) / 2} {
		got := decodeBase64Prefix([]byte(full[:cut]))
		if len(got) == 0 {
			t.Errorf("截到 %d 字节时解出空内容", cut)
			continue
		}
		if !strings.HasPrefix("这是一封很长的中文邮件正文，用来验证截断处理。", strings.TrimRight(string(got), "\x00")) &&
			!strings.HasPrefix(string(got), "这是") {
			t.Errorf("截到 %d 字节时解出的内容不对: %q", cut, got)
		}
	}
}

// 截断绝不能把一个多字节汉字切成半个。
func TestCleanKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("中文内容", 500)
	for _, max := range []int{10, 11, 12, 13, 100, 1200} {
		got := clean(long, max)
		if !utf8.ValidString(got) {
			t.Errorf("max=%d 时截出了非法 UTF-8: %q", max, got)
		}
	}
}

func TestCleanCollapsesWhitespace(t *testing.T) {
	got := clean("  第一行  \n\n\n\n   第二行\t\t尾巴  \n\n", 0)
	want := "第一行\n第二行 尾巴"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDecodeWordHandlesEncodedSubject(t *testing.T) {
	// RFC 2047，UTF-8 base64 编码的中文主题。
	in := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("项目评审通知")) + "?="
	if got := decodeWord(in); got != "项目评审通知" {
		t.Errorf("got %q, want 项目评审通知", got)
	}
	// 没编码的原样返回。
	if got := decodeWord("plain subject"); got != "plain subject" {
		t.Errorf("got %q", got)
	}
}

func TestPickTextPartPrefersPlainAndSkipsAttachments(t *testing.T) {
	plain := &imap.BodyStructureSinglePart{Type: "text", Subtype: "plain"}
	html := &imap.BodyStructureSinglePart{Type: "text", Subtype: "html"}
	pdf := &imap.BodyStructureSinglePart{Type: "application", Subtype: "pdf"}

	// multipart/mixed{ multipart/alternative{plain, html}, pdf }
	alt := &imap.BodyStructureMultiPart{Subtype: "alternative", Children: []imap.BodyStructure{html, plain}}
	mixed := &imap.BodyStructureMultiPart{Subtype: "mixed", Children: []imap.BodyStructure{alt, pdf}}

	part, path := pickTextPart(mixed)
	if part != plain {
		t.Fatalf("应当选中 text/plain，实际选了 %v", part)
	}
	if len(path) != 2 || path[0] != 1 || path[1] != 2 {
		t.Errorf("路径应为 [1 2]，实际 %v", path)
	}

	// 只有 html 时退而求其次。
	onlyHTML := &imap.BodyStructureMultiPart{Subtype: "mixed", Children: []imap.BodyStructure{html, pdf}}
	if part, _ := pickTextPart(onlyHTML); part != html {
		t.Error("没有 plain 时应选 html")
	}

	// 整封就是一段纯文本时，路径为空表示用 BODY[TEXT]。
	if part, path := pickTextPart(plain); part != plain || len(path) != 0 {
		t.Errorf("单段正文应返回空路径，实际 part=%v path=%v", part, path)
	}
}
