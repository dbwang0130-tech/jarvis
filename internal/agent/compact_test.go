package agent

import (
	"context"
	"strings"
	"testing"
)

// 压缩这一块原先只有「预算口径」四条测试（compact_budget_test.go），
// Compact 本身一条都没有。下面几条钉的是它的不变量——坏起来的样子分别是
// 服务端整个拒掉请求、对话凭空少一段、以及缓存前缀白瞎。

// 造一段带工具调用的历史：三轮，每轮都是
// user → assistant(tool_calls) → tool → assistant(答复)。
func historyWithTools(turns int) []Message {
	var h []Message
	for i := 1; i <= turns; i++ {
		n := string(rune('0' + i))
		h = append(h,
			Message{Role: "user", Content: "第" + n + "件事"},
			Message{Role: "assistant", ToolCalls: []ToolCall{{
				ID: "c" + n, Type: "function",
				Function: FunctionCall{Name: "查日程", Arguments: "{}"},
			}}},
			Message{Role: "tool", ToolCallID: "c" + n, Name: "查日程", Content: "查到了" + n},
			Message{Role: "assistant", Content: "办好了" + n},
		)
	}
	return h
}

// pairingOK 检查每条 tool 消息前面都有一个带 tool_calls 的 assistant。
//
// **这条坏了的后果是整个请求被服务端拒掉**（400），而不是答得差一点：
// OpenAI 那套协议要求 tool 消息必须紧跟在声明了对应 tool_calls 的 assistant
// 之后。压缩要是切在一组中间，下一轮请求直接发不出去。
func pairingOK(msgs []Message) string {
	pending := map[string]bool{}
	for i, m := range msgs {
		switch m.Role {
		case "assistant":
			for _, tc := range m.ToolCalls {
				pending[tc.ID] = true
			}
		case "tool":
			if !pending[m.ToolCallID] {
				return "第 " + string(rune('0'+i)) + " 条 tool 消息（" +
					m.ToolCallID + "）前面没有声明它的 assistant"
			}
			delete(pending, m.ToolCallID)
		}
	}
	return ""
}

// 切点只落在 user 消息上，所以工具调用组永远切不散。
func TestCompactKeepsToolPairsIntact(t *testing.T) {
	a, tr := newTestAgent(t, AutoOff, []string{say("这是交接说明：前两件事都办好了。")})
	a.History = historyWithTools(3)

	done, err := a.Compact(context.Background(), 1)
	if err != nil || !done {
		t.Fatalf("压缩没成：done=%v err=%v", done, err)
	}
	if bad := pairingOK(a.History); bad != "" {
		t.Errorf("压缩后的历史配对坏了：%s\n%+v", bad, a.History)
	}
	// **发给模型的那一份也要成对。** 压缩本身就是一次请求，它带的是被切掉
	// 那一段——那一段要是不成对，压缩这一步自己就先失败了。
	if len(tr.seen) == 0 {
		t.Fatal("没发出压缩请求")
	}
	if bad := pairingOK(tr.seen[0].Messages); bad != "" {
		t.Errorf("压缩请求里配对坏了：%s", bad)
	}
}

