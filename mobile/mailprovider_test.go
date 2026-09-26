package mobile

import (
	"encoding/json"
	"testing"
)

func lookupMail(t *testing.T, in string) MailProviderOutput {
	t.Helper()
	var v MailProviderOutput
	if err := json.Unmarshal([]byte(MailProvider(in)), &v); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return v
}

func TestMailProviderKnown(t *testing.T) {
	got := lookupMail(t, `{"address":"me@qq.com"}`)
	if !got.Known || got.Host != "imap.qq.com" || got.Port != 993 {
		t.Fatalf("QQ 没查对：%+v", got)
	}
	if got.Note == "" {
		t.Error("QQ 要授权码，这句话必须给出来——它是唯一真正会卡住人的一步")
	}
}

// 没收录的给一份猜的，但 known 必须是 false，界面才好说实话。
func TestMailProviderGuesses(t *testing.T) {
	got := lookupMail(t, `{"address":"me@some-company.example"}`)
	if got.Known {
		t.Fatal("没收录的报成了认得")
	}
	if got.Host != "imap.some-company.example" || got.SMTPPort != 465 {
		t.Errorf("猜的不对：%+v", got)
	}
}

// 地址还没打完时什么都别给——半截的 "imap." 填进框里比空着更糟。
// 界面是每改一个字就调一次的，这条路走得最多。
func TestMailProviderHoldsBackUntilUsable(t *testing.T) {
	for _, in := range []string{
		`{"address":""}`, `{"address":"me"}`, `{"address":"me@"}`,
		`{"address":"  "}`, `{}`, `不是 JSON`,
	} {
		got := lookupMail(t, in)
		if got.Host != "" || got.Port != 0 || got.Known {
			t.Errorf("%s 不该给出东西：%+v", in, got)
		}
	}
}
