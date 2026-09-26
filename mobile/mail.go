package mobile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/mailstore"
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

// persistMailSession 把会话里的邮件并进本地索引，返回有没有真存下东西。
//
// 秘书收信走的是 agent 的 refresh_mail 工具，它只更新内存里那个会话；
// 而邮件页读的是本地索引。少了这一趟，人在对话里刚听秘书说完「收到三封」，
// 切过去还是空的，得自己再下拉一次——同一批邮件收两遍，钱和时间都白花。
//
// **不做分拣**：该判的秘书那一轮里已经判过了，结论就在会话里，跟着一起存。
// 这儿再调一次模型就是为同一批邮件付第二次钱。
func persistMailSession(indexDir, bodyDir string, sess *mail.Session, accounts []MailAccount) bool {
	if sess == nil || strings.TrimSpace(indexDir) == "" {
		return false
	}
	items := sess.Items()
	if len(items) == 0 {
		return false
	}
	store, err := mailstore.New(indexDir, bodyDir)
	if err != nil {
		return false
	}

	byAccount := map[string][]mailstore.Record{}
	triages := map[string]map[uint32]mailstore.Triage{}
	for _, it := range items {
		m := it.Msg
		byAccount[m.Account] = append(byAccount[m.Account], mailstore.Record{
			Account: m.Account, UID: uint32(m.UID),
			From: m.From(), Addr: m.FromAddr, Subject: m.Subject,
			Date: m.Date, Unread: m.Unread, Snippet: m.Snippet,
		})
		if it.Verdict == "" {
			continue
		}
		if triages[m.Account] == nil {
			triages[m.Account] = map[uint32]mailstore.Triage{}
		}
		triages[m.Account][uint32(m.UID)] = mailstore.Triage{
			Verdict: string(it.Verdict), Reason: it.Reason}
	}

	saved := false
	for _, a := range accounts {
		// 一个账号的索引坏了，不该把别的账号也一起憋回去。存不下来就
		// 算了：秘书已经在对话里把事说清楚了，邮件页晚一步不至于误事。
		if _, err := store.Merge(a.Name, byAccount[a.Name]); err != nil {
			continue
		}
		saved = true
		if vs := triages[a.Name]; len(vs) > 0 {
			_ = store.SetTriages(a.Name, vs)
		}
	}
	return saved
}
