package chat

import (
	"testing"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/browser"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// 每个工具都得有中文标签，否则界面上直接露出英文工具名。
//
// 这条以前只靠 CLAUDE.md 里一句「新增工具别忘了补」，而表还有两份
// （桌面端一份、iOS 一份），谁也不守谁：iOS 那份留着早就删掉的 read_mail、
// 挂着五个根本不存在的工具，新加的健康那四个一个都没补。
// 现在表只有一份（agent/label.go），再加这个测试——注册了工具却忘了
// 登记标签，这儿就红。
func TestEveryToolHasALabel(t *testing.T) {
	cfg := config.Default()
	coll := &org.Collection{}
	skills := []agent.Skill{
		orgtools.NewSkill(orgtools.NewWorkspace(coll, cfg)),
		mail.NewSkill(mail.NewSession(cfg)),
		ledger.NewSkill(""),
		health.New(""),
		browser.New(cfg),
		memory.New(""),
	}
	// 内置的那两个不在任何 skill 里，单独点名。
	names := []string{"plan", "step_done"}
	for _, sk := range skills {
		for _, tl := range sk.Tools() {
			names = append(names, tl.Spec().Name)
		}
	}
	if len(names) < 30 {
		t.Fatalf("只收集到 %d 个工具，skill 的构造大概出错了", len(names))
	}
	for _, n := range names {
		if agent.ToolLabel(n) == n {
			t.Errorf("工具 %q 没有中文标签，界面上会直接显示英文——"+
				"去 internal/agent/label.go 补一行", n)
		}
	}
}
