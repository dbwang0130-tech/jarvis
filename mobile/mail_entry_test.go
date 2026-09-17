package mobile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/mailstore"
)

func mailCall(t *testing.T, in MailInput) MailOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out MailOutput
	if err := json.Unmarshal([]byte(Mail(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

// 打开邮件页要秒开：只读本地，一个网络请求都不发。
// 没配过邮箱、一封都没同步过的时候也得给出空清单而不是报错。
func TestMailListIsOfflineAndSafe(t *testing.T) {
	dir := t.TempDir()
	out := mailCall(t, MailInput{
		Op: "list", IndexDir: dir,
		Accounts: []MailAccount{{Name: "工作", Host: "imap.example.com", User: "me"}},
	})
	if out.Error != "" {
		t.Fatalf("没同步过不该报错：%s", out.Error)
	}
	if len(out.Items) != 0 {
		t.Errorf("该是空的，实际 %d 条", len(out.Items))
	}
}

// 烂入参一律给话说，不能 panic——手机上崩一次就是整个 app 挂掉。
func TestMailBadInput(t *testing.T) {
	var out MailOutput
	if err := json.Unmarshal([]byte(Mail("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂 JSON 该报错")
	}

	dir := t.TempDir()
	cases := []struct {
		name string
		in   MailInput
		want string
	}{
		{"没给目录", MailInput{Op: "list"}, "邮件库"},
		{"不认识的动作", MailInput{Op: "跳舞", IndexDir: dir}, "不认识"},
		{"读的引用不合法", MailInput{Op: "read", IndexDir: dir, Key: "没有冒号"}, "不合法"},
		{"没配邮箱就同步", MailInput{Op: "sync", IndexDir: dir}, "还没配邮箱"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mailCall(t, tc.in)
			if !strings.Contains(got.Error, tc.want) {
				t.Errorf("提示是 %q，该提到 %q", got.Error, tc.want)
			}
		})
	}
}

// 清理和读正文都该只靠本地索引里的「账号 + UID」，不要求这封信在
// 「这次拉下来的清单」里——用户看的是本地那份，里面的邮件多半早就不在
// 当前会话里了。让他为了清一封信先同步一次，是拿实现细节为难人。
func TestMailActionsDoNotNeedFreshFetch(t *testing.T) {
	dir := t.TempDir()
	accounts := []MailAccount{{
		Name: "工作", Host: "127.0.0.1", Port: 1, User: "me", Password: "x",
	}}

	// 连不上服务器是必然的（端口 1 没人听），要的是**错误话里不再提**
	// 「先同步一下」——那句说明它还在拿清单编号找邮件。
	for _, op := range []string{"read", "clean"} {
		in := MailInput{Op: op, IndexDir: dir, Accounts: accounts,
			Key: "工作:42", Keys: []string{"工作:42"}}
		got := mailCall(t, in)
		if got.Error == "" {
			t.Fatalf("%s 连不上服务器该报错", op)
		}
		if strings.Contains(got.Error, "先同步") {
			t.Errorf("%s 还在要求先同步：%q", op, got.Error)
		}
	}
}

// 同步没拉到新邮件（一封新的都没有，或者干脆连不上服务器）的时候，
// 本地存着的那些照样得给出来。
//
// 之前是按「这次拉到的邮件」分组再合并的，一次空手而归就等于谁都没合过，
// 出参里一封都没有——用户看到的是列表整个清空了，还以为邮件没了。
func TestMailSyncKeepsLocalWhenNothingNew(t *testing.T) {
	dir := t.TempDir()
	bodies := t.TempDir()
	store, err := mailstore.New(dir, bodies)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Merge("工作", []mailstore.Record{
		{UID: 7, From: "老板", Subject: "季度汇报", Date: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	// 端口 1 没人听，这次必然一封也拉不到。
	out := mailCall(t, MailInput{
		Op: "sync", IndexDir: dir, BodyDir: bodies,
		Accounts: []MailAccount{{
			Name: "工作", Host: "127.0.0.1", Port: 1, User: "me", Password: "x",
		}},
	})
	if len(out.Items) != 1 {
		t.Fatalf("本地那封该还在，实际 %d 条：%+v", len(out.Items), out.Items)
	}
	if out.Items[0].Subject != "季度汇报" {
		t.Errorf("给回来的不是本地那封：%+v", out.Items[0])
	}
}
