package chat

import (
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// 拿真技能量一遍：前后两次请求的系统提示，公共前缀占多少。
// 那一段才是按缓存价计费的。-v 看数字。
func TestCachePrefixRatioWithRealSkills(t *testing.T) {
	skills := testSkills()
	a := agent.NewAgent(nil, skills, 20, agent.AutoClean)
	t0 := time.Date(2026, 9, 26, 14, 21, 0, 0, time.Local)
	a.Now = func() time.Time { return t0 }
	first := a.SystemPromptForTest()
	firstNow := a.StateNoteForTest()
	a.Now = func() time.Time { return t0.Add(time.Minute) }
	second := a.SystemPromptForTest()

	if first != second {
		t.Fatalf("系统提示变了——它后面整段历史的缓存都会跟着废掉")
	}
	t.Logf("系统提示 %d 字节，整段会话里逐字节不变，跟对话历史一起落在缓存前缀里；"+
		"每轮按原价的只有末尾【当下】那 %d 字节加人新说的那句话",
		len(first), len(firstNow))
}
