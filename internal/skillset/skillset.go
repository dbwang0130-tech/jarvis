// Package skillset 决定「这一端、这份配置下，秘书带哪几项能力，按什么顺序」。
//
// **原先这是两份手抄清单**：`internal/chat` 一份、`mobile` 一份。两端带的东西
// 确实不一样（手机上不带浏览器，邮件要配了邮箱才有），但这个差异写成了「另一份
// 清单里恰好没写」——没人看得出来差在哪。后果不是假想的：CLAUDE.md 里那句
// 「iOS 端只带日程和记账两项技能」错了整整两项，而要发现它得把两个包里两份
// 分散的清单并排比一遍。
//
// 现在只有一份 `Build`。**两端的差异变成调用处的一句声明**（比如邮箱没配
// 就不带邮件能力），而不是一处遗漏。顺序也归这儿——它决定系统提示里各技能
// 的出场次序，而那是每轮都要重发的内容，不该由两个包各自决定。
//
// 这个包**不碰任何文件**：各端自己把 Workspace、Session 这些准备好（桌面从
// iCloud，手机从临时目录，见 mobile 包的说明），它只管谁该在场。
package skillset

import (
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/orgtools"
)

// Deps 是装配要用到的那些已经建好的东西。
//
// **nil 表示这一端（或这一轮）不带这项能力。** 这样「手机上没有浏览器」就是
// 调用处明写的一行，而不是某份清单里的一处空白。
type Deps struct {
	// Org 是日程工作区。两端都有。
	Org *orgtools.Workspace
	// Mail 为 nil 表示没配邮箱，这一轮就没有邮件能力。
	Mail *mail.Session
	// Ledger 是记账。两端都有。
	Ledger *ledger.Skill
	// Health 是体征和饮食。两端都有。
	Health *health.Skill
	// Memory 是跨会话长期记忆。两端都有。
	Memory *memory.Skill
}

// Build 按固定顺序交出这一轮的技能清单，nil 的那些跳过。
//
// 顺序有意这么排：日程和邮件是每天真会问的，排前面；记忆放最后，它只贡献
// 几行偏好，不贡献查询。这个顺序进的是系统提示，改它要想清楚。
//
// **别在这儿把 nil 直接塞进 []agent.Skill。** 一个 nil 的具体指针装进
// 接口之后接口本身不是 nil，循环里一调 Name() 就 panic——而手机上 panic 一次
// 就是整个 app 挂掉。
func Build(d Deps) []agent.Skill {
	var out []agent.Skill
	add := func(s agent.Skill, ok bool) {
		if ok {
			out = append(out, s)
		}
	}
	add(orgtools.NewSkill(d.Org), d.Org != nil)
	add(mail.NewSkill(d.Mail), d.Mail != nil)
	add(d.Ledger, d.Ledger != nil)
	add(d.Health, d.Health != nil)
	add(d.Memory, d.Memory != nil)
	return out
}

// Names 交出清单里各技能的名字，供界面和文档用——别再手抄一份。
func Names(skills []agent.Skill) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Name())
	}
	return out
}
