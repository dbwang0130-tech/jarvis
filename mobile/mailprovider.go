package mobile

import (
	"encoding/json"
	"strings"

	"github.com/dabao/jarvis/internal/config"
)

// MailProviderInput 只要一个邮箱地址。
type MailProviderInput struct {
	Address string `json:"address"`
}

// MailProviderOutput 是按域名查出来的接入参数。
type MailProviderOutput struct {
	// Known 表示这家是收录过的。**不是「有没有结果」**：没收录的也给一份
	// 按 imap.<域名> 猜的，界面靠这个 bool 决定说「已识别为 QQ 邮箱」
	// 还是「按常见约定填的，连不上就自己改」。
	Known bool `json:"known"`
	// Name 是显示名；没收录时就是域名本身。
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	// Note 是真正会卡住人的那一句：多数邮箱不收登录密码，要先开授权码。
	Note string `json:"note"`
	// NoteURL 是去哪儿开。能直接跳就别让人自己翻设置。
	NoteURL string `json:"note_url"`
	Error   string `json:"error"`
}

func (o *MailProviderOutput) normalize() {}

// MailProvider 按邮箱地址查出 IMAP/SMTP 接入参数，给加邮箱那一页自动填。
//
// **纯函数，不联网**，界面可以在每次改动地址时直接调。要它是因为服务器和端口
// 是固定的公开常识（imap.qq.com:993 十年没变过），却要用户自己去搜；搜错一个
// 字母，换来的是一句 IMAP 的英文超时。
//
// 表在 Go 这边只有一份（`config.LookupMailProvider`），Swift 不另抄——服务商表
// 在两端各写一份的毛病栽过一次（见 mobile/providers.go 那段）。
func MailProvider(inputJSON string) string {
	var in MailProviderInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&MailProviderOutput{Error: "入参不是合法 JSON"})
	}
	addr := strings.TrimSpace(in.Address)
	if config.MailDomain(addr) == "" {
		// 还没打完（没有 @ 或者 @ 后面是空的），什么都别给——
		// 半截的 "imap." 填进框里比空着更糟。
		return marshal(&MailProviderOutput{})
	}
	p, known := config.LookupMailProvider(addr)
	return marshal(&MailProviderOutput{
		Known: known, Name: p.Name,
		Host: p.Host, Port: p.Port,
		SMTPHost: p.SMTPHost, SMTPPort: p.SMTPPort,
		Note: p.Note, NoteURL: p.NoteURL,
	})
}
