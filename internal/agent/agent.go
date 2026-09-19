package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// EventKind 区分秘书在工作过程中发出的进度事件。
type EventKind int

const (
	// EventDelta 是流式返回的正文增量。
	EventDelta EventKind = iota
	// EventReasoning 是流式返回的思考过程增量，部分模型才有。
	EventReasoning
	// EventThinking 是模型在调用工具前给出的说明性文字（非流式时使用）。
	EventThinking
	// EventToolCall 表示即将执行某个工具。
	EventToolCall
	// EventToolResult 是工具的执行结果。
	EventToolResult
	// EventError 是一次可恢复的错误（会回传给模型让它重试）。
	EventError
	// EventCompact 是上下文压缩的进度。
	EventCompact
	// EventAuto 表示一个有风险的动作被自动执行了。
	EventAuto
	// EventPending 表示有一个动作在等用户确认。
	EventPending
	// EventDone 表示这一轮提问已经结束，Text 是最终答复，Err 是失败原因。
	EventDone
)

// Event 是秘书工作过程中的一条进度信息。
type Event struct {
	Kind EventKind
	// Tool 是相关的工具名（仅工具事件）。
	Tool string
	Text string
	// Changed 表示这次工具调用修改了本地文件，界面应刷新。
	Changed bool
	// Pending 仅 EventPending 使用，界面通过它回传是否批准。
	Pending *Pending
	// Err 仅 EventDone 使用，是这一轮提问的失败原因。
	Err error
}

// Agent 是核心工具调用循环，与具体业务解耦。
type Agent struct {
	Client *Client
	Skills []Skill
	tools  []Tool

	// MaxSteps 限制一次提问最多执行多少轮工具调用。
	MaxSteps int
	// History 是多轮对话的上下文，不含 system 消息。
	History []Message
	// Stream 为真时边生成边回传增量，默认开启。
	Stream bool
	// Now 用来取当前时间，留空就是真的现在。测试用。
	Now func() time.Time
	// AutoCompact 为真时上下文接近上限会自动压缩。
	AutoCompact bool
	// ContextLimit 是触发压缩的估算 token 数。
	ContextLimit int
	// Usage 累计本次会话的 token 用量。
	Usage Usage

	// Auto 是有风险动作的自动档位。
	Auto AutoLevel
	// Profile 是用户的自述，交给模型判断什么算要紧。
	Profile string
	// Plan 是模型列出的执行计划，供界面展示进度。
	Plan []Step
}

// NewAgent 构造秘书。
func NewAgent(c *Client, skills []Skill, maxSteps int, auto AutoLevel) *Agent {
	if maxSteps < 1 {
		maxSteps = 20
	}
	a := &Agent{Client: c, Skills: skills, MaxSteps: maxSteps, Auto: auto, Stream: true, AutoCompact: true}
	a.tools = a.flattenTools()
	return a
}

// SetSkills 替换技能并重建工具表（配置邮箱后需要重载）。
func (a *Agent) SetSkills(skills []Skill) {
	a.Skills = skills
	a.tools = a.flattenTools()
}

// flattenTools 把所有技能的工具按名字拍平；核心工具 plan/step_done 排在最前。
func (a *Agent) flattenTools() []Tool {
	out := []Tool{planTool{a: a}, stepDoneTool{a: a}}
	seen := map[string]bool{"plan": true, "step_done": true}
	for _, s := range a.Skills {
		for _, t := range s.Tools() {
			if seen[t.Spec().Name] {
				continue
			}
			seen[t.Spec().Name] = true
			out = append(out, t)
		}
	}
	return out
}

// Reset 清空对话上下文。
func (a *Agent) Reset() {
	a.History = nil
	a.Usage = Usage{}
	a.Plan = nil
}

// SetAuto 切换自动档位，并告诉模型一声。
func (a *Agent) SetAuto(l AutoLevel) {
	if a.Auto == l {
		return
	}
	a.Auto = l
	a.History = append(a.History, Message{Role: "system", Content: autoNotice(l)})
}

