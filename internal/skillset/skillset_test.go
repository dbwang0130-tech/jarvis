package skillset

import (
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// 全都给齐时的样子，也就是桌面端。
func full(cfg config.Config) Deps {
	return Deps{
		Org:    orgtools.NewWorkspace(&org.Collection{}, cfg),
		Mail:   mail.NewSession(cfg),
		Ledger: ledger.NewSkill(""),
		Health: health.New(""),
		Memory: memory.New(""),
	}
}

// 两端各带哪几项，钉在这儿。
//
// **不是为了防「顺序变了」，是为了让差异在 diff 里看得见。** 早先两端各写一份
// 清单，差异只体现为「另一份里恰好没写」——于是 CLAUDE.md 里那句「其他客户端只带
// 日程和记账两项技能」错了整整两项，而要发现它得把两个包里两份分散的清单并排
// 比一遍。以后改能力集，这儿会把变化摆出来。
//
// 浏览器那一项在合并成一个 app 时删掉了：它靠 chromedp fork/exec 起一个
// Chrome，而 App Sandbox 里不许这么做。
func TestBothEnds(t *testing.T) {
	cfg := config.Default()

	cases := []struct {
		name string
		deps Deps
		want []string
	}{
		{
			// 配了邮箱：全都有。
			"配了邮箱", full(cfg),
			[]string{"日程", "邮件", "记账", "健康", "记忆"},
		},
		{
			// 没配邮箱：邮件那项不在场，别的照旧。
			"没配邮箱", func() Deps { d := full(cfg); d.Mail = nil; return d }(),
			[]string{"日程", "记账", "健康", "记忆"},
		},
	}
	for _, c := range cases {
		var got []string
		for _, s := range Build(c.deps) {
			got = append(got, s.Name())
		}
		if strings.Join(got, "、") != strings.Join(c.want, "、") {
			t.Errorf("%s 的能力集：\n得到 %v\n应是 %v", c.name, got, c.want)
		}
	}
}

// nil 的那几项不许漏进清单。
//
// **一个 nil 的具体指针装进接口之后，接口本身不是 nil**——循环里一调 Name()
// 就 panic。这个测试就是钉这一条：全 nil 时清单必须是空的，而不是六个会炸的接口。
func TestNilMeansAbsent(t *testing.T) {
	if got := Build(Deps{}); len(got) != 0 {
		t.Fatalf("全 nil 时该是空清单，实际 %d 项", len(got))
	}
	// 真去调一遍：漏进来的 nil 就在这儿炸，而不是在用户手机上。
	for _, s := range Build(Deps{}) {
		_ = s.Name()
	}
}
