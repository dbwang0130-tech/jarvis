package acp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/session"
)

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URI      string `json:"uri,omitempty"`
	Name     string `json:"name,omitempty"`
	Resource *struct {
		URI  string `json:"uri"`
		Text string `json:"text,omitempty"`
	} `json:"resource,omitempty"`
}

// promptInput 把 ACP 的内容块拼成一句话和几张图（data URL）。
// 引用的文件只认带正文的；只给了链接的写成一行「附：…」，模型至少知道有这回事。
func promptInput(blocks []contentBlock) (string, []string) {
	var text []string
	var images []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			text = append(text, b.Text)
		case "image":
			if b.Data != "" {
				mime := b.MimeType
				if mime == "" {
					mime = "image/png"
				}
				images = append(images, "data:"+mime+";base64,"+b.Data)
			}
		case "resource":
			if b.Resource != nil && b.Resource.Text != "" {
				text = append(text, "附 "+b.Resource.URI+"：\n"+b.Resource.Text)
			}
		case "resource_link":
			text = append(text, "附："+b.URI)
		}
	}
	return strings.Join(text, "\n"), images
}

// pendingPrompt 是已经占住会话、还没开始答的一轮。
type pendingPrompt struct {
	id     json.RawMessage
	ss     *sess
	ctx    context.Context
	cancel context.CancelFunc
	text   string
	images []string
}

