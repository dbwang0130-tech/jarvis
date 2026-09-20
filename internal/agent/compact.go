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

// fixedTokens 估算每一轮都要重发的那部分：系统提示加上全部工具声明。
// 它不随对话增长，却一直占着窗口，所以必须算进预算里。
//
// 结果缓存起来：算一次要把所有技能的提示拼一遍、工具声明序列化一遍，
// 而状态栏每帧都要问一次占用。技能换了由 SetSkills 负责作废。
func (a *Agent) fixedTokens() int {
	if a.fixed == 0 {
		raw, _ := json.Marshal(a.wireTools())
		a.fixed = len([]rune(a.systemPrompt()))/2 + len(raw)/3
	}
	return a.fixed
}

// ContextUsage 返回这一轮请求的估算 token 数与占预算的比例。
// 算的是真实要发出去的全部内容，不只是对话历史。
func (a *Agent) ContextUsage() (tokens int, ratio float64) {
	tokens = a.fixedTokens() + EstimateTokens(a.History)
	limit := a.contextLimit()
	if limit <= 0 {
		return tokens, 0
	}
	return tokens, float64(tokens) / float64(limit)
}

func (a *Agent) contextLimit() int {
	if a.ContextLimit > 0 {
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
