package mailbox

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 账单、验证码、活动通知这类信的内容全在 HTML 的排版里，
// 压成纯文本就只剩一堆散字。展示用的原文必须留着 HTML。
func TestParseBodyKeepsHTML(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=b1\r\n\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"你的验证码是 123456\r\n" +
		"--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>你的验证码是 <b>123456</b></p>\r\n" +
		"--b1--\r\n"

	b, err := ParseBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.HTML, "<b>123456</b>") {
		t.Errorf("HTML 被剥掉了：%q", b.HTML)
	}
	if !strings.Contains(b.Text, "123456") {
		t.Errorf("纯文本那份也该留着：%q", b.Text)
	}
}

// 内联图片在正文里是 cid: 引着的，不嵌进去就是一排破图。
func TestParseBodyEmbedsInlineImages(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=b1\r\n\r\n" +
		"--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<img src="cid:logo123">` + "\r\n" +
		"--b1\r\nContent-Type: image/png\r\nContent-Id: <logo123>\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(png) + "\r\n" +
		"--b1--\r\n"

	b, err := ParseBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.HTML, "cid:logo123") {
		t.Errorf("cid 没换成 data URI：%q", b.HTML)
	}
	if !strings.Contains(b.HTML, "data:image/png;base64,") {
		t.Errorf("图片没嵌进去：%q", b.HTML)
	}
}

// 附件不塞进正文，但得让人知道有这些东西。
func TestParseBodyListsAttachments(t *testing.T) {
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b1\r\n\r\n" +
		"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"见附件\r\n" +
		"--b1\r\nContent-Type: application/pdf\r\n" +
		"Content-Disposition: attachment; filename=\"合同.pdf\"\r\n\r\n" +
		"%PDF-1.4 假的\r\n" +
		"--b1--\r\n"

	b, err := ParseBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Attachments) != 1 {
		t.Fatalf("该有一个附件，实际 %d：%+v", len(b.Attachments), b.Attachments)
	}
	a := b.Attachments[0]
	if a.Name != "合同.pdf" || a.Type != "application/pdf" || a.Size == 0 {
		t.Errorf("附件信息不对：%+v", a)
	}
	// 附件内容不该跟着正文出来——几 MB 的东西过一次 JSON 桥没有意义。
	if strings.Contains(b.Text, "%PDF") || strings.Contains(b.HTML, "%PDF") {
		t.Error("附件内容混进正文了")
	}
}

// 太大的内联图片不嵌——base64 会把体积撑到四三倍，嵌进去界面直接卡住。
// 但要让人知道有图没显示出来。
func TestParseBodySkipsHugeInline(t *testing.T) {
	huge := make([]byte, maxInlineOne+1024)
	raw := "MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=b1\r\n\r\n" +
		"--b1\r\nContent-Type: text/html\r\n\r\n" + `<img src="cid:big">` + "\r\n" +
		"--b1\r\nContent-Type: image/jpeg\r\nContent-Id: <big>\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		base64.StdEncoding.EncodeToString(huge) + "\r\n" +
		"--b1--\r\n"

	b, err := ParseBody([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.HTML, "data:image") {
		t.Error("超大的图不该嵌进来")
	}
	if b.InlineDropped != 1 {
		t.Errorf("该记下有 1 张图没显示，实际 %d", b.InlineDropped)
	}
}

// 烂邮件不能让整个读取失败——收件箱里什么都有。
func TestParseBodyTolerates(t *testing.T) {
	for _, raw := range []string{
		"",
		"就一行字，没有任何头",
		"Content-Type: multipart/mixed; boundary=b1\r\n\r\n--b1\r\n没有收尾",
	} {
		b, err := ParseBody([]byte(raw))
		if err != nil && b != nil {
			t.Errorf("既报错又返回了东西：%q", raw)
		}
		// 只要不 panic、不卡住就算过。
	}
}
