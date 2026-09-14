package mailbox

import (
	"io"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
)

func testAccount() config.MailAccount {
	return config.MailAccount{
		Name: "test", User: "me@example.com", From: "王大宝",
		Host: "imap.example.com", SMTPHost: "smtp.example.com", SMTPPort: 465,
	}
}

func TestDraftBuildEncodesChinese(t *testing.T) {
	d := Draft{
		ID:      "d1",
		To:      []string{"zhang@example.com"},
		Subject: "关于周三的项目评审",
		Body:    "张老师：\n\n周三下午我有空，会议室已经订好了。\n\n王大宝",
	}
	raw, err := d.Build(testAccount())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("生成的邮件不是合法 RFC 5322: %v", err)
	}

	// 主题必须经过 RFC 2047 编码，且能被标准库还原。
	if got := msg.Header.Get("Subject"); !strings.HasPrefix(got, "=?") {
		t.Errorf("中文主题没有编码: %q", got)
	}
	dec := new(mime.WordDecoder)
	subject, err := dec.DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != d.Subject {
		t.Errorf("主题还原后是 %q（err=%v），应为 %q", subject, err, d.Subject)
	}

	// 正文按 quoted-printable 编码，解回来要和原文一致。
	if ct := msg.Header.Get("Content-Transfer-Encoding"); ct != "quoted-printable" {
		t.Errorf("Content-Transfer-Encoding = %q", ct)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatalf("解码正文失败: %v", err)
	}
	got := strings.ReplaceAll(string(body), "\r\n", "\n")
	if got != d.Body {
		t.Errorf("正文还原后是:\n%q\n应为:\n%q", got, d.Body)
	}

	if from := msg.Header.Get("From"); !strings.Contains(from, "me@example.com") {
		t.Errorf("From 不对: %q", from)
	}
}

func TestDraftBuildThreadsReply(t *testing.T) {
	d := Draft{
		ID: "d2", To: []string{"a@example.com"}, Subject: "Re: 原主题", Body: "好的。",
		InReplyTo: "<abc@example.com>",
	}
	raw, err := d.Build(testAccount())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	// 没给 References 时应回落到 In-Reply-To，客户端才能把回信挂回原会话。
	if got := msg.Header.Get("References"); got != "<abc@example.com>" {
		t.Errorf("References = %q，应回落为 In-Reply-To", got)
	}
}

func TestDraftValidateRejectsBadInput(t *testing.T) {
	cases := []struct {
		name  string
		draft Draft
	}{
		{"没有收件人", Draft{Subject: "x", Body: "y"}},
		{"地址不合法", Draft{To: []string{"不是邮箱"}, Subject: "x", Body: "y"}},
		{"没有主题", Draft{To: []string{"a@b.com"}, Body: "y"}},
		{"正文为空", Draft{To: []string{"a@b.com"}, Subject: "x", Body: "   "}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.draft.Validate(); err == nil {
				t.Error("应当被拒绝，但通过了")
			}
		})
	}
}
