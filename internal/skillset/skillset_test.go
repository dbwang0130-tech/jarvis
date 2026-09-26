package skillset

import (
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/browser"
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
		Org:     orgtools.NewWorkspace(&org.Collection{}, cfg),
		Mail:    mail.NewSession(cfg),
		Ledger:  ledger.NewSkill(""),
		Health:  health.New(""),
		Browser: browser.New(cfg),
		Memory:  memory.New(""),
	}
}

// 两端各带哪几项，钉在这儿。
//
// **不是为了防「顺序变了」，是为了让差异在 diff 里看得见。** 手机上不带浏览器
// 是有意的，可这件事以前只体现为「另一份清单里恰好没写」——于是 CLAUDE.md 里
// 那句「iOS 端只带日程和记账两项技能」错了整整两项，而要发现它得把两个包里
// 两份分散的清单并排比一遍。以后改任何一端的能力集，这儿会把变化摆出来。
func TestBothEnds(t *testing.T) {
	cfg := config.Default()

	cases := []struct {
		name string
		deps Deps
		want []string
	}{
		{
			// 桌面端：全都有，浏览器也有（CDP 连本机 Chrome）。
			"桌面端", full(cfg),
			[]string{"日程", "邮件", "记账", "健康", "浏览器", "记忆"},
		},
		{
			// 手机端配了邮箱：只少浏览器。
			"手机端（配了邮箱）", func() Deps { d := full(cfg); d.Browser = nil; return d }(),
			[]string{"日程", "邮件", "记账", "健康", "记忆"},
		},
		{
			// 手机端没配邮箱：邮件那项也不在场。
			"手机端（没配邮箱）", func() Deps {
				d := full(cfg)
				d.Browser, d.Mail = nil, nil
				return d
			}(),
			[]string{"日程", "记账", "健康", "记忆"},
		},
	}
	for _, c := range cases {
		got := Names(Build(c.deps))
		if strings.Join(got, "、") != strings.Join(c.want, "、") {
			t.Errorf("%s 的能力集：\n得到 %v\n应是 %v", c.name, got, c.want)
		}
	}
}

// nil 的那几项不许漏进清单。
//
// **一个 nil 的具体指针装进接口之后，接口本身不是 nil**——循环里一调 Name()
// 就 panic，而手机上 panic 一次是整个 app 挂掉（见 CLAUDE.md 的第一条硬规矩）。
// 这个测试就是钉这一条：全 nil 时清单必须是空的，而不是六个会炸的接口。
func TestNilMeansAbsent(t *testing.T) {
	if got := Build(Deps{}); len(got) != 0 {
		t.Fatalf("全 nil 时该是空清单，实际 %d 项", len(got))
	}
	// 真去调一遍：漏进来的 nil 就在这儿炸，而不是在用户手机上。
	for _, s := range Build(Deps{}) {
		_ = s.Name()
	}
}
