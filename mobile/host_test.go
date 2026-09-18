package mobile

import (
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

type fakeHost struct {
	steps  []string
	answer bool
	asked  []string
}

func (h *fakeHost) Step(kind, name, label, text string) {
	h.steps = append(h.steps, kind+":"+name+":"+label)
}

func (h *fakeHost) Confirm(tool, title, detail string, irreversible bool) bool {
	h.asked = append(h.asked, tool)
	return h.answer
}

// EventPending 之后 agent 立刻阻塞在 p.Reply 上收答复。emit 里不回话
// 就是整轮卡死——手机上表现为转圈转到天荒地老。这条守着那个出口。
func TestPendingAlwaysAnswered(t *testing.T) {
	cases := []struct {
		name string
		host *fakeHost
		want bool
	}{
		{"没有界面回调就是不批", nil, false},
		{"用户点了同意", &fakeHost{answer: true}, true},
		{"用户点了算了", &fakeHost{answer: false}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var steps []Step
			var host Host
			if tc.host != nil {
				host = tc.host
			}
			emit := hostEmit(host, &steps)
			p := &agent.Pending{
				Tool: "send_draft", Title: "发给老板",
				Risk: agent.RiskIrreversible, Reply: make(chan bool, 1),
			}

			done := make(chan bool, 1)
			go func() {
				emit(agent.Event{Kind: agent.EventPending, Tool: p.Tool, Pending: p})
				// agent 就是这么收答复的。
				done <- <-p.Reply
			}()

			select {
			case got := <-done:
				if got != tc.want {
					t.Errorf("答复是 %v，该是 %v", got, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("没人答复，卡死了")
			}
		})
	}
}

// Pending 为 nil 时也不能炸——真出了这种事，卡死比崩了更难查。
func TestPendingNilSafe(t *testing.T) {
	var steps []Step
	emit := hostEmit(&fakeHost{}, &steps)
	emit(agent.Event{Kind: agent.EventPending, Tool: "x", Pending: nil})
}

// 进度既要攒进出参（给没接回调的老界面），也要实时报给界面。
func TestStepsGoBothWays(t *testing.T) {
	var steps []Step
	h := &fakeHost{}
	emit := hostEmit(h, &steps)
	emit(agent.Event{Kind: agent.EventToolCall, Tool: "list_mail", Text: "收件箱"})
	emit(agent.Event{Kind: agent.EventToolResult, Tool: "list_mail", Text: "3 封"})
	emit(agent.Event{Kind: agent.EventAuto, Tool: "clean_mail", Text: "清理了 2 封"})

	if len(steps) != 3 {
		t.Fatalf("出参里该有 3 步，实际 %d", len(steps))
	}
	if steps[2].Kind != "auto" {
		t.Errorf("自动做掉的动作该标成 auto：%+v", steps[2])
	}
	if len(h.steps) != 3 {
		t.Errorf("界面该收到 3 步，实际 %v", h.steps)
	}
}
