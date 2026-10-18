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
)

// Event 是秘书工作过程中的一条进度信息。
type Event struct {
	Kind EventKind
	// Tool 是相关的工具名（仅工具事件）。
	Tool string
	Text string
	// Pending 仅 EventPending 使用，界面通过它回传是否批准。
	Pending *Pending

	// 以下三项给需要逐个跟踪工具调用的宿主（ACP）用，终端界面只看上面几项。
	//
	// ID 是模型给的工具调用 id，EventToolCall / EventPending 和这次调用的收尾事件
	// 都带它。**每个 EventToolCall 都恰好有一个带同一 ID 的收尾事件**：成功是
	// EventToolResult，失败或被拒绝是 EventError——宿主靠它把卡片收掉，不必猜。
	ID string
	// Args 是 EventToolCall 的原始 JSON 参数（Text 只是压成一行的摘要）。
	Args string
	// Output 是 EventToolResult 的完整结果（Text 只有第一行）。
	Output string
	// Declined 标记 EventError 是用户拒绝了确认，不是执行出错。界面已经在
	// 确认框里看到了自己的决定，不必再报一遍错。
	Declined bool
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
	// ContextLimit 是一次请求的估算 token 预算（提示 + 工具 + 历史）。
	ContextLimit int
	// Usage 累计本次会话的 token 用量。
	Usage Usage

	// Auto 是有风险动作的自动档位。
	Auto AutoLevel
	// Profile 是用户的自述，交给模型判断什么算要紧。
	Profile string
	// Persona 是 Jarvis 自己的人设，空串表示用内置的 DefaultPersona。
	// 它存在 iCloud 里，由调用方读好了塞进来——agent 不碰文件，这跟它
	// 对业务零感知是同一条线。
	Persona string
	// SavePersona 落盘新人设，nil 表示不让改（工具就不注入）。
	// 见 SetPersona。
	SavePersona func(text string) error
	// Extra 是用户放在 skills/ 目录下的扩展技能，只有名字和用途常驻提示，
	// 正文靠 skill 工具按需读。见 extra.go。
	Extra []ExtraSkill
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

// flattenTools 把所有技能的工具按名字拍平，重名的后来者跳过。
//
// 原先它开头先塞两个内置工具 plan / step_done。那两个已经删了——`Agent.Plan`
// 除了 plan.go 自己没有任何读点，也就是说它们喂的那个进度展示压根不存在。代价却是每轮 239 token 的声明、提示里一条规则，以及模型
// 照做时一个三步任务要多四次完整往返（每次都带着整份提示和全部历史）。
// 更糟的是 plan 返回给模型的那句「已展示给用户」是假话，而系统提示里写着
// 「绝不说自己做了没做的事」。
//
// 干活的进度界面一直有：`EventToolCall` 那串事件（终端的状态行、ACP 的工具
// 卡片），不需要模型再花一次调用去声明。
func (a *Agent) flattenTools() []Tool {
	var out []Tool
	seen := map[string]bool{}
	// 人设可写才给这个工具。存不下来还摆着它，模型会回一句「已经改好了」
	// 而下一轮人设原样——见 SetPersona。
	if a.SavePersona != nil {
		out = append(out, personaTool{a: a})
		seen["persona"] = true
	}
	// 一份扩展技能都没有就别摆这个工具：摆了模型会去试，试出来一句
	// 「没有任何扩展技能」，白花一个来回。
	if len(a.Extra) > 0 {
		out = append(out, skillTool{a: a})
		seen["skill"] = true
	}
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

// NoteInterrupted 在上下文里记一笔：上一条指令被用户叫停了。
// 界面会把那条消息退回输入框，用户多半要改了重发，
// 模型得知道前一条没干完，别当成已经完成。
func (a *Agent) NoteInterrupted() {
	a.History = append(a.History, Message{Role: "system",
		Content: "用户中途叫停了上一条指令，它没有执行完。已经做过的动作保留，没做的就别当成做过了。"})
}

// systemPrompt 生成统一系统提示。
//
// **顺序是按「变不变」排的，不是按「重不重要」排的。** 前面是整段会话里一个字
// 都不变的部分（人设、能力清单、各技能的行为规则、通用规则、档位、用户自述），
// 最后才是每一轮都不一样的【当下】（几点几号、各技能的环境快照）。
//
// 为的是服务端的前缀缓存。那些兼容 OpenAI 协议的家伙（deepseek、kimi 这些）
// 都按「跟上一次请求的最长公共前缀」计费，命中的那段便宜一个数量级。原先
// 「现在是 … 14:21」排在第二行——它每分钟都变，于是从第二行往后全都命不中，
// 能缓存的只有人设那两百来字节，后面 10KB 行为规则每轮原样重发、一分钱便宜
// 都占不到。挪到最后之后，可缓存的前缀从两百字节变成两万多。
//
// 所以**往这儿加东西之前先问一句：它会不会每轮都变？** 会变的只能往最后那段
// 里加；加在前面等于把它后面所有东西的缓存一起废掉。
//
// 行为规则那部分有意分成「办事 / 说话 / 出岔子」三块。混在一张清单里的话，
// 模型容易只照顾到前几条；分块之后每一块都短，也好往里加。
func (a *Agent) systemPrompt() string {
	var b strings.Builder

	// 人设从 iCloud 里的 persona.md 来（见 persona.go）。没配、没同步下来、
	// 或者被清空，都退回 DefaultPersona——这一段要是空了，秘书当场
	// 变成一个没身份的模型，而原因在文件系统那头，最难猜。
	b.WriteString(a.personaText())
	b.WriteString("\n")

	// 说清楚这一轮手上有什么。不说的话，模型会照着「一个全能管家」去应承，
	// 张口就是「我去看看邮件」——而没配邮箱时根本没带邮件这项能力。
	if names := a.skillNames(); len(names) > 0 {
		fmt.Fprintf(&b, "这一轮你能用的能力：%s。\n", strings.Join(names, "、"))
		b.WriteString("没列出来的事就是做不到，直说做不到，别应承也别绕。\n\n")
	} else {
		b.WriteString("这一轮你没有任何工具，只能聊天。\n\n")
	}

	// 这儿只写行为规则。环境快照（余额、未读数、最近一次体重…）每轮都变，
	// 统一收进最后那段【当下】——夹在规则中间的话，它后面的规则就全都
	// 命不中缓存了。
	for _, s := range a.Skills {
		fmt.Fprintf(&b, "【%s】\n", s.Name())
		b.WriteString(s.Instructions())
		b.WriteString("\n\n")
	}

	a.describeExtra(&b)

	b.WriteString(`办事：
- 先从上文找答案，再考虑发问。用户说话短、带指代（「那个」「更新一下」「日程里面」），
  多半是接着刚才的事说的——把刚做完的、刚汇报过的当成上下文，自己接上。
- 能靠一次查询弄清的就去查，别拿问题把活推回给用户。只有在确实不知道指哪一件、
  而且猜错了代价大（删东西、发信）时才反问。
- 用户说「刷新」「重新拉」「看看有没有新邮件」时，必须真去服务器同步，不许拿旧清单敷衍。

说话：
- 干完用一两句话说你做了什么，说人话，不要罗列引用和工具名。
- 别把用户已经看得见的东西再抄一遍——他手上就有那份清单。
- 答案短就短。别为了显得认真加小标题、加总结段、加「希望对您有帮助」。

出岔子：
- **说「这里好像不对」之前先自己查一遍。** 最后那条【当下】里的环境快照是**这一轮开始时**的，
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

	b.WriteString("\n最后一条规矩：**动手查之前，先用一句中文说你要干什么**，像这样——\n")
	b.WriteString("「我看一下这周的日程。」「先去服务器收一趟信。」「查查上个月这笔账。」\n")
	b.WriteString("一句话，句号收尾，然后就去调工具。第一个字必须是中文。\n")

	return b.String()
}

// stateNote 是【当下】那条消息：几点几号，加各技能的环境快照。
//
// **它是消息列表的最后一条，不是系统提示的最后一段。** 这两者差别很大：
// 请求的前缀是「工具声明 + system + 历史…」，服务端按跟上一次的最长公共前缀
// 算便宜价。这段东西每轮都变，只要它待在 system 里，**它后面的整段对话历史
// 就全都命不中缓存**——而长会话里历史能占两万多 token，那才是钱的大头。
// 挪到最后之后，system 和历史都落在公共前缀里，每轮按原价的只有这几百字节
// 加上人新说的那句话。
//
// 用 system 角色而不是 user：它不是人说的话，标成 user 的话模型会去回应它
// （「你告诉我现在几点，所以呢？」）。
//
// **它不进 History。** 进了的话，上一轮的旧快照会永远留在上下文里，几轮之后
// 就是一串互相矛盾的余额；而且历史一变，缓存又废了。
//
// 各技能的规则里要指回这儿的话，说「【当下】里那张…」，别说「上面那张」：
// 它现在在最后面。用名字指而不是用方向指，下次再调顺序也不会说错——
// `internal/chat/label_test.go` 的 TestNoSkillPointsUpwards 守着这条。
func (a *Agent) stateNote() Message {
	var sb strings.Builder
	b := &sb
	b.WriteString("【当下】\n")

	// 几点几号是所有场景都要的，不能挂在某个技能下面——只配了邮箱、没加载
	// 日程的那一轮，模型会连今天几号都不知道，「明天」「这周」全算错。
	now := a.now()
	fmt.Fprintf(b, "现在是 %s %s（%s）。\n",
		now.Format("2006-01-02"), weekdayCN(now), now.Format("15:04"))

	for _, s := range a.Skills {
		var one strings.Builder
		s.Describe(&one)
		if strings.TrimSpace(one.String()) == "" {
			continue
		}
		fmt.Fprintf(b, "\n【%s】\n", s.Name())
		b.WriteString(one.String())
	}
	return Message{Role: "system", Content: sb.String()}
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
// 看那张图（「把小票上的金额记下来」是两步）。但图片不进会话存档
// （Images 是 `json:"-"`），所以恢复会话时它们不会跟着回来。
func (a *Agent) AskWith(ctx context.Context, input string, images []string,
	emit func(Event)) (string, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	a.maybeCompact(ctx, emit)

	a.History = append(a.History, Message{Role: "user", Content: input, Images: images})
	tools := a.wireTools()

	// **系统提示和【当下】都在循环外算一次。**
	//
	// 不是为了省那点拼字符串的时间，是**语义要求它们在一轮里不变**：【当下】
	// 那段快照，各技能的规则里明写着「是这一轮开始时的」，「别拿它跟工具刚返回
	// 的数对不上就报账目异常」这条也建立在它不变上。放在循环里每步重建的话，
	// 第二步看到的余额已经包含了第一步刚记的那笔——它自称的那句话就成了假话。
	//
	// 顺带省掉的活不小：每建一次要跑全部 Describe，账本余额是不带缓存的全账本
	// 重算，健康那边 13 项体征各过一遍历史。MaxSteps 默认二十，一次
	// 「记一笔顺手查一下」原先要重算十来遍。
	//
	// 系统提示同理：这一轮里它一个字都不会变（只随 SetSkills / SetPersona /
	// SetExtraSkills / Auto 变，那些都发生在轮次之间）。
	head := Message{Role: "system", Content: a.systemPrompt()}
	now := a.stateNote()

	for step := 0; step < a.MaxSteps; step++ {
		// 稳定的 system 在前、历史居中、每轮都变的【当下】垫在最后——
		// 这个顺序是为了服务端的前缀缓存，见 stateNote。
		msgs := append([]Message{head}, a.History...)
		msgs = append(msgs, now)

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
				Text: describeCall(call), ID: call.ID, Args: call.Function.Arguments})
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
		err     error
	}
	results := make([]res, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func(i int, c ToolCall) {
			defer wg.Done()
			t := a.lookupTool(c.Function.Name)
			text, err := t.Run(c.Function.Arguments)
			results[i] = res{content: text, err: err}
		}(i, call)
	}
	wg.Wait()
	for i, call := range calls {
		r := results[i]
		content := r.content
		if r.err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: r.err.Error(), ID: call.ID})
			content = "执行失败：" + r.err.Error()
		} else {
			emit(Event{Kind: EventToolResult, Tool: call.Function.Name,
				Text: firstLine(r.content), ID: call.ID, Output: r.content})
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
		emit(Event{Kind: EventError, Tool: call.Function.Name, Text: msg, ID: call.ID})
		return msg
	}

	if !a.Auto.Allows(t.Risk()) {
		title, detail, err := t.Preview(call.Function.Arguments)
		if err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error(), ID: call.ID})
			return "执行失败：" + err.Error()
		}
		p := &Pending{Tool: t.Spec().Name, Title: title, Detail: detail,
			Risk: t.Risk(), Reply: make(chan bool, 1)}
		p.run = func() (string, error) { return t.Run(call.Function.Arguments) }
		emit(Event{Kind: EventPending, Tool: p.Tool, Text: p.Title, Pending: p, ID: call.ID})
		if !<-p.Reply {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: "用户拒绝了这个操作",
				ID: call.ID, Declined: true})
			return "用户拒绝了这个操作，没有执行。请不要重试，改问他想怎么办。"
		}
		result, err := p.Do()
		if err != nil {
			emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error(), ID: call.ID})
			return "执行失败：" + err.Error()
		}
		emit(Event{Kind: EventToolResult, Tool: call.Function.Name, Text: firstLine(result),
			ID: call.ID, Output: result})
		return result
	}

	// 自动放行的有风险动作仍要留痕。
	if t.Risk() != RiskNone {
		if title, _, err := t.Preview(call.Function.Arguments); err == nil && title != "" {
			emit(Event{Kind: EventAuto, Text: title})
		}
	}

	result, err := t.Run(call.Function.Arguments)
	if err != nil {
		emit(Event{Kind: EventError, Tool: call.Function.Name, Text: err.Error(), ID: call.ID})
		return "执行失败：" + err.Error()
	}
	emit(Event{Kind: EventToolResult, Tool: call.Function.Name, Text: firstLine(result),
		ID: call.ID, Output: result})
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

// DescribeCall 把工具参数压成一行，给要自己回放历史的宿主（ACP）用。
func DescribeCall(call ToolCall) string { return describeCall(call) }

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

// SystemPromptForTest 把系统提示交出去给别的包量一量（缓存前缀占多少）。
// 只给测试用——真正拼请求的地方在 Ask 里，不走这儿。
func (a *Agent) SystemPromptForTest() string { return a.systemPrompt() }

// StateNoteForTest 同上，把【当下】那条交出去量一量。
func (a *Agent) StateNoteForTest() string { return a.stateNote().Content }
