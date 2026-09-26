package chat

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

const goldenPrompt = "testdata/prompt.txt"

// 系统提示和【当下】的金标。
//
// **提示是每轮都要重发的成本**，跟工具声明一样（见 TestToolSchemas）。改一个字
// 都该在 diff 里看得见，连带看见自己让它长了多少字节——现在 systemPrompt 是
// 14.9KB，每轮原价重发的只有末尾几百字节，这个比例是 868d717 那次重排换来的，
// 很容易被一次「顺手补一句规则」悄悄破坏。
//
// 它同时是这批行为改动的**唯一确定性覆盖**：改提示词不会有任何编译错误，别的
// 测试也不会响。真让模型跑一遍的那种评测在 internal/eval，要钱、要网、不进
// make test。
//
// 更新：go test ./internal/chat/ -run TestPromptGolden -update
func TestPromptGolden(t *testing.T) {
	a := agent.NewAgent(nil, testSkills(), 20, agent.AutoClean)
	a.Profile = "我是谁：明哥\n我在意的：账单与续费、面试与合作邀约"
	// 钉住时间，否则【当下】每分钟都不一样。跟 testSkills 用同一个时钟——
	// 日程的 Describe 也会印日期，两处不一致的话金标照样不稳。
	a.Now = func() time.Time { return testClock }

	sys := a.SystemPromptForTest()
	now := a.StateNoteForTest()
	got := fmt.Sprintf(""+
		"# 系统提示（%d 字节，整段会话里逐字节不变，落在服务端缓存前缀里）\n"+
		"%s\n"+
		"# 【当下】（%d 字节，每轮都变，所以垫在消息列表最后）\n"+
		"%s", len(sys), sys, len(now), now)

	if *update {
		if err := os.WriteFile(goldenPrompt, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("更新了金标：系统提示 %d 字节，【当下】%d 字节", len(sys), len(now))
		return
	}
	want, err := os.ReadFile(goldenPrompt)
	if err != nil {
		t.Fatalf("读不到金标（第一次跑加 -update 生成）：%v", err)
	}
	if got != string(want) {
		t.Errorf("提示跟金标不一样。改动是有意的就跑一次：\n"+
			"  go test ./internal/chat/ -run TestPromptGolden -update\n"+
			"然后在 diff 里看清改了什么、长了多少（现在 %d 字节，金标 %d）",
			len(got), len(want))
	}
}
