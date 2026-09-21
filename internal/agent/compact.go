package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultContextLimit 是一次请求的估算 token 预算：系统提示 + 工具声明 +
// 对话历史，三样加起来。
//
// **这个口径是改过的。** 原先它只管 History，固定开销一分不算——于是
// 16000 的预算看着清爽，真实请求却是 16000 加上小两万字节的工具声明，
// 一倍还多。工具表一路长到五十几个也没人喊疼，正是因为长的那部分
// 不在账上。现在固定开销进了账，数字才对得上真实发出去的东西，
// 上限也跟着提到 32000。
const DefaultContextLimit = 32000

// compactPrompt 指导模型怎么总结早期对话。
const compactPrompt = `请把上面这段对话压缩成一份交接说明，供你自己稍后继续工作时阅读。

必须保留：
1. 用户提出过的目标和偏好（比如习惯几点做什么、不想被打扰的时段）
2. 你已经做了哪些改动（条目名称、改成了什么、邮件是否已清理/发送）
3. 还没做完或用户明确要求接下来做的事
4. 用户纠正过你的地方

不要保留：寒暄、你查询到但已经用过的中间结果、重复的内容。
直接给出说明本身，不要写"好的"之类的开场白。`

// EstimateTokens 粗略估算一组消息占多少 token。
func EstimateTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += len([]rune(m.Content))/2 + len([]rune(m.ReasoningContent))/3
		for _, tc := range m.ToolCalls {
			total += len([]rune(tc.Function.Name+tc.Function.Arguments)) / 2
		}
		total += 4
	}
	return total
}

// EstimateToolTokens 估算工具声明占多少 token。工具表跟着每一次请求发，
// 不随对话增长却一直占着窗口——五十几个工具的声明就有两万多字节。
// 按字节估而不是按 rune：它是 JSON，绝大部分是 ASCII。
func EstimateToolTokens(defs []ToolDef) int {
	raw, _ := json.Marshal(defs)
	return len(raw) / 3
}

// fixedTokens 是每一轮都要重发的那部分：系统提示加上全部工具声明。
//
// **不缓存。** 这两样里只有工具声明是真不变的：系统提示带着各技能的环境
// 快照（余额、未读数、当前时间），`/auto` 换档也会换掉一段文案。按技能
// 集合缓存一次、往后整个会话都用开头那一版，漏算的恰好是会长的那部分。
// 成本也撑不起缓存——界面那层早按 History 长度缓存了整个 ContextUsage
// 结果（chat.contextRatio），真打到这儿的只有「又追加了一条消息」和压缩检查。
func (a *Agent) fixedTokens() int {
	return EstimateTokens([]Message{{Role: "system", Content: a.systemPrompt()}}) +
		EstimateToolTokens(a.wireTools())
}

// ContextUsage 返回这一轮请求的估算 token 数与占预算的比例。
// 算的是真实要发出去的全部内容，不只是对话历史。
func (a *Agent) ContextUsage() (tokens int, ratio float64) {
	fixed := a.fixedTokens()
	tokens = fixed + EstimateTokens(a.History)
	limit := a.contextLimit(fixed)
	if limit <= 0 {
		return tokens, 0
	}
	return tokens, float64(tokens) / float64(limit)
}

// contextLimit 是这一轮的 token 预算。
//
// 配置里存着的可能是**旧口径**的数：那会儿这个值只管对话历史，不含系统
// 提示和工具声明。同一个 16000 放到新口径下连固定开销的两倍都不到，
// 一进门就到压缩线。装不下的值当没设过，省得老用户升上来之后每轮都在压缩。
func (a *Agent) contextLimit(fixed int) int {
	if a.ContextLimit > fixed*2 {
		return a.ContextLimit
	}
	return DefaultContextLimit
}

// splitPoint 找一个能安全截断的位置：必须落在某个 user 消息之前。
func (a *Agent) splitPoint(keepTurns int) int {
	if keepTurns < 1 {
		keepTurns = 1
	}
	turns := 0
	for i := len(a.History) - 1; i >= 0; i-- {
		if a.History[i].Role != "user" {
			continue
		}
		turns++
		if turns == keepTurns {
			return i
		}
	}
	return 0
}

// Compact 把早期对话换成一段摘要，只保留最近 keepTurns 轮完整对话。
func (a *Agent) Compact(ctx context.Context, keepTurns int) (bool, error) {
	cut := a.splitPoint(keepTurns)
	if cut <= 0 {
		return false, nil
	}

	old := append([]Message(nil), a.History[:cut]...)
	req := append(old, Message{Role: "user", Content: compactPrompt})
	summary, usage, err := a.Client.Chat(ctx, req, nil)
	if err != nil {
		return false, fmt.Errorf("压缩上下文失败: %w", err)
	}
	a.Usage.PromptTokens += usage.PromptTokens
	a.Usage.CompletionTokens += usage.CompletionTokens
	a.Usage.TotalTokens += usage.TotalTokens

	text := strings.TrimSpace(summary.Content)
	if text == "" {
		return false, fmt.Errorf("压缩上下文失败: 模型没有给出摘要")
	}
	a.History = append([]Message{
		{Role: "user", Content: "这是我们之前对话的交接说明，请据此继续：\n\n" + text},
		{Role: "assistant", Content: "好的，我记住了，继续。"},
	}, a.History[cut:]...)
	return true, nil
}

// maybeCompact 在上下文接近上限时自动压缩。
func (a *Agent) maybeCompact(ctx context.Context, emit func(Event)) {
	if !a.AutoCompact {
		return
	}
	tokens, ratio := a.ContextUsage()
	if ratio < 0.7 {
		return
	}
	emit(Event{Kind: EventCompact, Text: fmt.Sprintf("上下文约 %d tokens，正在压缩…", tokens)})

	done, err := a.Compact(ctx, 2)
	switch {
	case err != nil:
		emit(Event{Kind: EventError, Text: err.Error()})
	case done:
		after, _ := a.ContextUsage()
		emit(Event{Kind: EventCompact,
			Text: fmt.Sprintf("已压缩上下文：约 %d → %d tokens", tokens, after)})
	}
}
