package agent

import "testing"

// 上下文预算必须把「每一轮都要重发的那部分」算进去：系统提示和工具声明。
//
// 这条以前是漏的——ContextUsage 只数 History，于是工具表一路长到五十几个，
// 每轮白白多发两万字节，而状态栏和压缩阈值都看不见。工具越多漏得越狠，
// 偏偏工具多正是该压缩的时候。
func TestContextUsageCountsPromptAndTools(t *testing.T) {
	a := &Agent{Skills: []Skill{fakeSkill{tools: []Tool{}}}}
	a.tools = a.flattenTools()

	empty, _ := a.ContextUsage()
	if empty <= 0 {
		t.Fatal("一句话没说，系统提示和工具声明也已经占了位置，不该是 0")
	}
	// 光 History 的估算必须比总量小——差的就是固定开销。
	if h := EstimateTokens(a.History); h >= empty {
		t.Fatalf("固定开销没算进去：总量 %d，历史 %d", empty, h)
	}
}

// 工具多了，固定开销就得跟着涨，否则这笔账等于没记。
func TestFixedTokensGrowWithTools(t *testing.T) {
	bare := &Agent{Skills: []Skill{fakeSkill{}}}
	bare.tools = bare.flattenTools()

	loaded := &Agent{Skills: []Skill{fakeSkill{tools: []Tool{
		&fakeTool{name: "工具一"}, &fakeTool{name: "工具二"},
	}}}}
	loaded.tools = loaded.flattenTools()

	if loaded.fixedTokens() <= bare.fixedTokens() {
		t.Fatalf("多挂了两个工具，固定开销却没涨：%d vs %d",
			loaded.fixedTokens(), bare.fixedTokens())
	}
}

// 换技能要让缓存作废，否则配好邮箱之后账还是旧的。
func TestSetSkillsResetsFixedTokens(t *testing.T) {
	a := NewAgent(nil, []Skill{fakeSkill{}}, 10, AutoOff)
	before := a.fixedTokens()

	a.SetSkills([]Skill{fakeSkill{tools: []Tool{&fakeTool{name: "新来的"}}}})
	if a.fixedTokens() == before {
		t.Fatal("换了技能，固定开销还是缓存里那个旧数")
	}
}
