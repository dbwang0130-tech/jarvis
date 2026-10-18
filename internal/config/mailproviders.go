package config

import "strings"

// MailProvider 是一家邮件服务商的接入参数。
//
// **要它是因为这几项没人记得住。** 加一个邮箱要填服务器、端口、授权码三样，
// 前两样是固定的公开常识（imap.qq.com:993 十年没变过），却要用户自己去搜；
// 搜错一个字母，得到的是一句 IMAP 的英文超时。按域名查出来直接填上，
// 真正需要人做的就只剩「去开一个授权码」那一件事。
type MailProvider struct {
	// Name 是显示名（「QQ 邮箱」），认出来之后说给用户听。
	Name string
	// Domains 是这家的邮箱后缀，全小写。
	Domains []string
	Host    string
	Port    int
	// SMTPHost / SMTPPort 留空就按 Host 推导（见 fill）。多数家都是
	// imap.x.com / smtp.x.com:465，只有几家不守这个规矩，才在表里写死。
	SMTPHost string
	SMTPPort int
	// Note 是**真正会卡住人的那一句**：绝大多数邮箱不收登录密码，要先去开
	// 一个授权码，而这件事没人会主动去查。
	Note string
	// NoteURL 是去哪儿开那个授权码。能直接跳就别让人自己翻设置。
	NoteURL string
}

// mailProviders 是常见服务商。
//
// 排序无关紧要，查的是域名。**只收「密码/授权码能登进去」的**——
// 微软个人版 Outlook 从 2024 年起停了 IMAP 的基本认证，只剩 OAuth，
// 列进来等于给一份必然失败的配置，比不列更糟。
var mailProviders = []MailProvider{
	{
		Name: "QQ 邮箱", Domains: []string{"qq.com", "foxmail.com", "vip.qq.com"},
		Host: "imap.qq.com", Port: 993, SMTPHost: "smtp.qq.com", SMTPPort: 465,
		Note:    "QQ 邮箱不收登录密码，要在「设置 → 账号」里开 IMAP 并生成授权码。",
		NoteURL: "https://wx.mail.qq.com/list/readtemplate?name=app_intro.html#/agreement/authorizationCode",
	},
	{
		Name: "网易 163", Domains: []string{"163.com"},
		Host: "imap.163.com", Port: 993, SMTPHost: "smtp.163.com", SMTPPort: 465,
		Note:    "163 要在「设置 → POP3/SMTP/IMAP」里开服务，拿到的授权码填在密码栏。",
		NoteURL: "https://mail.163.com/",
	},
	{
		Name: "网易 126", Domains: []string{"126.com"},
		Host: "imap.126.com", Port: 993, SMTPHost: "smtp.126.com", SMTPPort: 465,
		Note:    "126 要在「设置 → POP3/SMTP/IMAP」里开服务，拿到的授权码填在密码栏。",
		NoteURL: "https://mail.126.com/",
	},
	{
		Name: "网易 yeah", Domains: []string{"yeah.net"},
		Host: "imap.yeah.net", Port: 993, SMTPHost: "smtp.yeah.net", SMTPPort: 465,
		Note: "要在「设置 → POP3/SMTP/IMAP」里开服务，拿到的授权码填在密码栏。",
	},
	{
		Name: "Gmail", Domains: []string{"gmail.com", "googlemail.com"},
		Host: "imap.gmail.com", Port: 993, SMTPHost: "smtp.gmail.com", SMTPPort: 465,
		Note:    "Gmail 要先开两步验证，再生成一个「应用专用密码」填在密码栏。",
		NoteURL: "https://myaccount.google.com/apppasswords",
	},
	{
		Name: "iCloud 邮件", Domains: []string{"icloud.com", "me.com", "mac.com"},
		// 苹果不按 imap.<域名> 来，所以这两项必须写死。
		Host: "imap.mail.me.com", Port: 993,
		SMTPHost: "smtp.mail.me.com", SMTPPort: 587,
		Note:    "iCloud 要在 Apple 账户页生成一个「App 专用密码」填在密码栏。",
		NoteURL: "https://account.apple.com/account/manage",
	},
	{
		Name: "新浪邮箱", Domains: []string{"sina.com", "sina.cn"},
		Host: "imap.sina.com", Port: 993, SMTPHost: "smtp.sina.com", SMTPPort: 465,
		Note: "要在邮箱设置里开 IMAP 服务。",
	},
	{
		Name: "阿里云邮箱", Domains: []string{"aliyun.com"},
		Host: "imap.aliyun.com", Port: 993, SMTPHost: "smtp.aliyun.com", SMTPPort: 465,
	},
	{
		Name: "中国移动 139", Domains: []string{"139.com"},
		Host: "imap.139.com", Port: 993, SMTPHost: "smtp.139.com", SMTPPort: 465,
		Note: "要在邮箱设置里开 IMAP 服务并拿授权码。",
	},
	{
		Name: "Yahoo", Domains: []string{"yahoo.com", "yahoo.co.jp", "ymail.com"},
		Host: "imap.mail.yahoo.com", Port: 993,
		SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 465,
		Note:    "Yahoo 要生成一个「应用专用密码」填在密码栏。",
		NoteURL: "https://login.yahoo.com/account/security",
	},
	{
		Name: "Fastmail", Domains: []string{"fastmail.com", "fastmail.fm"},
		Host: "imap.fastmail.com", Port: 993,
		SMTPHost: "smtp.fastmail.com", SMTPPort: 465,
		Note:    "Fastmail 要在 Settings → Privacy & Security 里生成一个 App Password。",
		NoteURL: "https://app.fastmail.com/settings/security/apppassword",
	},
	{
		Name: "讯飞邮箱", Domains: []string{"iflytek.com"},
		// 收发都在 mail.iflytek.com，**不是** imap./smtp. 那一套，
		// 所以猜不出来，必须写死。
		Host: "mail.iflytek.com", Port: 993,
		SMTPHost: "mail.iflytek.com", SMTPPort: 465,
	},
	{
		Name: "Zoho Mail", Domains: []string{"zoho.com", "zohomail.com"},
		Host: "imap.zoho.com", Port: 993, SMTPHost: "smtp.zoho.com", SMTPPort: 465,
	},
}

// MailDomain 取出邮箱地址的域名部分，全小写。不是个邮箱地址就返回空串。
func MailDomain(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(address[at+1:]))
}

// LookupMailProvider 按邮箱地址查出接入参数。
//
// 第二个返回值是**认不认得这家**，不是「有没有结果」：没收录的域名一样给一份
// 按 imap.<域名>:993 / smtp.<域名>:465 猜出来的，因为这个约定绝大多数自建
// 和企业邮箱都守着，猜中了就省掉两个框，猜错了用户照样能改。区分这两者是为了
// 界面能说实话——认得的可以说「已识别为 QQ 邮箱」，猜的只能说「按常见约定填了，
// 连不上就自己改」。
func LookupMailProvider(address string) (MailProvider, bool) {
	domain := MailDomain(address)
	if domain == "" {
		return MailProvider{}, false
	}
	for _, p := range mailProviders {
		for _, d := range p.Domains {
			if d == domain {
				return p, true
			}
		}
	}
	return MailProvider{
		Name:     domain,
		Domains:  []string{domain},
		Host:     "imap." + domain,
		Port:     993,
		SMTPHost: "smtp." + domain,
		SMTPPort: 465,
	}, false
}