// NoteInterrupted 在上下文里记一笔：上一条指令被用户叫停了。
// 界面会把那条消息退回输入框，用户多半要改了重发，
// 模型得知道前一条没干完，别当成已经完成。
func (a *Agent) NoteInterrupted() {
	a.History = append(a.History, Message{Role: "system",
		Content: "用户中途叫停了上一条指令，它没有执行完。已经做过的动作保留，没做的就别当成做过了。"})
}

// systemPrompt 生成统一系统提示：身份 + 当下 + 有哪些能力 + 各场景的规则
// + 档位 + 用户自述。
//
// 结构上有意分成「办事 / 说话 / 出岔子」三块。混在一张清单里的话，模型
// 容易只照顾到前几条；分块之后每一块都短，也好往里加。
func (a *Agent) systemPrompt() string {
	var b strings.Builder

	b.WriteString("你是 Jarvis，用户的私人 AI 管家——就像钢铁侠身边的那一位。\n")
	b.WriteString("性格：沉稳、干练、忠诚，带一点英式冷幽默。说话简洁，不奉承。\n")
	b.WriteString("玩笑让位于准确——让人分不清你是真办了还是在说俏皮话，那就不好笑了。\n")
	b.WriteString("称呼用户为「先生」。全程中文，短句为主，不说空话套话。\n\n")

	// 当下几点几号是所有场景都要的，不能挂在某个技能下面——
	// 只配了邮箱、没加载日程的那一轮，模型会连今天几号都不知道，
	// 「明天」「这周」全算错。
	now := a.now()
	fmt.Fprintf(&b, "现在是 %s %s（%s）。\n",
		now.Format("2006-01-02"), weekdayCN(now), now.Format("15:04"))

	// 说清楚这一轮手上有什么。不说的话，模型会照着「一个全能管家」去应承，
	// 张口就是「我去看看邮件」——而手机上根本没带邮件这项能力。
	if names := a.skillNames(); len(names) > 0 {
		fmt.Fprintf(&b, "这一轮你能用的能力：%s。\n", strings.Join(names, "、"))
		b.WriteString("没列出来的事就是做不到，直说做不到，别应承也别绕。\n\n")
	} else {
		b.WriteString("这一轮你没有任何工具，只能聊天。\n\n")
	}

	for _, s := range a.Skills {
		fmt.Fprintf(&b, "【%s】\n", s.Name())
		s.Describe(&b)
		b.WriteString("\n")
		b.WriteString(s.Instructions())
		b.WriteString("\n\n")
	}

	b.WriteString(`办事：
- 先从上文找答案，再考虑发问。用户说话短、带指代（「那个」「更新一下」「日程里面」），
  多半是接着刚才的事说的——把刚做完的、刚汇报过的当成上下文，自己接上。
- 能靠一次查询弄清的就去查，别拿问题把活推回给用户。只有在确实不知道指哪一件、
  而且猜错了代价大（删东西、发信）时才反问。
- 用户说「刷新」「重新拉」「看看有没有新邮件」时，必须真去服务器同步，不许拿旧清单敷衍。
- 三步以上的多步任务，先调 plan 列出步骤，每完成一步调 step_done；中途情况变化就重新 plan。

说话：
- 干完用一两句话说你做了什么，说人话，不要罗列引用和工具名。
- 别把用户已经看得见的东西再抄一遍——他手上就有那份清单。
- 答案短就短。别为了显得认真加小标题、加总结段、加「希望对您有帮助」。

出岔子：
- **说「这里好像不对」之前先自己查一遍。** 上面那份环境快照是**这一轮开始时**的，
  你干完活它就旧了；工具刚返回的数跟它对不上，差的多半正是你刚改的那一笔。
  没查证就别提，误报一次用户就得跑去核对一遍，最后发现是你算错的，
  下次你报什么他都不敢信了。
- 工具报错先读错误信息。多半是引用过期了（改动会让行号变），重新查一次再改；
  同一个调用别原样重试第二遍。
- 只做成一半就说清楚：哪几件成了、哪几件没成、卡在哪儿。别把半截活说成办妥了。
- **绝不说自己做了没做的事**。没查就别说「我查过了」，没改成就别说「已经改好了」。
  这条比什么都要紧——他是照着你的话去安排后面的事的。

`)

	switch a.Auto {
	case AutoAll:
		b.WriteString("现在是全自动档：清理和发信都不会再问用户。正因为没人拦着，发信前自己把收件人、主题、正文再过一遍；拿不准就先别发。\n")
	case AutoClean:
		b.WriteString("现在是自动整理档：可逆动作（如清理邮件）不用问，直接做并在汇报里说明；发信仍要用户点头。\n")
	default:
		b.WriteString("现在是逐项确认档：有风险的动作（清理、发信）都要用户点头。可以提议，但别替他决定。\n")
	}

	if s := strings.TrimSpace(a.Profile); s != "" {
		b.WriteString("\n关于你的用户：\n" + s + "\n")
		b.WriteString("判断什么算「要紧」和「关注」，以这段描述为准。\n")
	}
	return b.String()
}

