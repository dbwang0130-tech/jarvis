package chat

import (
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// testSkills 给出这个 app 真实的那一套技能，**走的是生产代码那条路**
// （`Session.buildSkills()`），不是另抄一份清单。
//
// 这个包里有四道守门测试都要这套东西：工具标签齐不齐、只读标记丢没丢、
// 行为规则有没有指错方向、工具声明跟金标一致。它们原先各自照抄一份
// 「六个技能」的字面清单——于是往 buildSkills() 里加第七个技能时，四道守卫
// 会**全部照旧变绿**，而它们的全部价值就在于覆盖全。
//
// 这里只负责把 Session 上那几个字段填成空的/临时的，技能清单本身由
// buildSkills() 说了算：加一项就自动进这四道守卫。
// testClock 是测试里的「现在」。
//
// **金标类的测试必须钉住它。** 日程的 Describe 会印出今天的日期，不钉的话金标
// 每天都不一样——而且长度往往相同（日期位数不变），报出来只是「内容不同」，
// 最难猜。
var testClock = time.Date(2026, 9, 26, 14, 21, 0, 0, time.Local)

func testSkills() []agent.Skill {
	cfg := config.Default()
	s := &Session{cfg: cfg}
	ws := orgtools.NewWorkspace(&org.Collection{}, cfg)
	ws.Now = func() time.Time { return testClock }
	s.orgWS = ws
	s.mailS = mail.NewSession(cfg)
	s.ledgerSkill = ledger.NewSkill("")
	s.healthSkill = health.New("")
	s.memSkill = memory.New("")
	return s.buildSkills()
}
