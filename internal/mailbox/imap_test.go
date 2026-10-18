package mailbox

import (
	"fmt"
	"io"
	"mime"
	"net"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-message/charset"
)

// CLAUDE.md 里写着「mail/browser 要真的 IMAP 和 Chrome，没测」。真的 IMAP 确实
// 没法拿用户的邮箱当靶子——评测会真去动服务器上的邮件。但**假的 IMAP 是现成的**：
// go-imap/v2 自带 imapserver + imapmemserver，而它已经是直接依赖了，起在进程里
// 不联网、不加依赖、不碰任何真邮件。
//
// 生产代码一行没改：`Connect` 走 TLS 拨号，而 `Client` 的字段是包内可见的，
// 测试放在同一个包里就能自己拨号、自己组装——比在生产路径上留一个注入点干净。
//
// 钉的重点是 CLAUDE.md 点过名的那条：**按 UID 而不是时间划增量**。IMAP 的 SINCE
// 只比日期，按时间做增量会把上次同步当天的邮件重新拉一遍。

// fakeIMAP 起一个内存 IMAP，返回已登录的 Client 和往里投信的函数。
func fakeIMAP(t *testing.T) (*Client, func(mailbox, raw string) imap.UID) {
	t.Helper()

	mem := imapmemserver.New()
	user := imapmemserver.NewUser("我", "口令")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	// 垃圾箱探测要认得出它——清理邮件就是往这儿搬。
	if err := user.Create("Trash", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		// 本机明文连接，没有 TLS——测试里不需要，加了只是多一套自签证书要管。
		InsecureAuth: true,
		Caps: imap.CapSet{
			imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {},
			imap.CapMove: {}, imap.CapUIDPlus: {},
		},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	// 用跟生产同一套 WordDecoder：RFC 2047 编码过的主题靠它还原，
	// 而那恰恰是真邮件里最常见的一种。
	c, err := imapclient.DialInsecure(ln.Addr().String(), &imapclient.Options{
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Login("我", "口令").Wait(); err != nil {
		t.Fatal(err)
	}

	cl := &Client{Account: config.MailAccount{
		Name: "测试", User: "我", Days: 3650, Limit: 100,
	}, c: c}

	put := func(mailbox, raw string) imap.UID {
		t.Helper()
		raw = strings.ReplaceAll(raw, "\n", "\r\n")
		cmd := c.Append(mailbox, int64(len(raw)), nil)
		if _, err := io.WriteString(cmd, raw); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := cmd.Wait()
		if err != nil {
			t.Fatal(err)
		}
		return data.UID
	}
	return cl, put
}

func letter(subject, from, body string) string {
	return fmt.Sprintf("From: %s\nTo: 我 <me@example.com>\nSubject: %s\n"+
		"Date: Sat, 26 Sep 2026 10:00:00 +0800\nMIME-Version: 1.0\n"+
		"Content-Type: text/plain; charset=utf-8\n\n%s\n", from, subject, body)
}

// 拉一封、解对：主题、发件人、正文摘要。
func TestFetchParsesMessage(t *testing.T) {
	cl, put := fakeIMAP(t)
	put("INBOX", letter("季度报告要交了", "小王 <wang@example.com>", "记得周五之前交。"))

	msgs, _, err := cl.FetchNew(0, 0)
	if err != nil {
		t.Fatalf("拉取失败：%v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("该拉到 1 封，实际 %d", len(msgs))
	}
	m := msgs[0]
	if m.Subject != "季度报告要交了" {
		t.Errorf("主题不对：%q", m.Subject)
	}
	if m.FromName != "小王" || m.FromAddr != "wang@example.com" {
		t.Errorf("发件人不对：%q <%q>", m.FromName, m.FromAddr)
	}
	if !strings.Contains(m.Snippet, "周五") {
		t.Errorf("正文摘要不对：%q", m.Snippet)
	}
	if m.Account != "测试" {
		t.Errorf("没记住是哪个账号：%q", m.Account)
	}
}

// **RFC 2047 编码过的主题要还原。** 真邮件里中文主题几乎都是这么编的，
// 不解码的话清单上一眼看过去全是 =?utf-8?B?...?=，人根本认不出哪封要紧。
func TestFetchDecodesEncodedSubject(t *testing.T) {
	cl, put := fakeIMAP(t)
	put("INBOX", "From: bank@example.com\nTo: me@example.com\n"+
		"Subject: =?utf-8?B?6ZO26KGM5a+554+t5o+Q6YCB?=\n"+
		"Date: Sat, 26 Sep 2026 10:00:00 +0800\n\n正文\n")

	msgs, _, err := cl.FetchNew(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("该拉到 1 封，实际 %d", len(msgs))
	}
	if strings.Contains(msgs[0].Subject, "=?") {
		t.Errorf("编码过的主题没还原：%q", msgs[0].Subject)
	}
}

// **增量按 UID 划，不按时间。**
//
// CLAUDE.md 专门点过这条：IMAP 的 SINCE 只比日期，按时间做增量会把上次同步当天的
// 邮件重新拉一遍——用户看到的是同一封信在清单上出现两次，而编号还不一样。
func TestFetchNewIsIncrementalByUID(t *testing.T) {
	cl, put := fakeIMAP(t)
	put("INBOX", letter("第一封", "a@example.com", "一"))
	put("INBOX", letter("第二封", "b@example.com", "二"))

	first, info, err := cl.FetchNew(0, 100)
	if err != nil {
		t.Fatalf("首次同步失败：%v", err)
	}
	if len(first) != 2 {
		t.Fatalf("首次该拉到 2 封，实际 %d", len(first))
	}
	if info.MaxUID == 0 {
		t.Fatal("没记下 MaxUID，下次就没法接着拉")
	}
	if info.UIDValidity == 0 {
		t.Error("没记下 UIDValidity，服务器重建索引之后就不知道该重来")
	}

	// 同一天又来了一封。按时间划界的话这三封会被一起拉回来。
	put("INBOX", letter("第三封", "c@example.com", "三"))

	again, info2, err := cl.FetchNew(info.MaxUID, 100)
	if err != nil {
		t.Fatalf("增量同步失败：%v", err)
	}
	if len(again) != 1 {
		var got []string
		for _, m := range again {
			got = append(got, m.Subject)
		}
		t.Fatalf("增量该只有 1 封新的，实际 %d 封：%v——同一封信会在清单上出现两次",
			len(again), got)
	}
	if again[0].Subject != "第三封" {
		t.Errorf("拉回来的不是新那封：%q", again[0].Subject)
	}
	if info2.MaxUID <= info.MaxUID {
		t.Errorf("MaxUID 没往前走：%d → %d", info.MaxUID, info2.MaxUID)
	}

	// 再同步一次，没有新信就该是空的——不是又把第三封拉一遍。
	empty, _, err := cl.FetchNew(info2.MaxUID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("没有新信却拉回 %d 封", len(empty))
	}
}

// 清理邮件就是往垃圾箱搬——搬完收件箱里就不该有了。
func TestMoveToTrash(t *testing.T) {
	cl, put := fakeIMAP(t)
	uid := put("INBOX", letter("广告", "promo@example.com", "打折"))
	put("INBOX", letter("留着的", "a@example.com", "正事"))

	if err := cl.MoveTo([]imap.UID{uid}, "Trash"); err != nil {
		t.Fatalf("搬到垃圾箱失败：%v", err)
	}
	msgs, _, err := cl.FetchNew(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Subject != "留着的" {
		var got []string
		for _, m := range msgs {
			got = append(got, m.Subject)
		}
		t.Errorf("搬走的还在收件箱里：%v", got)
	}
}

// 垃圾箱探测走一遍**真的 IMAP LIST**。
//
// junk_test.go 那几条是拿现成的文件夹名清单跑纯函数，这一条补的是「从服务器上
// LIST 回来的东西能不能认出来」——中间隔着 LIST 的层级分隔符、SPECIAL-USE 标记
// 这些只有真协议才有的东西。
//
// 顺带钉住一条**故意的行为**：只有废纸篓（Trash）时不认，因为语义不同——
// 清理广告是「扔进垃圾邮件」，不是「删掉」。junk.go 里那条拒绝是有意的
// （TestDetectJunkRefusesTrash 守着），我第一次写这条测试时断言反了。
func TestDetectJunkOverRealIMAP(t *testing.T) {
	cl, _ := fakeIMAP(t)

	// 先只有 Trash：该拒绝。
	if _, err := cl.DetectJunk(); err == nil {
		if box, err := cl.JunkMailbox(); err == nil {
			t.Errorf("只有废纸篓却认成了垃圾箱 %q——清理广告不是删邮件", box)
		}
	}

	// 建一个真正的垃圾邮件箱，再探一次。
	if err := cl.c.Create("Junk", nil).Wait(); err != nil {
		t.Fatalf("建 Junk 失败：%v", err)
	}
	if _, err := cl.DetectJunk(); err != nil {
		t.Fatalf("有 Junk 了还是没探出来：%v", err)
	}
	box, err := cl.JunkMailbox()
	if err != nil {
		t.Fatalf("没认出垃圾箱：%v", err)
	}
	if !strings.EqualFold(box, "Junk") {
		t.Errorf("认出来的不是 Junk：%q", box)
	}
}
