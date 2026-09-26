package config

import (
	"strings"
	"testing"
)

// 认得的那几家要给出**写死的**那一份，不是按域名猜的——苹果就不按
// imap.<域名> 来（imap.mail.me.com），猜出来的连不上。
func TestLookupKnownProviders(t *testing.T) {
	cases := map[string]struct {
		name, host string
		port       int
	}{
		"a@qq.com":      {"QQ 邮箱", "imap.qq.com", 993},
		"a@foxmail.com": {"QQ 邮箱", "imap.qq.com", 993},
		"A@GMail.COM":   {"Gmail", "imap.gmail.com", 993}, // 大小写不该影响
		"a@icloud.com":  {"iCloud 邮件", "imap.mail.me.com", 993},
		"a@me.com":      {"iCloud 邮件", "imap.mail.me.com", 993},
		"a@163.com":     {"网易 163", "imap.163.com", 993},
		// 讯飞收发都在 mail.iflytek.com，不是 imap./smtp. 那一套。
		"a@iflytek.com": {"讯飞邮箱", "mail.iflytek.com", 993},
	}
	for addr, want := range cases {
		got, known := LookupMailProvider(addr)
		if !known {
			t.Errorf("%s 该认得出来", addr)
			continue
		}
		if got.Name != want.name || got.Host != want.host || got.Port != want.port {
			t.Errorf("%s 查出来是 %+v", addr, got)
		}
	}
}

// 没收录的域名照样给一份猜的，但**要说清是猜的**——界面靠这个 bool
// 决定说「已识别为 QQ 邮箱」还是「按常见约定填了，连不上就自己改」。
func TestLookupGuessesUnknown(t *testing.T) {
	got, known := LookupMailProvider("me@mycompany.example")
	if known {
		t.Fatal("没收录的域名不该报成认得")
	}
	if got.Host != "imap.mycompany.example" || got.Port != 993 {
		t.Errorf("猜的不对：%+v", got)
	}
	if got.SMTPHost != "smtp.mycompany.example" || got.SMTPPort != 465 {
		t.Errorf("SMTP 猜的不对：%+v", got)
	}
	// 猜的那份不该编出一条「去哪儿开授权码」——我们并不知道。
	if got.Note != "" || got.NoteURL != "" {
		t.Errorf("给没收录的域名编了提示：%+v", got)
	}
}

// 不是邮箱地址就什么都别给，别让界面拿 "imap." 当服务器填进去。
func TestLookupRejectsNonAddress(t *testing.T) {
	for _, bad := range []string{"", "没有 at 号", "@", "a@", "   "} {
		got, known := LookupMailProvider(bad)
		if known {
			t.Errorf("%q 不该认得", bad)
		}
		if got.Host != "" && strings.TrimSuffix(got.Host, "imap.") == "" {
			t.Errorf("%q 给出了半截服务器：%q", bad, got.Host)
		}
	}
	if h, _ := LookupMailProvider("a@"); h.Host != "" {
		t.Errorf("域名是空的却给了服务器：%q", h.Host)
	}
}

// 表本身的自检：域名不许重复（两条都能匹配时，查出来的是先写的那条，
// 而写表的人多半以为是后写的那条生效），端口不许是 0。
func TestProviderTableIsSane(t *testing.T) {
	seen := map[string]string{}
	for _, p := range MailProviders() {
		if p.Host == "" || p.Port == 0 {
			t.Errorf("%s 没写全：%+v", p.Name, p)
		}
		if len(p.Domains) == 0 {
			t.Errorf("%s 一个域名都没有", p.Name)
		}
		for _, d := range p.Domains {
			if d != strings.ToLower(d) {
				t.Errorf("%s 的域名 %q 不是小写，查不到", p.Name, d)
			}
			if prev, dup := seen[d]; dup {
				t.Errorf("域名 %q 在 %s 和 %s 里各有一条", d, prev, p.Name)
			}
			seen[d] = p.Name
		}
	}
}

// 表里那几项要能直接喂给 MailAccount——fill 只在字段是零值时才补，
// 所以预设填过的项必须原样留着。
func TestProviderFeedsAccount(t *testing.T) {
	p, _ := LookupMailProvider("me@icloud.com")
	acc := MailAccount{
		Name: "私人", User: "me@icloud.com",
		Host: p.Host, Port: p.Port, SMTPHost: p.SMTPHost, SMTPPort: p.SMTPPort,
	}
	acc.Normalize()
	// 苹果的 SMTP 是 587，而 fill 的默认是 465——预设被默认值盖掉的话，
	// 收信正常、发信一直失败，最难查。
	if acc.SMTPPort != 587 || acc.SMTPHost != "smtp.mail.me.com" {
		t.Errorf("预设被默认值盖了：%+v", acc)
	}
}
