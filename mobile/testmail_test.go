package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

// 连服务器那一段没法在测试里跑，但「参数都没填全就别去连」这段逻辑
// 是纯判断——填错了当场说清楚差哪一项，比连出去报一句 IMAP 的英文强。
func TestMailChecksFieldsFirst(t *testing.T) {
	cases := []struct {
		name string
		in   TestMailInput
		want string
	}{
		{"没填服务器", TestMailInput{}, "没填收信服务器"},
		{"没填账号", TestMailInput{Account: MailAccount{Host: "imap.qq.com"}}, "没填账号"},
		{"没填密码", TestMailInput{Account: MailAccount{
			Host: "imap.qq.com", User: "me@qq.com"}}, "没填密码或授权码"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			var out TestMailOutput
			if err := json.Unmarshal([]byte(TestMail(string(raw))), &out); err != nil {
				t.Fatalf("出参不是合法 JSON：%v", err)
			}
			if out.OK {
				t.Error("参数都没填全，不该说通了")
			}
			if out.Message != tc.want {
				t.Errorf("提示是 %q，该是 %q", out.Message, tc.want)
			}
		})
	}

	// 烂入参也不许 panic——手机上崩一次就是整个 app 挂掉。
	var out TestMailOutput
	if err := json.Unmarshal([]byte(TestMail("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}
}

// 英文错误要翻成能照着动手的话。原样甩一句 authentication failed，
// 人也不知道该改哪一项。
func TestExplainMailError(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"AUTHENTICATE failed", "授权码"},
		{"dial tcp: lookup imap.wrong.com: no such host", "找不到这个服务器"},
		{"dial tcp 1.2.3.4:993: i/o timeout", "超时"},
		{"connection refused", "端口"},
		{"x509: certificate is valid for ...", "证书"},
	}
	for _, tc := range cases {
		if got := explainMailError(tc.raw); !strings.Contains(got, tc.want) {
			t.Errorf("%q 翻成了 %q，里面该提到 %q", tc.raw, got, tc.want)
		}
	}
}
