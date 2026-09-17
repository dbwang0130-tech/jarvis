package mailbox

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/emersion/go-message"
)

// FullBody 是一封邮件适合**展示给人看**的原文。
//
// 跟 Message.Snippet 不是一回事：那个是压成纯文本喂给模型判断的，HTML 标签
// 全被剥掉。人要看的是原来的排版、表格、图片——尤其账单、验证码、活动通知
// 这类信，剥完就只剩一堆散字。
type FullBody struct {
	// HTML 是 text/html 部分，内联图片已经换成 data: URI。没有就是空。
	HTML string `json:"html"`
	// Text 是 text/plain 部分。两者可能都有，界面优先用 HTML。
	Text string `json:"text"`
	// Attachments 是附件清单。正文里不显示，但得让人知道有这些东西。
	Attachments []Attachment `json:"attachments"`
	// InlineDropped 说明有内联图片因为太大没嵌进来。
	InlineDropped int `json:"inline_dropped"`
}

// Attachment 是一个附件的信息。内容不带出来——几 MB 的东西过一次 JSON
// 桥没有意义，真要看再单独取。
type Attachment struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

// 内联图片嵌成 data: URI 会把体积撑到原来的四三倍（base64）。
// 整封封顶 8MB，单张封顶 2MB——超过的多半是附件被写成了内联，
// 嵌进去只会让界面卡住。
const (
	maxInlineTotal = 8 << 20
	maxInlineOne   = 2 << 20
)

// ParseBody 把整封原始邮件解析成能展示的样子。
func ParseBody(raw []byte) (*FullBody, error) {
	ent, err := message.Read(bytes.NewReader(raw))
	if ent == nil {
		return nil, fmt.Errorf("这封邮件解析不了：%w", err)
	}

	c := &collector{inline: map[string]string{}}
	c.walk(ent, 0)

	// HTML 里的 cid: 引用换成 data: URI。换不掉的留着也无妨——
	// 界面那边拦着不让它去联网取远程图片，一个取不到的 cid 只是个空框。
	for cid, uri := range c.inline {
		for _, form := range []string{"cid:" + cid, "cid:%3C" + cid + "%3E"} {
			c.out.HTML = strings.ReplaceAll(c.out.HTML, form, uri)
		}
	}
	return &c.out, nil
}

// collector 是解析过程中攒结果的地方。
//
// 收进一个结构体而不是三个参数手手相传：内联图的总字节数要跨递归共享，
// 写成 `used *int` 那种取地址的形式，多一个人读就多一次「这是干嘛的」。
type collector struct {
	out    FullBody
	inline map[string]string // Content-ID → data: URI
	used   int
}

func (c *collector) walk(ent *message.Entity, depth int) {
	// 深度上限跟解析摘要那条路共用同一个常量。畸形邮件能嵌套到爆栈，
	// 而两条路各设各的上限，哪天有人调了一个、另一个不跟，就是个只在
	// 特定邮件上复现的崩溃。
	if depth > maxMIMEDepth {
		return
	}
	mr := ent.MultipartReader()
	if mr == nil {
		c.collect(ent)
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return // io.EOF 或者这一段坏了，已经收到的照常用
		}
		c.walk(part, depth+1)
	}
}

func (c *collector) collect(ent *message.Entity) {
	ct, params, _ := ent.Header.ContentType()
	disp, dispParams, _ := ent.Header.ContentDisposition()
	isAttach := disp == "attachment"
	cid := strings.Trim(ent.Header.Get("Content-Id"), "<>")

	name := dispParams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = decodeWord(name)

	switch {
	case ct == "text/plain" && !isAttach:
		c.out.Text += readAll(ent)

	case ct == "text/html" && !isAttach:
		c.out.HTML += readAll(ent)

	// 只有带 Content-ID 的才是内联图——正文里用 cid: 引着它。
	// 没有 ID 的图引不着，那就是个附件，走下面那条路。
	case strings.HasPrefix(ct, "image/") && !isAttach && cid != "":
		body := readAll(ent)
		if len(body) > maxInlineOne || c.used+len(body) > maxInlineTotal {
			c.out.InlineDropped++
			return
		}
		c.used += len(body)
		c.inline[cid] = "data:" + ct + ";base64," +
			base64.StdEncoding.EncodeToString([]byte(body))

	default:
		if name == "" {
			name = "未命名"
			if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
				name += exts[0]
			}
		}
		// 附件只要大小，不要内容——一封带 9MB PDF 的信，每次点开都把
		// 那 9MB 解码进内存再整块扔掉，纯属白烧。
		n, err := io.Copy(io.Discard, ent.Body)
		if err != nil && n == 0 {
			return
		}
		c.out.Attachments = append(c.out.Attachments,
			Attachment{Name: name, Type: ct, Size: int(n)})
	}
}

func readAll(ent *message.Entity) string {
	b, err := io.ReadAll(ent.Body)
	if err != nil {
		return string(b) // 读到一半坏了，已经拿到的照常用
	}
	return string(b)
}
