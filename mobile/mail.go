package mobile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/mail"
)

// MailAccount 是界面传进来的一个邮箱账号。字段跟桌面端的配置对齐，
// 但密码是直接给的——手机上没有 macOS 钥匙串，密码存在 iOS 自己的钥匙串里。
type MailAccount struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	SMTPHost string `json:"smtp_host,omitempty"`
	SMTPPort int    `json:"smtp_port,omitempty"`
	From     string `json:"from,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Days     int    `json:"days,omitempty"`
}

func (a MailAccount) toConfig() config.MailAccount {
	c := config.MailAccount{
		Name: a.Name, Host: a.Host, Port: a.Port, User: a.User,
		Password: a.Password, SMTPHost: a.SMTPHost, SMTPPort: a.SMTPPort,
		From: a.From, Limit: a.Limit, Days: a.Days,
	}
	c.Normalize()
	return c
}

// 邮件会话跨轮次留着。
//
// 每轮都新建一个的话，上一轮列出来的邮件编号、triage 的结论、起好的草稿
// 全没了——「把第 3 封清理掉」就指不到任何东西。手机上 app 进程是长着的，
// 正好可以留住。账号一改就换一个新的。
var (
	mailMu      sync.Mutex
	mailSession *mail.Session
	mailKey     string
)

func mailSessionFor(accounts []MailAccount) *mail.Session {
	if len(accounts) == 0 {
		return nil
	}
	key := accountsKey(accounts)

	mailMu.Lock()
	defer mailMu.Unlock()
	if mailSession != nil && mailKey == key {
		return mailSession
	}
	if mailSession != nil {
		mailSession.Close() // 账号换了，旧连接别留着
	}

	cfg := config.Config{}
	for _, a := range accounts {
		cfg.Accounts = append(cfg.Accounts, a.toConfig())
	}
	mailSession = mail.NewSession(cfg)
	// 手机上只拉未读。已读的多半在别处处理过了，再拉回来只会让清单变长、
	// 分拣多花一次钱。
	mailSession.UnreadOnly = true
	mailKey = key
	return mailSession
}

func accountsKey(accounts []MailAccount) string {
	h := sha256.New()
	for _, a := range accounts {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%d\x00",
			a.Name, a.Host, a.Port, a.User, a.Password, a.SMTPHost, a.SMTPPort)
	}
	return hex.EncodeToString(h.Sum(nil))
}
