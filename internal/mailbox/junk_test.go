package mailbox

import (
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func box(name string, attrs ...imap.MailboxAttr) *imap.ListData {
	return &imap.ListData{Mailbox: name, Delim: '/', Attrs: attrs}
}

// 用各家邮箱实际返回的文件夹名做判定。
func TestDetectJunkAcrossProviders(t *testing.T) {
	cases := []struct {
		name  string
		boxes []*imap.ListData
		want  string
	}{
		{
			// Gmail 会老老实实标 \Junk。
			"Gmail",
			[]*imap.ListData{
				box("INBOX"),
				box("[Gmail]/所有邮件", imap.MailboxAttrAll),
				box("[Gmail]/垃圾邮件", imap.MailboxAttrJunk),
				box("[Gmail]/已删除邮件", imap.MailboxAttrTrash),
			},
			"[Gmail]/垃圾邮件",
		},
		{
			// QQ 不报 SPECIAL-USE，且有个不可选的容器节点。
			"QQ",
			[]*imap.ListData{
				box("INBOX"),
				box("Drafts"), box("Junk"), box("Sent Messages"),
				box("Deleted Messages"),
				box("其他文件夹", imap.MailboxAttrNoSelect),
				box("其他文件夹/存档"),
			},
			"Junk",
		},
		{
			// 讯飞用的 Coremail。
			"讯飞 Coremail",
			[]*imap.ListData{box("INBOX"), box("Junk E-mail"), box("Deleted Messages")},
			"Junk E-mail",
		},
		{
			"163 中文名",
			[]*imap.ListData{box("INBOX"), box("垃圾邮件"), box("已删除")},
			"垃圾邮件",
		},
		{
			"Outlook",
			[]*imap.ListData{box("INBOX"), box("Junk Email"), box("Deleted Items")},
			"Junk Email",
		},
		{
			"大小写不敏感",
			[]*imap.ListData{box("INBOX"), box("SPAM")},
			"SPAM",
		},
		{
			// 层级路径只比对最后一段。
			"嵌套路径",
			[]*imap.ListData{box("INBOX"), box("其他文件夹/垃圾邮件")},
			"其他文件夹/垃圾邮件",
		},
		{
			// 精确表不命中，靠关键词兜住。
			"带后缀的名字",
			[]*imap.ListData{box("INBOX"), box("Junk E-mail (7)")},
			"Junk E-mail (7)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := detectJunk(c.boxes)
			if !ok {
				t.Fatalf("没判出垃圾箱，应为 %q", c.want)
			}
			if got.Mailbox != c.want {
				t.Errorf("判成了 %q，应为 %q", got.Mailbox, c.want)
			}
			if got.Reason == "" {
				t.Error("判定依据是空的")
			}
		})
	}
}

// 废纸篓不是垃圾箱，不能拿来顶替。
func TestDetectJunkRefusesTrash(t *testing.T) {
	boxes := []*imap.ListData{
		box("INBOX"), box("Sent"), box("Trash"), box("已删除"), box("Deleted Items"),
	}
	if got, ok := detectJunk(boxes); ok {
		t.Fatalf("只有废纸篓时不该判出垃圾箱，却给了 %q", got.Mailbox)
	}

	// 但错误信息里要提到废纸篓，好让用户知道可以手动指定。
	err := junkNotFound("work", boxes)
	if !strings.Contains(err.Error(), "Trash") {
		t.Errorf("错误没提到识别到的废纸篓: %v", err)
	}
	if !strings.Contains(err.Error(), "junk =") {
		t.Errorf("错误没告诉用户怎么配: %v", err)
	}
}

// 名字再像，不可选的节点也不能选——移邮件进去会失败。
func TestDetectJunkSkipsNoSelect(t *testing.T) {
	boxes := []*imap.ListData{
		box("INBOX"),
		box("Junk", imap.MailboxAttrNoSelect), // 名字匹配，但只是个容器
		box("Spam"),                           // 这个才存得了邮件
	}
	got, ok := detectJunk(boxes)
	if !ok {
		t.Fatal("跳过容器后应当仍能判出垃圾箱")
	}
	if got.Mailbox != "Spam" {
		t.Errorf("选中了 %q，应为可选的 Spam", got.Mailbox)
	}

	// 连 \Junk 标记也一样：标在不可选的节点上不算数。
	marked := []*imap.ListData{
		box("INBOX"),
		box("垃圾", imap.MailboxAttrJunk, imap.MailboxAttrNoSelect),
	}
	if got, ok := detectJunk(marked); ok {
		t.Errorf("不可选的节点即便标了 \\Junk 也不该选，却给了 %q", got.Mailbox)
	}
}

// \Junk 标记比名字更可信。
func TestDetectJunkPrefersSpecialUse(t *testing.T) {
	boxes := []*imap.ListData{
		box("INBOX"),
		box("Junk"), // 名字像，但没标记
		box("我的垃圾桶", imap.MailboxAttrJunk),
	}
	got, ok := detectJunk(boxes)
	if !ok || got.Mailbox != "我的垃圾桶" {
		t.Errorf("应优先采信 \\Junk 标记，实际选了 %q", got.Mailbox)
	}
}

// 收件箱和草稿箱绝不能被误判成垃圾箱。
func TestDetectJunkNeverPicksInbox(t *testing.T) {
	boxes := []*imap.ListData{box("INBOX"), box("Drafts"), box("Sent"), box("Archive")}
	if got, ok := detectJunk(boxes); ok {
		t.Fatalf("没有垃圾箱时不该乱选，却给了 %q", got.Mailbox)
	}
}

func TestLeafName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Junk", "junk"},
		{"[Gmail]/垃圾邮件", "垃圾邮件"},
		{"其他文件夹/垃圾邮件", "垃圾邮件"},
		{"  Spam  ", "spam"},
	}
	for _, c := range cases {
		if got := leafName(c.in, '/'); got != c.want {
			t.Errorf("leafName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 有的服务器用点号做分隔符。
	if got := leafName("INBOX.Junk", '.'); got != "junk" {
		t.Errorf("点号分隔符下 got %q", got)
	}
}
