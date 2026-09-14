package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Delta 是流式响应里的一小段增量。
type Delta struct {
	// Text 是正文增量。
	Text string
	// Reasoning 是思考过程增量，部分模型（如 deepseek-reasoner）会单独给出。
	Reasoning string
}

// streamChunk 是 SSE 里一帧的结构。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ChatStream 与 Chat 等价，但边收边把增量交给 onDelta。
func (c *Client) ChatStream(ctx context.Context, msgs []Message, tools []ToolDef, onDelta func(Delta)) (Message, Usage, error) {
	if onDelta == nil {
		return c.Chat(ctx, msgs, tools)
	}
	req := chatRequest{
		Model:       c.Model,
		Messages:    msgs,
		Temperature: c.Temperature,
		Stream:      true,
	}
	if c.SupportsTools && len(tools) > 0 {
		req.Tools = tools
		req.ToolChoice = "auto"
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, Usage{}, err
	}
	url := strings.TrimSuffix(c.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Message{}, Usage{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("请求模型失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return Message{}, Usage{}, fmt.Errorf("模型返回 HTTP %d: %s",
			resp.StatusCode, truncate(strings.TrimSpace(string(data)), 300))
	}
	return readStream(resp.Body, onDelta)
}

// readStream 解析 SSE 流并把分片拼回一条完整消息。
func readStream(r io.Reader, onDelta func(Delta)) (Message, Usage, error) {
	msg := Message{Role: "assistant"}
	var usage Usage
	calls := map[int]*ToolCall{}
	var order []int

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil {
			return msg, usage, fmt.Errorf("模型返回错误: %s", chunk.Error.Message)
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta

		if d.Content != "" {
			msg.Content += d.Content
			onDelta(Delta{Text: d.Content})
		}
		if d.ReasoningContent != "" {
			msg.ReasoningContent += d.ReasoningContent
			onDelta(Delta{Reasoning: d.ReasoningContent})
		}
		for _, tc := range d.ToolCalls {
			cur, ok := calls[tc.Index]
			if !ok {
				cur = &ToolCall{Index: tc.Index, Type: "function"}
				calls[tc.Index] = cur
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Type != "" {
				cur.Type = tc.Type
			}
			cur.Function.Name += tc.Function.Name
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return msg, usage, fmt.Errorf("读取流式响应失败: %w", err)
	}

	for _, i := range order {
		msg.ToolCalls = append(msg.ToolCalls, *calls[i])
	}
	return msg, usage, nil
}

// IsCanceled 报告错误是不是因为主动打断造成的。
func IsCanceled(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(err.Error(), context.Canceled.Error())
}
