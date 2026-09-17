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
	HTML string
	// Text 是 text/plain 部分。两者可能都有，界面优先用 HTML。
	Text string
	// Attachments 是附件清单。正文里不显示，但得让人知道有这些东西。
	Attachments []Attachment
	// InlineDropped 说明有内联图片因为太大没嵌进来。
	InlineDropped int
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

	out := &FullBody{}
	inline := map[string]string{} // Content-ID → data: URI
	var used int
	walkParts(ent, out, inline, &used)

	// HTML 里的 cid: 引用换成 data: URI。换不掉的留着也无妨——
	// 界面那边拦着不让它去联网取远程图片，一个取不到的 cid 只是个空框。
	for cid, uri := range inline {
		for _, form := range []string{"cid:" + cid, "cid:%3C" + cid + "%3E"} {
			out.HTML = strings.ReplaceAll(out.HTML, form, uri)
		}
	}
	return out, nil
}

func walkParts(ent *message.Entity, out *FullBody, inline map[string]string, used *int) {
	mr := ent.MultipartReader()
	if mr == nil {
		collectPart(ent, out, inline, used)
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			return // io.EOF 或者这一段坏了，已经收到的照常用
		}
		walkParts(part, out, inline, used)
	}
}

func collectPart(ent *message.Entity, out *FullBody, inline map[string]string, used *int) {
	ct, params, _ := ent.Header.ContentType()
	disp, dispParams, _ := ent.Header.ContentDisposition()

	body, err := io.ReadAll(ent.Body)
	if err != nil {
		return
	}

	name := dispParams["filename"]
	if name == "" {
		name = params["name"]
	}
	name = decodeWord(name)

	switch {
	case ct == "text/plain" && disp != "attachment":
		out.Text += string(body)

	case ct == "text/html" && disp != "attachment":
		out.HTML += string(body)

	case strings.HasPrefix(ct, "image/") && disp != "attachment":
		// 内联图片：正文里用 cid: 引着，得嵌进去，不然就是一排破图。
		cid := strings.Trim(ent.Header.Get("Content-Id"), "<>")
		if cid == "" || len(body) > maxInlineOne || *used+len(body) > maxInlineTotal {
			if cid != "" {
				out.InlineDropped++
			} else if name != "" {
				out.Attachments = append(out.Attachments,
					Attachment{Name: name, Type: ct, Size: len(body)})
			}
			return
		}
		*used += len(body)
		inline[cid] = "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(body)

	default:
		if name == "" {
			name = "未命名"
			if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
				name += exts[0]
			}
		}
		out.Attachments = append(out.Attachments,
			Attachment{Name: name, Type: ct, Size: len(body)})
	}
}
