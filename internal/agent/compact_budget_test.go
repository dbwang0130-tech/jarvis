package agent

import "testing"

// 上下文预算必须把「每一轮都要重发的那部分」算进去：系统提示和工具声明。
//
// 这条以前是漏的——ContextUsage 只数 History，于是工具表一路长到五十几个，
// 每轮白白多发两万字节，而状态栏和压缩阈值都看不见。工具越多漏得越狠，
// 偏偏工具多正是该压缩的时候。
func TestContextUsageCountsPromptAndTools(t *testing.T) {
	a := NewAgent(nil, []Skill{fakeSkill{}}, 10, AutoOff)

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
	bare := NewAgent(nil, []Skill{fakeSkill{}}, 10, AutoOff)
	loaded := NewAgent(nil, []Skill{fakeSkill{tools: []Tool{
		&fakeTool{name: "工具一"}, &fakeTool{name: "工具二"},
	}}}, 10, AutoOff)

	if loaded.fixedTokens() <= bare.fixedTokens() {
		t.Fatalf("多挂了两个工具，固定开销却没涨：%d vs %d",
			loaded.fixedTokens(), bare.fixedTokens())
	}
}

// 老配置里存的是旧口径的数（那会儿只管历史，不含提示和工具声明）。
// 直接拿来用的话，一进门就过压缩线，每轮都在压缩。
func TestStaleContextLimitFallsBackToDefault(t *testing.T) {
	a := NewAgent(nil, []Skill{fakeSkill{}}, 10, AutoOff)
	a.ContextLimit = 16000 // 旧默认值

	// 真实装配下固定开销约九千，16000 连它的两倍都不到。
	if got := a.contextLimit(9000); got != DefaultContextLimit {
		t.Fatalf("装不下的旧值该当没设过，得到 %d", got)
	}
	// 固定开销小的时候，同一个数是够用的，就该照用户说的办。
	if got := a.contextLimit(1000); got != 16000 {
		t.Fatalf("够大的设置该照用，得到 %d", got)
	}
}
