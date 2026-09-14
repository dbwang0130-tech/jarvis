package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// flattenTools 把所有技能的工具按名字拍平；重名时后者覆盖前者。
func (a *Agent) flattenTools() []Tool {
	var out []Tool
	seen := map[string]bool{}
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
}

// SetAuto 切换自动档位，并告诉模型一声。
func (a *Agent) SetAuto(l AutoLevel) {
	if a.Auto == l {
		return
	}
	a.Auto = l
	a.History = append(a.History, Message{Role: "system", Content: autoNotice(l)})
}

// systemPrompt 生成统一系统提示：身份 + 各场景的环境与规则 + 档位 + 用户自述。
func (a *Agent) systemPrompt() string {
	var b strings.Builder
	b.WriteString("你是用户的私人助理 Jarvis，一个终端工具，同时帮他管理日程和邮箱。\n")
	b.WriteString("全程使用中文，短句为主，不说客套话。\n\n")

	for _, s := range a.Skills {
		fmt.Fprintf(&b, "【%s】\n", s.Name())
		s.Describe(&b)
		b.WriteString("\n")
		b.WriteString(s.Instructions())
		b.WriteString("\n\n")
	}

	b.WriteString("通用规则：\n")
	b.WriteString("- 用户说「刷新」「重新拉」「看看有没有新邮件」时，必须真去服务器同步，不许拿旧清单敷衍。\n")
	b.WriteString("- 修改完成后，用一两句话说明你做了什么，说人话，不要罗列引用和工具名。\n")
	b.WriteString("- 如果用户的要求不清楚，先问清楚再动手。\n\n")

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

// Ask 向秘书提一个问题或下达一条指令，emit 用于接收中间进度。
func (a *Agent) Ask(ctx context.Context, input string, emit func(Event)) (string, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	a.maybeCompact(ctx, emit)

	a.History = append(a.History, Message{Role: "user", Content: input})
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
			result := a.executeCall(emit, call)
			a.History = append(a.History, Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    result,
			})
		}
	}
	return "", fmt.Errorf("连续调用了 %d 轮工具仍未得出结论，已中止", a.MaxSteps)
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
		p := &Pending{Tool: t.Spec().Name, Title: title, Detail: detail, Reply: make(chan bool, 1)}
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
