package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 下面这些用例都不碰网络：往 Client.HTTP 里塞一个自制的 Transport，
// 按预设脚本一轮轮地"回答"。测的是工具调用循环本身——它是整个程序的
// 心脏，出错的样子是秘书自作主张或者干脆停摆。

// scriptedTransport 按顺序把预设的回复发回去，并记下每次请求的内容。
type scriptedTransport struct {
	replies []string
	seen    []chatRequest
	n       int
}

func (t *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// 真的 Transport 会先看 ctx，假的也得看，不然测不出打断。
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	var got chatRequest
	if body, err := io.ReadAll(req.Body); err == nil {
		_ = json.Unmarshal(body, &got)
	}
	t.seen = append(t.seen, got)

	if t.n >= len(t.replies) {
		return nil, fmt.Errorf("脚本用完了，第 %d 次请求没有对应的回复", t.n+1)
	}
	body := t.replies[t.n]
	t.n++
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// say 造一句纯文本回复。
func say(text string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"total_tokens":7}}`, text)
}

// callTool 造一个工具调用回复。
func callTool(name, args string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"c1","type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, name, args)
}

// fakeTool 是个可以摆布的工具：风险等级、是否只读、跑多久、返回什么都能定。
type fakeTool struct {
	name     string
	risk     Risk
	readOnly bool
	took     time.Duration
	calls    atomic.Int32
}

func (t *fakeTool) Spec() ToolSpec {
	return ToolSpec{Name: t.name, Description: "测试用", Parameters: map[string]any{}}
}
func (t *fakeTool) Risk() Risk     { return t.risk }
func (t *fakeTool) ReadOnly() bool { return t.readOnly }
func (t *fakeTool) Preview(string) (string, string, error) {
	return "要对「" + t.name + "」动手了", "细节若干", nil
}
func (t *fakeTool) Run(string) (string, error) {
	t.calls.Add(1)
	time.Sleep(t.took)
	return t.name + " 干完了", nil
}

type fakeSkill struct{ tools []Tool }

func (fakeSkill) Name() string                { return "测试" }
func (fakeSkill) Describe(b *strings.Builder) { b.WriteString("没什么可说的\n") }
func (fakeSkill) Instructions() string        { return "照做就是" }
func (s fakeSkill) Tools() []Tool             { return s.tools }

// newTestAgent 造一个按脚本作答的秘书。
func newTestAgent(t *testing.T, auto AutoLevel, replies []string, tools ...Tool) (*Agent, *scriptedTransport) {
	t.Helper()
	tr := &scriptedTransport{replies: replies}
	c := &Client{
		BaseURL:       "http://模型不存在/v1",
		Model:         "测试模型",
		SupportsTools: true,
		HTTP:          &http.Client{Transport: tr},
	}
	a := NewAgent(c, []Skill{fakeSkill{tools: tools}}, 10, auto)
	a.Stream = false      // 非流式，脚本就是普通 JSON
	a.AutoCompact = false // 别让压缩混进来
	return a, tr
}

func TestAskReturnsPlainAnswer(t *testing.T) {
	a, tr := newTestAgent(t, AutoOff, []string{say("好的，先生。")})

	got, err := a.Ask(context.Background(), "在吗", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "好的，先生。" {
		t.Fatalf("答复是 %q", got)
	}
	if len(tr.seen) != 1 {
		t.Fatalf("只该问模型一次，实际 %d 次", len(tr.seen))
	}
	// 第一条永远是系统提示；**最后一条是【当下】**，为了服务端的前缀缓存
	// 它垫在整个列表末尾（见 stateNote），用户那句排在它前面。
	msgs := tr.seen[0].Messages
	if msgs[0].Role != "system" {
		t.Fatalf("第一条该是 system，实际 %q", msgs[0].Role)
	}
	if last := msgs[len(msgs)-1]; last.Role != "system" ||
		!strings.Contains(last.Content, "【当下】") {
		t.Fatalf("最后一条该是【当下】：%+v", last)
	}
	if user := msgs[len(msgs)-2]; user.Role != "user" || user.Content != "在吗" {
		t.Fatalf("倒数第二条该是用户那句：%+v", user)
	}
}

// 工具跑完，结果要回灌给模型再问一轮。
func TestToolResultGoesBackToModel(t *testing.T) {
	tool := &fakeTool{name: "查日程", readOnly: true}
	a, tr := newTestAgent(t, AutoOff,
		[]string{callTool("查日程", "{}"), say("今天三件事。")}, tool)

	got, err := a.Ask(context.Background(), "今天干啥", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "今天三件事。" {
		t.Fatalf("答复是 %q", got)
	}
	if n := tool.calls.Load(); n != 1 {
		t.Fatalf("工具该跑一次，实际 %d 次", n)
	}
	if len(tr.seen) != 2 {
		t.Fatalf("该问两轮，实际 %d 轮", len(tr.seen))
	}
	// 第二轮里必须带着工具的结果。
	var found bool
	for _, m := range tr.seen[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "查日程 干完了") {
			found = true
		}
	}
	if !found {
		t.Fatal("第二轮没把工具结果带上")
	}
}

// 有风险的动作在「逐项确认」档要停下来等人点头。
func TestRiskyToolWaitsForApproval(t *testing.T) {
	tool := &fakeTool{name: "发信", risk: RiskIrreversible}
	a, _ := newTestAgent(t, AutoOff,
		[]string{callTool("发信", "{}"), say("发出去了。")}, tool)

	var pending *Pending
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = a.Ask(context.Background(), "把信发了", func(e Event) {
			if e.Kind == EventPending {
				pending = e.Pending
				e.Pending.Reply <- true
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("卡住了")
	}

	if pending == nil {
		t.Fatal("没有等确认就把信发了")
	}
	if pending.Risk != RiskIrreversible {
		t.Fatalf("挂起时该带上风险等级，实际 %v", pending.Risk)
	}
	if pending.Title == "" {
		t.Fatal("确认框该有标题")
	}
	if n := tool.calls.Load(); n != 1 {
		t.Fatalf("点头之后该执行一次，实际 %d 次", n)
	}
}

// 用户摇头，工具就不能跑，而且要明确告诉模型别重试。
func TestRejectedToolDoesNotRun(t *testing.T) {
	tool := &fakeTool{name: "清理邮件", risk: RiskReversible}
	a, tr := newTestAgent(t, AutoOff,
		[]string{callTool("清理邮件", "{}"), say("那就不动了。")}, tool)

	_, err := a.Ask(context.Background(), "清一下", func(e Event) {
		if e.Kind == EventPending {
			e.Pending.Reply <- false
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := tool.calls.Load(); n != 0 {
		t.Fatalf("被拒绝了还是跑了 %d 次", n)
	}
	var told bool
	for _, m := range tr.seen[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "拒绝") {
			told = true
		}
	}
	if !told {
		t.Fatal("没告诉模型用户拒绝了")
	}
}

// 档位决定哪些动作免确认。
func TestAutoLevelGatesRisk(t *testing.T) {
	cases := []struct {
		auto    AutoLevel
		risk    Risk
		wantAsk bool
	}{
		{AutoOff, RiskNone, false},
		{AutoOff, RiskReversible, true},
		{AutoOff, RiskIrreversible, true},
		{AutoClean, RiskReversible, false},
		{AutoClean, RiskIrreversible, true},
		{AutoAll, RiskIrreversible, false},
	}
	for _, c := range cases {
		tool := &fakeTool{name: "动作", risk: c.risk}
		a, _ := newTestAgent(t, c.auto,
			[]string{callTool("动作", "{}"), say("完事。")}, tool)

		var asked bool
		_, err := a.Ask(context.Background(), "动手", func(e Event) {
			if e.Kind == EventPending {
				asked = true
				e.Pending.Reply <- true
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if asked != c.wantAsk {
			t.Errorf("档位 %v + 风险 %v：问了吗=%v，该问吗=%v",
				c.auto, c.risk, asked, c.wantAsk)
		}
		if n := tool.calls.Load(); n != 1 {
			t.Errorf("档位 %v + 风险 %v：该执行一次，实际 %d 次", c.auto, c.risk, n)
		}
	}
}

// 一批只读工具可以并发跑；掺一个会改状态的就得老老实实排队。
func TestReadOnlyToolsRunInParallel(t *testing.T) {
	const delay = 80 * time.Millisecond
	two := `{"choices":[{"message":{"role":"assistant","tool_calls":[
		{"id":"a","type":"function","function":{"name":"甲","arguments":"{}"}},
		{"id":"b","type":"function","function":{"name":"乙","arguments":"{}"}}]}}]}`

	t.Run("都只读就并发", func(t *testing.T) {
		a, _ := newTestAgent(t, AutoOff, []string{two, say("好了。")},
			&fakeTool{name: "甲", readOnly: true, took: delay},
			&fakeTool{name: "乙", readOnly: true, took: delay})
		start := time.Now()
		if _, err := a.Ask(context.Background(), "查", nil); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > delay*2 {
			t.Fatalf("两个只读工具花了 %v，看着是排队跑的", took)
		}
	})

	t.Run("有一个会改状态就排队", func(t *testing.T) {
		a, _ := newTestAgent(t, AutoAll, []string{two, say("好了。")},
			&fakeTool{name: "甲", readOnly: true, took: delay},
			&fakeTool{name: "乙", readOnly: false, took: delay})
		start := time.Now()
		if _, err := a.Ask(context.Background(), "改", nil); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took < delay*2 {
			t.Fatalf("只花了 %v，不该并发", took)
		}
	})
}

// 模型要是一直调工具不给结论，得有个刹车。
func TestMaxStepsStopsRunawayLoop(t *testing.T) {
	tool := &fakeTool{name: "转圈", readOnly: true}
	replies := make([]string, 20)
	for i := range replies {
		replies[i] = callTool("转圈", "{}")
	}
	a, _ := newTestAgent(t, AutoOff, replies, tool)
	a.MaxSteps = 3

	_, err := a.Ask(context.Background(), "开始", nil)
	if err == nil {
		t.Fatal("一直调工具却没有报错")
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("错误里该提到轮数上限：%v", err)
	}
	if n := tool.calls.Load(); n != 3 {
		t.Fatalf("该在第 3 轮刹住，实际跑了 %d 次", n)
	}
}

// 模型报了个不存在的工具名，要如实回给它，而不是崩掉。
func TestUnknownToolIsReportedBack(t *testing.T) {
	a, tr := newTestAgent(t, AutoOff,
		[]string{callTool("查天气", "{}"), say("这个我做不了。")})

	if _, err := a.Ask(context.Background(), "天气如何", nil); err != nil {
		t.Fatal(err)
	}
	var told bool
	for _, m := range tr.seen[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "查天气") {
			told = true
		}
	}
	if !told {
		t.Fatal("没把「没有这个工具」回给模型")
	}
}

// 打断之后要能看出是被取消的，而不是普通报错。
func TestCancelIsRecognizable(t *testing.T) {
	a, _ := newTestAgent(t, AutoOff, []string{say("来不及说完")})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Ask(ctx, "说点什么", nil)
	if err == nil {
		t.Fatal("取消了却没报错")
	}
	if !IsCanceled(err) {
		t.Fatalf("该认得出是取消：%v", err)
	}
}

// 换档位要在上下文里留一句，好让模型知道规矩变了。
func TestSetAutoTellsTheModel(t *testing.T) {
	a, _ := newTestAgent(t, AutoOff, nil)
	a.SetAuto(AutoAll)

	if len(a.History) != 1 || a.History[0].Role != "system" {
		t.Fatalf("该往上下文里记一句：%+v", a.History)
	}
	before := len(a.History)
	a.SetAuto(AutoAll) // 没变就不用再说一遍
	if len(a.History) != before {
		t.Fatal("档位没变还是记了一笔")
	}
}

// 工具表里重名的只留第一个。
//
// 原先这条还断言 plan/step_done 排在最前——那两个内置工具已经删了（它们喂的
// 进度展示压根不存在，见 flattenTools 的说明）。
func TestToolTableIsStable(t *testing.T) {
	a, _ := newTestAgent(t, AutoOff, nil,
		&fakeTool{name: "甲"}, &fakeTool{name: "甲"}, &fakeTool{name: "乙"})

	names := make([]string, 0, len(a.tools))
	for _, tl := range a.tools {
		names = append(names, tl.Spec().Name)
	}
	var jia int
	for _, n := range names {
		if n == "甲" {
			jia++
		}
	}
	if jia != 1 {
		t.Fatalf("重名的工具该只留一个，实际 %d 个：%v", jia, names)
	}
}

// 流式那条路要能把增量拼回完整答复。
func TestStreamAssemblesDeltas(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"好的\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"，先生。\"}}]}\n\n" +
		"data: [DONE]\n\n"
	tr := &scriptedTransport{replies: []string{sse}}
	c := &Client{BaseURL: "http://模型不存在/v1", Model: "测试模型",
		SupportsTools: true, HTTP: &http.Client{Transport: tr}}
	a := NewAgent(c, []Skill{fakeSkill{}}, 10, AutoOff)
	a.AutoCompact = false

	var got bytes.Buffer
	answer, err := a.Ask(context.Background(), "在吗", func(e Event) {
		if e.Kind == EventDelta {
			got.WriteString(e.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "好的，先生。" {
		t.Fatalf("拼出来的答复是 %q", answer)
	}
	if got.String() != "好的，先生。" {
		t.Fatalf("增量事件拼出来是 %q", got.String())
	}
}

// 系统提示是这个 app 行为的地基，几条要害得钉住——改提示词很容易顺手
// 把某一条挤掉，而挤掉之后没有任何编译错误或测试失败会提醒你。
func TestSystemPrompt(t *testing.T) {
	at := time.Date(2026, 9, 17, 14, 30, 0, 0, time.Local)
	a := &Agent{
		Now:    func() time.Time { return at },
		Skills: []Skill{fakeSkill{}},
		Auto:   AutoClean,
	}
	p := a.systemPrompt()

	// 当下几点几号是所有场景都要的。只配了邮箱、没加载日程的那一轮，
	// 模型会连今天几号都不知道，「明天」「这周」全算错。
	//
	// **它不在系统提示里，在最后那条【当下】里**——每分钟都变的东西留在
	// 系统提示里，会把它后面所有内容的服务端缓存一起废掉。见 stateNote。
	now := a.stateNote().Content
	for _, want := range []string{"2026-09-17", "周四", "14:30"} {
		if !strings.Contains(now, want) {
			t.Errorf("【当下】里没有 %q：\n%s", want, now)
		}
	}
	if strings.Contains(p, "14:30") {
		t.Error("时间又回到系统提示里了——它每分钟都变，会废掉后面全部缓存")
	}

	// 手上有什么能力要说明白，不然模型会照着「全能管家」应承，
	// 张口就是「我去看看邮件」——而这一轮根本没带邮件。
	if !strings.Contains(p, "测试") {
		t.Errorf("没列出这一轮的能力：\n%s", p)
	}
	if !strings.Contains(p, "做不到") {
		t.Errorf("没说清楚没列出来的事做不到：\n%s", p)
	}

	// 技能自己的环境和规则要带上。
	if !strings.Contains(p, "照做就是") {
		t.Errorf("技能的规则没带上：\n%s", p)
	}

	// 最要紧的那条：不许说自己做了没做的事。
	if !strings.Contains(p, "绝不说自己做了没做的事") {
		t.Errorf("诚实那条丢了：\n%s", p)
	}

	if !strings.Contains(p, "自动整理档") {
		t.Errorf("没说清楚当前档位：\n%s", p)
	}
}

// 一个工具都没有的时候别装得像什么都能干。
func TestSystemPromptWithoutSkills(t *testing.T) {
	a := &Agent{Now: func() time.Time { return time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local) }}
	p := a.systemPrompt()
	if !strings.Contains(p, "没有任何工具") {
		t.Errorf("没工具时该说清楚：\n%s", p)
	}
}

// 用户自述只在填了的时候出现，空的不该留个空标题在那儿。
func TestSystemPromptProfile(t *testing.T) {
	a := &Agent{Now: time.Now, Skills: []Skill{fakeSkill{}}}
	if strings.Contains(a.systemPrompt(), "关于你的用户") {
		t.Error("没填自述却出现了自述那一段")
	}
	a.Profile = "  博士在读，在写毕业论文  "
	p := a.systemPrompt()
	if !strings.Contains(p, "关于你的用户") || !strings.Contains(p, "毕业论文") {
		t.Errorf("自述没带上：\n%s", p)
	}
}

// 每个工具调用都恰好收一次尾，且带同一个 ID——ACP 那边靠它关卡片。
// 成功（含空结果）、执行失败、被拒绝三条路都要覆盖到。
func TestEveryToolCallIsClosedOnce(t *testing.T) {
	cases := []struct {
		name    string
		tool    *fakeTool
		approve bool
		want    EventKind
	}{
		{"自动放行", &fakeTool{name: "查日程"}, true, EventToolResult},
		{"点头后执行", &fakeTool{name: "发信", risk: RiskIrreversible}, true, EventToolResult},
		{"被拒绝", &fakeTool{name: "发信", risk: RiskIrreversible}, false, EventError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, _ := newTestAgent(t, AutoOff, []string{callTool(c.tool.name, `{"x":1}`), say("好了。")}, c.tool)
			var calls, closes []Event
			_, err := a.Ask(context.Background(), "办一下", func(e Event) {
				switch e.Kind {
				case EventToolCall:
					calls = append(calls, e)
				case EventToolResult, EventError:
					closes = append(closes, e)
				case EventPending:
					if e.ID != "c1" {
						t.Errorf("确认事件该带调用 id，实际 %q", e.ID)
					}
					e.Pending.Reply <- c.approve
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Args != `{"x":1}` {
				t.Fatalf("工具调用事件不对：%+v", calls)
			}
			if len(closes) != 1 || closes[0].ID != "c1" || closes[0].Kind != c.want {
				t.Fatalf("收尾事件不对：%+v", closes)
			}
			if c.want == EventToolResult && closes[0].Output == "" {
				t.Fatal("成功的收尾该带完整结果")
			}
			if !c.approve && !closes[0].Declined {
				t.Fatal("拒绝要标成 Declined")
			}
		})
	}
}
