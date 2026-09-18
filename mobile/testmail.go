package mobile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/mailbox"
)

// TestMailInput 只要一个账号的那几项，不碰本地邮件库。
type TestMailInput struct {
	Account MailAccount `json:"account"`
}

// TestMailOutput 说清楚成没成、看到了几个信箱。
type TestMailOutput struct {
	OK bool `json:"ok"`
	// Boxes 是登录后能看到的信箱数。光说「通了」还不够——能列出信箱
	// 说明登录真的成了，不只是 TCP 连上了。
	Boxes   int    `json:"boxes"`
	Millis  int    `json:"millis"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message"`
}

func (o *TestMailOutput) normalize() {}

// TestMail 拿填的这套参数真连一次 IMAP，看通不通。
//
// 模型那边早有「测试连接」，邮箱一直没有：服务器地址、端口、授权码
// 三样里错一样都连不上，而填错了要等到点「收信」才发现，那会儿报的是
// 一句 IMAP 的英文错误，也看不出是哪一项填错了。
//
// 不走 mailSessionFor：那个会把连接缓存起来给后面用，而这儿试的可能是
// 一套还没保存的参数——试完就该断掉，不该留下一个连着错账号的会话。
func TestMail(inputJSON string) string {
	var in TestMailInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&TestMailOutput{
			Error:   fmt.Sprintf("入参不是合法 JSON：%v", err),
			Message: "入参不对",
		})
	}

	a := in.Account
	switch {
	case strings.TrimSpace(a.Host) == "":
		return marshal(&TestMailOutput{Error: "没填服务器", Message: "没填收信服务器"})
	case strings.TrimSpace(a.User) == "":
		return marshal(&TestMailOutput{Error: "没填账号", Message: "没填账号"})
	case strings.TrimSpace(a.Password) == "":
		return marshal(&TestMailOutput{Error: "没填密码", Message: "没填密码或授权码"})
	}

	start := time.Now()
	// Connect 里就把登录做掉了，连上且登录成功才返回。
	cl, err := mailbox.Connect(a.toConfig())
	if err != nil {
		return marshal(&TestMailOutput{
			Millis:  int(time.Since(start).Milliseconds()),
			Error:   err.Error(),
			Message: explainMailError(err.Error()),
		})
	}
	defer cl.Close()

	boxes, err := cl.Mailboxes()
	ms := int(time.Since(start).Milliseconds())
	if err != nil {
		// 登录已经过了（Connect 里做的），只是列不出信箱。账号密码是对的，
		// 别报成连不上——那会让人去改本来没错的那几项。
		return marshal(&TestMailOutput{
			OK: true, Millis: ms,
			Message: fmt.Sprintf("登录成了，%d 毫秒（信箱列不出来：%v）", ms, err)})
	}
	return marshal(&TestMailOutput{
		OK: true, Boxes: len(boxes), Millis: ms,
		Message: fmt.Sprintf("通了，%d 毫秒，看到 %d 个信箱", ms, len(boxes))})
}

// explainMailError 把 IMAP 那边的英文错误翻成能照着动手的话。
// 原样给一句 "authentication failed"，人也不知道该改哪一项。
func explainMailError(raw string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "authentication"), strings.Contains(l, "login"),
		strings.Contains(l, "invalid credentials"), strings.Contains(l, "auth"):
		return "账号或密码不对。多数邮箱要的是**授权码**，不是登录密码" +
			"——在邮箱网页版的设置里开 IMAP 时会给你一串。"
	case strings.Contains(l, "no such host"), strings.Contains(l, "not found"),
		strings.Contains(l, "dns"):
		return "找不到这个服务器，检查一下地址（常见的是 imap.qq.com、imap.163.com）。"
	case strings.Contains(l, "timeout"), strings.Contains(l, "deadline"),
		strings.Contains(l, "i/o timeout"):
		return "连不上，超时了。检查网络，以及端口是不是 993。"
	case strings.Contains(l, "connection refused"):
		return "对面拒绝连接。多半是端口填错了（IMAP over TLS 是 993）。"
	case strings.Contains(l, "certificate"), strings.Contains(l, "x509"),
		strings.Contains(l, "tls"):
		return "证书对不上。检查服务器地址有没有写错。"
	default:
		return "连不上：" + raw
	}
}