// beginPrompt 在读循环里同步占住会话：set_mode 这类请求也在读循环里办，
// 它们看到的「忙不忙」必须和这里一致，不然会在答到一半时往历史里插消息。
func (s *Server) beginPrompt(m message) (*pendingPrompt, error) {
	var p struct {
		SessionID string         `json:"sessionId"`
		Prompt    []contentBlock `json:"prompt"`
	}
	if err := json.Unmarshal(m.Params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}
	ss, err := s.mustLookup(p.SessionID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.cancel != nil {
		cancel()
		return nil, &rpcError{Code: codeInvalidParams, Message: "上一轮还没答完"}
	}
	ss.cancel = cancel
	text, images := promptInput(p.Prompt)
	return &pendingPrompt{id: m.ID, ss: ss, ctx: ctx, cancel: cancel, text: text, images: images}, nil
}

// runPrompt 答一轮。读循环之外跑（中间还要反过来问客户端要不要批准），结束时自己回请求。
func (s *Server) runPrompt(p *pendingPrompt) {
	ss, ctx := p.ss, p.ctx
	defer p.cancel()

	t := &turn{s: s, ss: ss, ctx: ctx}
	answer, err := ss.ag.AskWith(ctx, p.text, p.images, t.emit)
	t.flush()
	// 非流式的服务商（或者这一步没有流出正文）不发增量，答复只在返回值里。
	if err == nil && answer != "" && !t.streamed {
		s.update(ss.id, textChunk("agent_message_chunk", stripLinks(answer)))
	}

	stop := "end_turn"
	if ctx.Err() != nil {
		// 跟终端界面一样记一笔，不然下一轮模型会以为上一件事办完了。
		ss.ag.NoteInterrupted()
		stop, err = "cancelled", nil
	}
	// 先放开会话再回请求：客户端一收到回复就可能发下一轮。
	s.endTurn(ss)
	if err != nil {
		_ = s.c.fail(p.id, codeInternal, err.Error())
		return
	}
	_ = s.c.reply(p.id, map[string]any{"stopReason": stop})
}

// endTurn 把这一轮攒下的事办掉、存盘，再把会话标成空闲。办的过程中又攒了新的
// （set_mode 恰好这时候到），一并办完才放手；存盘和放手在同一把锁里，
// 下一轮不会在存到一半时开始往历史里写。
func (s *Server) endTurn(ss *sess) {
	for {
		ss.mu.Lock()
		todo := ss.afterTurn
		ss.afterTurn = nil
		if len(todo) == 0 {
			s.save(ss)
			ss.cancel = nil
			ss.mu.Unlock()
			return
		}
		ss.mu.Unlock()
		for _, f := range todo {
			f()
		}
	}
}

// save 存历史；第一轮答完顺手定下标题，告诉客户端。
func (s *Server) save(ss *sess) {
	_ = s.store.Save(ss.id, ss.ag.History)
	if ss.title != "" {
		return
	}
	ss.title = titleOf(ss.ag.History)
	if ss.title == "" {
		return
	}
	_ = s.store.SaveMeta(ss.id, session.Meta{CWD: ss.cwd, Title: ss.title})
	s.update(ss.id, map[string]any{"sessionUpdate": "session_info_update", "title": ss.title})
}

// turn 是一轮里的推送状态。emit 由 agent 在它自己的 goroutine 里顺序调用。
type turn struct {
	s   *Server
	ss  *sess
	ctx context.Context
	// streamed 记这一步的正文有没有流式推过：最后一步没流的话，答复要补发。
	streamed bool
	links    linkFilter
}

// say 推一段正文（先滤掉内部链接）。
func (t *turn) say(text string) {
	if text = t.links.feed(text); text != "" {
		t.s.update(t.ss.id, textChunk("agent_message_chunk", text))
	}
}

// flush 把滤链接时攒着的半截正文放出去。
func (t *turn) flush() {
	if text := t.links.flush(); text != "" {
		t.s.update(t.ss.id, textChunk("agent_message_chunk", text))
	}
}

func (t *turn) emit(e agent.Event) {
	id := t.ss.id
	switch e.Kind {
	case agent.EventDelta:
		t.streamed = true
		t.say(e.Text)
	case agent.EventReasoning:
		t.s.update(id, textChunk("agent_thought_chunk", e.Text))
	case agent.EventThinking:
		// 非流式时调工具前的那句「我先看一下日程」。
		t.say(e.Text + "\n\n")
	case agent.EventCompact:
		t.s.update(id, textChunk("agent_thought_chunk", e.Text))
	case agent.EventToolCall:
		t.flush()
		t.streamed = false
		t.s.update(id, toolCall(e.ID, e.Tool, e.Args, e.Text, "in_progress"))
	case agent.EventToolResult:
		t.s.update(id, toolDone(e.ID, "completed", e.Output))
	case agent.EventError:
		if e.ID != "" {
			t.s.update(id, toolDone(e.ID, "failed", e.Text))
		}
	case agent.EventPending:
		e.Pending.Reply <- t.askPermission(e)
	}
}

// askPermission 向客户端要一个决定，阻塞到答复或者这一轮被取消。
func (t *turn) askPermission(e agent.Event) bool {
	p := e.Pending
	options := []map[string]any{{"optionId": "allow", "name": "允许", "kind": "allow_once"}}
	// 「以后不再问」= 把档位抬到正好放行这一级风险，和终端确认框一个意思。
	raise := agent.AutoClean
	if p.Risk >= agent.RiskIrreversible {
		raise = agent.AutoAll
	}
	if raise > t.ss.ag.Auto {
		options = append(options, map[string]any{"optionId": "always", "kind": "allow_always",
			"name": "以后不再问（切到「" + raise.Label() + "」）"})
	}
	options = append(options, map[string]any{"optionId": "reject", "name": "拒绝", "kind": "reject_once"})

	call := map[string]any{"toolCallId": e.ID, "title": p.Title}
	if p.Detail != "" {
		call["content"] = []any{textContent(p.Detail)}
	}
	ch, forget, err := t.s.c.call("session/request_permission",
		map[string]any{"sessionId": t.ss.id, "toolCall": call, "options": options})
	if err != nil {
		return false
	}
	defer forget()
	var m message
	select {
	case m = <-ch:
	case <-t.ctx.Done():
		return false
	}
	if m.Error != nil {
		return false
	}
	var r struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if json.Unmarshal(m.Result, &r) != nil || r.Outcome.Outcome != "selected" {
		return false
	}
	switch r.Outcome.OptionID {
	case "allow":
		return true
	case "always":
		// 这一轮里后面同级的动作就别再问了，所以档位现在就抬；告诉模型的那条
		// system 消息得等这一轮答完再插（见 sess.afterTurn）。
		prev := t.ss.ag.Auto
		t.ss.ag.Auto = raise
		t.ss.mu.Lock()
		t.ss.afterTurn = append(t.ss.afterTurn, func() {
			t.ss.ag.Auto = prev
			t.ss.ag.SetAuto(raise)
		})
		t.ss.mu.Unlock()
		t.s.update(t.ss.id, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": modeID(raise)})
		return true
	}
	return false
}

func textContent(text string) map[string]any {
	return map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": text}}
}

// toolCall 是一张工具卡片。title 用中文标签（标准 ACP 客户端只认 title），
// 原始工具名和一行摘要放在 _meta.jarvis 里，UULink 那边据此渲染。
// kind 一律 other：Jarvis 的工具是日程、邮件这些领域动作，不是读写文件、
// 跑命令，硬套 read / edit 只会让客户端按文件去渲染。
func toolCall(id, tool, args, summary, status string) map[string]any {
	u := map[string]any{"sessionUpdate": "tool_call", "toolCallId": id, "title": agent.ToolLabel(tool),
		"kind": "other", "status": status,
		"_meta": map[string]any{"jarvis": map[string]any{"toolName": tool, "summary": summary}}}
	var input any
	if json.Unmarshal([]byte(args), &input) == nil && input != nil {
		u["rawInput"] = input
	}
	return u
}

func toolDone(id, status, output string) map[string]any {
	u := map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": id, "status": status, "rawOutput": output}
	if output != "" {
		u["content"] = []any{textContent(output)}
	}
	return u
}

// replay 把存下的历史翻成 session/update，session/load 时整段推给客户端。
// system 消息（档位变更、被打断、压缩摘要的说明）是写给模型的，不推。
func replay(h []agent.Message) []map[string]any {
	var out []map[string]any
	for _, m := range h {
		switch m.Role {
		case "user":
			if m.Content != "" {
				out = append(out, textChunk("user_message_chunk", m.Content))
			}
		case "assistant":
			if m.ReasoningContent != "" {
				out = append(out, textChunk("agent_thought_chunk", m.ReasoningContent))
			}
			if c := strings.TrimSpace(m.Content); c != "" {
				out = append(out, textChunk("agent_message_chunk", stripLinks(c)))
			}
			for _, call := range m.ToolCalls {
				out = append(out, toolCall(call.ID, call.Function.Name, call.Function.Arguments,
					agent.DescribeCall(call), "in_progress"))
			}
		case "tool":
			status := "completed"
			if failedResult(m.Content) {
				status = "failed"
			}
			out = append(out, toolDone(m.ToolCallID, status, m.Content))
		}
	}
	return out
}

// failedResult 认出 agent 回灌给模型的失败结果（见 agent.executeCall 的几句固定说法）。
func failedResult(content string) bool {
	return strings.HasPrefix(content, "执行失败：") || strings.HasPrefix(content, "用户拒绝了这个操作") ||
		strings.HasPrefix(content, "没有名为 ")
}