// 最近那几轮要原样留着——压缩是为了腾地方，不是为了把刚说的话也糊掉。
func TestCompactKeepsRecentTurnsVerbatim(t *testing.T) {
	a, _ := newTestAgent(t, AutoOff, []string{say("交接说明。")})
	a.History = historyWithTools(3)
	tail := append([]Message(nil), a.History[8:]...) // 第三轮那四条

	if _, err := a.Compact(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(a.History) < len(tail) {
		t.Fatalf("历史比该留的还短：%d < %d", len(a.History), len(tail))
	}
	got := a.History[len(a.History)-len(tail):]
	for i := range tail {
		if got[i].Role != tail[i].Role || got[i].Content != tail[i].Content {
			t.Errorf("第 %d 条不是原样：\n得到 %+v\n应是 %+v", i, got[i], tail[i])
		}
	}
	// 头上换成了交接说明。
	if !strings.Contains(a.History[0].Content, "交接说明") {
		t.Errorf("开头不是交接说明：%+v", a.History[0])
	}
}

// **压缩失败时历史必须一个字都不动。**
//
// 这是最要紧的一条：压缩是「先问模型要摘要，再拿摘要换掉旧对话」。要是失败了
// 还把旧的删了，用户说过的话就凭空没了——而他完全不会知道发生了什么，只会发现
// 秘书突然不记得刚才的事。
func TestCompactFailureLeavesHistoryIntact(t *testing.T) {
	cases := []struct {
		name  string
		reply string
	}{
		{"模型给了空摘要", say("   ")},
		{"模型那边报错", `{"error":{"message":"服务器忙"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newTestAgent(t, AutoOff, []string{c.reply})
			a.History = historyWithTools(3)
			before := append([]Message(nil), a.History...)

			done, err := a.Compact(context.Background(), 1)
			if err == nil {
				t.Fatal("该报错")
			}
			if done {
				t.Error("失败了却说压缩成了")
			}
			if len(a.History) != len(before) {
				t.Fatalf("历史被改了：%d → %d 条", len(before), len(a.History))
			}
			for i := range before {
				if a.History[i].Content != before[i].Content {
					t.Errorf("第 %d 条变了：%q → %q",
						i, before[i].Content, a.History[i].Content)
				}
			}
		})
	}
}

// 轮数不够就不压缩，也不能报错——它是每轮都会走一遍的路。
func TestCompactNoopWhenTooShort(t *testing.T) {
	a, tr := newTestAgent(t, AutoOff, nil)
	a.History = historyWithTools(1)
	before := len(a.History)

	done, err := a.Compact(context.Background(), 2)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if done {
		t.Error("只有一轮却说压缩了")
	}
	if len(a.History) != before {
		t.Errorf("历史被动了：%d → %d", before, len(a.History))
	}
	if len(tr.seen) != 0 {
		t.Error("不该压缩却发了请求，白花一次钱")
	}
}

// 压缩之后，那份**逐字节不变的系统提示**和垫在最后的【当下】还得各就各位。
//
// 它们都不在 History 里（见 stateNote 的说明），所以压缩本来就不该碰到它们——
// 但这条是整套服务端缓存布局的地基，值得钉一下：哪天有人把【当下】挪回
// History，压缩会把它一起糊掉，而表现只是「账变贵了」，没人会想到是这儿。
func TestCompactDoesNotDisturbPromptLayout(t *testing.T) {
	a, tr := newTestAgent(t, AutoOff, []string{
		say("交接说明。"),  // 压缩那一次
		say("好的，先生。"), // 接着提问那一次
	})
	a.History = historyWithTools(3)
	sysBefore := a.systemPromptForTest()

	if _, err := a.Compact(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if a.systemPromptForTest() != sysBefore {
		t.Error("压缩把系统提示弄变了——那份逐字节不变是缓存前缀的地基")
	}

	// 再问一句，看真实请求的形状。
	if _, err := a.Ask(context.Background(), "还有什么", func(Event) {}); err != nil {
		t.Fatal(err)
	}
	msgs := tr.seen[len(tr.seen)-1].Messages
	if msgs[0].Role != "system" {
		t.Errorf("第一条不是 system：%+v", msgs[0])
	}
	last := msgs[len(msgs)-1]
	if last.Role != "system" || !strings.Contains(last.Content, "【当下】") {
		t.Errorf("最后一条不是【当下】：%+v", last)
	}
	if bad := pairingOK(msgs); bad != "" {
		t.Errorf("压缩之后的请求配对坏了：%s", bad)
	}
}

// systemPromptForTest 只是包内的一个短名字，省得测试里到处写 SystemPromptForTest。
func (a *Agent) systemPromptForTest() string { return a.systemPrompt() }
