package mobile

import (
	"encoding/json"
	"strings"
	"testing"
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
		Op: "list", Dir: dir,
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
		{"不认识的动作", MailInput{Op: "跳舞", Dir: dir}, "不认识"},
		{"读的引用不合法", MailInput{Op: "read", Dir: dir, Key: "没有冒号"}, "不合法"},
		{"没配邮箱就同步", MailInput{Op: "sync", Dir: dir}, "还没配邮箱"},
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

// 引用是「账号:UID」。账号名里可能有冒号，得从最后一个切。
func TestSplitMailKey(t *testing.T) {
	cases := map[string]struct {
		account string
		uid     uint32
		ok      bool
	}{
		"工作:1234": {"工作", 1234, true},
		"a:b:99":  {"a:b", 99, true},
		"没有冒号":    {"", 0, false},
		"工作:":     {"", 0, false},
		"工作:不是数字": {"", 0, false},
		"工作:-1":   {"", 0, false},
	}
	for key, want := range cases {
		a, u, ok := splitMailKey(key)
		if ok != want.ok || (ok && (a != want.account || u != want.uid)) {
			t.Errorf("splitMailKey(%q) = (%q, %d, %v)，该是 (%q, %d, %v)",
				key, a, u, ok, want.account, want.uid, want.ok)
		}
	}
}
