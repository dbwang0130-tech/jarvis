package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/config"
)

// Preset 是一个模型服务商的默认接入参数。
type Preset struct {
	BaseURL string
	Model   string
	KeyEnv  string
	// NoTools 表示该模型不支持 function calling。
	NoTools bool
}

// Presets 覆盖常见的 OpenAI 兼容服务商。
var Presets = map[string]Preset{
	"deepseek": {
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-chat",
		KeyEnv:  "DEEPSEEK_API_KEY",
	},
	"deepseek-reasoner": {
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-reasoner",
		KeyEnv:  "DEEPSEEK_API_KEY",
	},
	"qwen": {
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen-max",
		KeyEnv:  "DASHSCOPE_API_KEY",
	},
	"kimi": {
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "moonshot-v1-32k",
		KeyEnv:  "MOONSHOT_API_KEY",
	},
	"minimax": {
		BaseURL: "https://api.minimax.chat/v1",
		Model:   "MiniMax-Text-01",
		KeyEnv:  "MINIMAX_API_KEY",
	},
	"zhipu": {
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model:   "glm-4-plus",
		KeyEnv:  "ZHIPU_API_KEY",
	},
	"doubao": {
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Model:   "doubao-pro-32k",
		KeyEnv:  "ARK_API_KEY",
	},
	"spark": {
		BaseURL: "https://spark-api-open.xf-yun.com/v1",
		Model:   "generalv3.5",
		KeyEnv:  "SPARK_API_KEY",
	},
	"baichuan": {
		BaseURL: "https://api.baichuan-ai.com/v1",
		Model:   "Baichuan4",
		KeyEnv:  "BAICHUAN_API_KEY",
	},
	"ollama": {
		BaseURL: "http://localhost:11434/v1",
		Model:   "qwen2.5",
		KeyEnv:  "",
	},
}

// PresetNames 返回全部预设名，便于提示用户。
func PresetNames() []string {
	names := make([]string, 0, len(Presets))
	for k := range Presets {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Message 是一条对话消息，字段与 OpenAI chat completions 对齐。
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolCallID 仅在 role 为 tool 时使用。
	ToolCallID string `json:"tool_call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	// ReasoningContent 是部分服务商（如 deepseek-reasoner）返回的思考过程。
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ToolCall 是模型发起的一次工具调用。
type ToolCall struct {
	// Index 用于流式响应下的分片拼接。
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall 是工具调用的函数名与 JSON 参数。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef 是发往模型的工具声明（wire 格式）。
type ToolDef struct {
	Type     string   `json:"type"`
	Function ToolSpec `json:"function"`
}

// Usage 是一次请求的 token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Client 是一个 OpenAI 兼容接口的最小客户端。
type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	HTTP        *http.Client
	// SupportsTools 为假时不发送 tools 字段。
	SupportsTools bool
}

// Options 是构造 Client 所需的参数，字段留空时回落到预设值。
type Options struct {
	Provider    string
	Model       string
	BaseURL     string
	APIKey      string
	APIKeyEnv   string
	APIKeyCmd   string
	Temperature float64
	Timeout     time.Duration
	Proxy       string
}

// New 按预设与覆盖项构造客户端。
func New(opts Options) (*Client, error) {
	provider := opts.Provider
	if provider == "" {
		provider = "deepseek"
	}
	preset, ok := Presets[provider]
	if !ok {
		if opts.BaseURL == "" || opts.Model == "" {
			return nil, fmt.Errorf("未知的模型预设 %q，可选 %s；或同时提供 base_url 与 model",
				provider, strings.Join(PresetNames(), "、"))
		}
		preset = Preset{BaseURL: opts.BaseURL, Model: opts.Model, KeyEnv: opts.APIKeyEnv}
	}

	tr, err := transportFor(opts.Proxy)
	if err != nil {
		return nil, err
	}
	c := &Client{
		BaseURL:       firstNonEmpty(opts.BaseURL, preset.BaseURL),
		Model:         firstNonEmpty(opts.Model, preset.Model),
		Temperature:   opts.Temperature,
		SupportsTools: !preset.NoTools,
		HTTP: &http.Client{
			Timeout:   orDuration(opts.Timeout, 120*time.Second),
			Transport: tr,
		},
	}

	keyEnv := firstNonEmpty(opts.APIKeyEnv, preset.KeyEnv)
	c.APIKey = firstNonEmpty(opts.APIKey, os.Getenv(keyEnv))
	if c.APIKey == "" && opts.APIKeyCmd != "" {
		if key, cerr := config.RunCmd(opts.APIKeyCmd); cerr == nil {
			c.APIKey = key
		}
	}
	if c.APIKey == "" {
		if key, kerr := config.KeychainGet("llm", c.Model); kerr == nil {
			c.APIKey = key
		}
	}
	if c.APIKey == "" && keyEnv != "" {
		return nil, fmt.Errorf("环境变量 %s 未设置，无法连接 %s", keyEnv, provider)
	}
	return c, nil
}

// transportFor 按配置决定模型接口怎么走网络。
func transportFor(proxy string) (http.RoundTripper, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	switch strings.ToLower(strings.TrimSpace(proxy)) {
	case "":
		// 跟随环境变量。
	case "none", "off", "direct":
		tr.Proxy = nil
	default:
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("agent.proxy = %q 不是合法地址: %w", proxy, err)
		}
		tr.Proxy = http.ProxyURL(u)
	}
	return tr, nil
}

// Chat 发起一次对话请求并返回模型的回复消息。
func (c *Client) Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Message, Usage, error) {
	req := chatRequest{
		Model:       c.Model,
		Messages:    msgs,
		Temperature: c.Temperature,
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
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return Message{}, Usage{}, fmt.Errorf("请求模型失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Message{}, Usage{}, err
	}
	var out chatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Message{}, Usage{}, fmt.Errorf("解析响应失败 (HTTP %d): %s", resp.StatusCode, truncate(string(data), 300))
	}
	if out.Error != nil {
		return Message{}, Usage{}, fmt.Errorf("模型返回错误: %s", out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return Message{}, Usage{}, fmt.Errorf("模型返回 HTTP %d: %s", resp.StatusCode, truncate(string(data), 300))
	}
	if len(out.Choices) == 0 {
		return Message{}, out.Usage, fmt.Errorf("模型没有返回任何内容")
	}
	msg := out.Choices[0].Message
	msg.Role = "assistant"
	return msg, out.Usage, nil
}

// Verify 用一次轻量请求确认接口地址与密钥可用。
func (c *Client) Verify(ctx context.Context) error {
	url := strings.TrimSuffix(c.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接 %s：%w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("密钥被拒绝（HTTP %d）：%s", resp.StatusCode, truncate(strings.TrimSpace(string(body)), 200))
	case resp.StatusCode >= 500:
		return fmt.Errorf("服务端异常（HTTP %d）", resp.StatusCode)
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func orDuration(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