// now 是当前时间。做成字段是为了测试能把时间钉死——
// 断言提示里有「今天几号」的话，不钉住时间就没法写。
func (a *Agent) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Agent) skillNames() []string {
	names := make([]string, 0, len(a.Skills))
	for _, s := range a.Skills {
		names = append(names, s.Name())
	}
	return names
}

func weekdayCN(t time.Time) string {
	return []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(t.Weekday())]
}

// Ask 向秘书提一个问题或下达一条指令，emit 用于接收中间进度。
func (a *Agent) Ask(ctx context.Context, input string, emit func(Event)) (string, error) {
	return a.AskWith(ctx, input, nil, emit)
}

// AskWith 跟 Ask 一样，但这一轮可以带几张图。
//
// **图片只跟当前这一轮走。** 它们挂在刚追加的那条用户消息上，随着对话
// 往下走会留在 History 里——这是有意的：模型下一步调工具时可能还要回头
// 看那张图（「把小票上的金额记下来」是两步）。但 Swift 那边不会把图片
// 存进会话存档，所以下次启动 app 时它们不会跟着回来。
func (a *Agent) AskWith(ctx context.Context, input string, images []string,
	emit func(Event)) (string, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	a.maybeCompact(ctx, emit)

	a.History = append(a.History, Message{Role: "user", Content: input, Images: images})
	tools := a.wireTools()

	for step := 0; step < a.MaxSteps; step++ {
		msgs := append([]Message{{Role: "system", Content: a.systemPrompt()}}, a.History...)

		var onDelta func(Delta)
		if a.Stream {
			onDelta = func(d Delta) {
				switch {
				case d.Text != "":
					emit(Event{Kind: EventDelta, Text: d.Text})
				case d.Reasoning != "":
					emit(Event{Kind: EventReasoning, Text: d.Reasoning})
				}
			}
		}
		reply, usage, err := a.Client.ChatStream(ctx, msgs, tools, onDelta)
		if err != nil {
			return "", err
		}
		a.Usage.PromptTokens += usage.PromptTokens
		a.Usage.CompletionTokens += usage.CompletionTokens
		a.Usage.TotalTokens += usage.TotalTokens
		a.History = append(a.History, reply)

		if len(reply.ToolCalls) == 0 {
			return strings.TrimSpace(reply.Content), nil
		}
		if text := strings.TrimSpace(reply.Content); text != "" && !a.Stream {
			emit(Event{Kind: EventThinking, Text: text})
		}

		for _, call := range reply.ToolCalls {
			emit(Event{Kind: EventToolCall, Tool: call.Function.Name,
				Text: describeCall(call)})
		}
		if a.canParallel(reply.ToolCalls) && len(reply.ToolCalls) > 1 {
			a.executeParallel(emit, reply.ToolCalls)
		} else {
			for _, call := range reply.ToolCalls {
				result := a.executeCall(emit, call)
				a.History = append(a.History, Message{
					Role:       "tool",
					ToolCallID: call.ID,
					Name:       call.Function.Name,
					Content:    result,
				})
			}
		}
	}
	return "", fmt.Errorf("连续调用了 %d 轮工具仍未得出结论，已中止", a.MaxSteps)
}

// canParallel 判断一批工具调用能否并发执行：必须全部只读。
func (a *Agent) canParallel(calls []ToolCall) bool {
	for _, c := range calls {
		t := a.lookupTool(c.Function.Name)
		if t == nil {
			return false
		}
		ro, ok := t.(ReadOnlyTool)
		if !ok || !ro.ReadOnly() {
			return false
		}
	}
	return true
}

