package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// volatileSkill 的行为规则是死的，环境快照每次都不一样——真实技能就是这个
// 形状（余额、未读数、最近一次体重都会变，规则不会）。
type volatileSkill struct{ n int }

func (volatileSkill) Name() string { return "记账" }

func (s *volatileSkill) Describe(b *strings.Builder) {
	s.n++
	fmt.Fprintf(b, "账本共 %d 笔交易，余额 %d.00。\n", s.n, s.n*37)
}

func (volatileSkill) Instructions() string {
	// 长一点，才看得出「稳定的那段有多长」这件事的分量。
	return strings.Repeat("复式记账，每笔至少两条 posting，付款那条留空让系统配平。\n", 40)
}

func (volatileSkill) Tools() []Tool { return nil }

// 系统提示里**一个会变的字都不许有**。
//
// 请求的前缀是「工具声明 + system + 对话历史…」，服务端按跟上一次的最长公共
// 前缀算便宜价。所以只要 system 里有一处每轮都变的东西，它后面的整段历史就
// 全都命不中——长会话里历史能占两万多 token，那才是钱的大头。每轮都变的东西
// 统一收进最后那条【当下】（见 stateNote）。
//
// 这个测试换了两次时间、也让技能的快照变了一次，systemPrompt 必须逐字节相同。
func TestSystemPromptIsByteStableAcrossTurns(t *testing.T) {
	sk := &volatileSkill{}
	a := NewAgent(nil, []Skill{sk}, 5, AutoOff)
	a.Profile = "我是明哥"

	t0 := time.Date(2026, 9, 26, 14, 21, 0, 0, time.Local)
	a.Now = func() time.Time { return t0 }
	first := a.systemPrompt()
	firstNow := a.stateNote().Content

	a.Now = func() time.Time { return t0.Add(time.Minute) }
	second := a.systemPrompt()
	secondNow := a.stateNote().Content

	if first != second {
		n := 0
		for n < len(first) && n < len(second) && first[n] == second[n] {
			n++
		}
		t.Errorf("系统提示变了（第 %d 字节起），这会废掉它后面整段历史的缓存。\n"+
			"变的那截：%q ↔ %q", n,
			first[n:min(n+80, len(first))], second[n:min(n+80, len(second))])
	}

	// 反过来也要成立：易变的东西确实在【当下】里，不是被谁弄丢了。
	if firstNow == secondNow {
		t.Error("【当下】两次一模一样——时间该变的，是不是没取到")
	}
	if !strings.Contains(firstNow, "14:21") {
		t.Errorf("【当下】里没有当下时间：%q", firstNow)
	}
	// 快照也归它。
	if !strings.Contains(firstNow, "余额") {
		t.Errorf("【当下】里没有技能的环境快照：%q", firstNow)
	}
	if strings.Contains(first, "余额") {
		t.Error("环境快照又回到系统提示里了")
	}
}
