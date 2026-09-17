package mail

import (
	"strings"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/mailbox"
)

// 分拣清单**不许带正文**。正文是私人内容，为了给一封信打个标签就把它整段
// 发给模型，代价和收益不成比例——推广和垃圾的信号本来就全在头里。
//
// 这条容易在「让分拣更准一点」的名义下被悄悄加回去，所以钉死。
func TestRenderNeverLeaksBody(t *testing.T) {
	secret := "银行卡尾号 8821，验证码 993741，这是很私人的内容"
	items := []*Item{{
		No: 1,
		Msg: &mailbox.Message{
			Account: "工作", FromName: "老张", FromAddr: "z@example.com",
			Subject: "季度汇报", Date: time.Now(), Unread: true,
			Snippet: secret, Size: 4096,
		},
	}}

	out := render(items)

	if strings.Contains(out, secret) || strings.Contains(out, "8821") {
		t.Fatalf("正文漏进给模型的清单里了：\n%s", out)
	}
	if strings.Contains(out, "正文:") {
		t.Errorf("清单里不该有正文那一栏：\n%s", out)
	}

	// 判断要用的那些还得在。
	for _, want := range []string{"老张", "季度汇报", "工作"} {
		if !strings.Contains(out, want) {
			t.Errorf("清单里少了 %q：\n%s", want, out)
		}
	}
	// 得告诉模型正文为什么不在，免得它以为是漏了。
	if !strings.Contains(out, "read_mail") {
		t.Errorf("没说清楚拿不准时该怎么办：\n%s", out)
	}
}

// 已经判过的那些同样不带正文。
func TestRenderCompactAlsoClean(t *testing.T) {
	items := []*Item{{
		No: 1,
		Msg: &mailbox.Message{
			Account: "工作", FromName: "推广", Subject: "大促",
			Date: time.Now(), Snippet: "买一送一快来抢购",
		},
		Triage: Triage{Verdict: VerdictPromo, Reason: "营销"},
	}}
	out := render(items)
	if strings.Contains(out, "买一送一") {
		t.Errorf("紧凑格式也漏了正文：\n%s", out)
	}
}