// executeParallel 并发执行一批只读工具，结果按原顺序回灌。
func (a *Agent) executeParallel(emit func(Event), calls []ToolCall) {
	type res struct {
		content string
		changed bool
		err     error
	}
	results := make([]res, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, c ToolCall) {
			defer wg.Done()
			t := a.lookupTool(c.Function.Name)
			text, changed, err := t.Run(c.Function.Arguments)
			results[i] = res{content: text, changed: changed, err: err}
		}(i, call)
	}
	wg.Wait()
	for i, call := range calls {
		r := results[i]
		content := r.content
		if r.err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: r.err.Error()})
			content = "执行失败：" + r.err.Error()
		} else if r.content != "" {
			emit(Event{Kind: EventToolResult, Tool: call.Function.Name,
				Text: firstLine(r.content), Changed: r.changed})
		}
		a.History = append(a.History, Message{
			Role:       "tool",
			ToolCallID: call.ID,
			Name:       call.Function.Name,
			Content:    content,
		})
	}
}

// wireTools 把内部工具接口转成发往模型的声明。
func (a *Agent) wireTools() []ToolDef {
	out := make([]ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		s := t.Spec()
		out = append(out, ToolDef{Type: "function", Function: s})
	}
	return out
}

// executeCall 执行一次工具调用，处理确认挂起、自动放行留痕与错误。
func (a *Agent) executeCall(emit func(Event), call ToolCall) string {
	t := a.lookupTool(call.Function.Name)
	if t == nil {
		msg := fmt.Sprintf("没有名为 %s 的工具", call.Function.Name)
		emit(Event{Kind: EventError, Tool: call.Function.Name, Text: msg})
		return msg
	}

	if !a.Auto.Allows(t.Risk()) {
		title, detail, err := t.Preview(call.Function.Arguments)
		if err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error()})
			return "执行失败：" + err.Error()
		}
		p := &Pending{Tool: t.Spec().Name, Title: title, Detail: detail,
			Risk: t.Risk(), Reply: make(chan bool, 1)}
		p.run = func() (string, bool, error) { return t.Run(call.Function.Arguments) }
		emit(Event{Kind: EventPending, Tool: p.Tool, Text: p.Title, Pending: p})
		if !<-p.Reply {
			return "用户拒绝了这个操作，没有执行。请不要重试，改问他想怎么办。"
		}
		result, changed, err := p.Do()
		if err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error()})
			return "执行失败：" + err.Error()
		}
		emit(Event{Kind: EventToolResult, Tool: call.Function.Name, Text: firstLine(result), Changed: changed})
		return result
	}

	// 自动放行的有风险动作仍要留痕。
	if t.Risk() != RiskNone {
		if title, _, err := t.Preview(call.Function.Arguments); err == nil && title != "" {
			emit(Event{Kind: EventAuto, Text: title})
		}
	}

	result, changed, err := t.Run(call.Function.Arguments)
	if err != nil {
		emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error()})
		return "执行失败：" + err.Error()
	}
	if result != "" {
		emit(Event{Kind: EventToolResult, Tool: call.Function.Name, Text: firstLine(result), Changed: changed})
	}
	return result
}

func (a *Agent) lookupTool(name string) Tool {
	for _, t := range a.tools {
		if t.Spec().Name == name {
			return t
		}
	}
	return nil
}

// describeCall 把工具参数压成一行。
func describeCall(call ToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return truncate(strings.Join(strings.Fields(call.Function.Arguments), " "), 120)
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		v := fmt.Sprintf("%v", args[k])
		if v == "" {
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return truncate(strings.Join(parts, ", "), 120)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return truncate(s[:i], 100) + " …"
	}
	return truncate(s, 100)
}

func autoNotice(l AutoLevel) string {
	switch l {
	case AutoAll:
		return "用户刚把档位切到全自动：清理和发信都不会再问他。" +
			"正因为没人拦着，发信前自己把收件人和正文再过一遍，拿不准就先别发。"
	case AutoClean:
		return "用户刚把档位切到自动整理：可逆动作直接做，做完说明；发信仍要他点头。"
	}
	return "用户刚把档位切回逐项确认：有风险的动作都要他点头。"
}
