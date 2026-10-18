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
	// Vision 表示这个默认模型认得图片。其他客户端能发图、能拍照记账，
	// 发给纯文本模型不是报错就是装作没看见——后者尤其难查，所以这件事
	// 要在切模型的时候就说清楚，别等用户拍完小票才发现白拍。
	Vision bool
	// Console 是去哪儿领密钥。
	//
	// **整个配置流程只有一个真门槛：密钥。** 而界面上原先只有一个空的密码框，
	// 没有任何地方说它从哪儿来——第一次用的人卡在这儿，得自己去搜。这个地址
	// 属于服务商表，跟 BaseURL 是同一类东西，所以放这儿而不是在 Swift 里另抄
	// 一份（抄的那份不会跟着更新，服务商列表变了它就是错的）。
	Console string
}

// Presets 覆盖常见的 OpenAI 兼容服务商。
//
// **这张表会过期。** 模型名是服务商说了算的，改名、下线都不打招呼——
// kimi 的 moonshot-v1 系列 2026-08-31 直接停服，deepseek-chat /
// deepseek-reasoner 也已降级成正在淘汰的别名。表里填着停服的模型，
// 用户切过去只会得到一句看不懂的 400。所以默认模型一律挑**当前在产**
// 的那个，宁可几个月更新一次，也别留个打不通的名字在这儿。
//
// 默认一律选**看得见图片**的那一档：其他客户端能发图、能拍照记账，而
// 纯文本模型收到图片不是报错就是装作没看见，两种都难排查。
var Presets = map[string]Preset{
	"deepseek": {
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-flash", // V4.1 Flash，原生视觉；deepseek-chat 是淘汰中的别名
		KeyEnv:  "DEEPSEEK_API_KEY",
		Vision:  true,
		Console: "https://platform.deepseek.com/api_keys",
	},
	"qwen": {
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen3-vl-plus",
		KeyEnv:  "DASHSCOPE_API_KEY",
		Vision:  true,
		Console: "https://bailian.console.aliyun.com/?apiKey=1",
	},
	"kimi": {
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "kimi-k3", // moonshot-v1 系列已于 2026-08-31 停服
		KeyEnv:  "MOONSHOT_API_KEY",
		Vision:  true,
		Console: "https://platform.moonshot.cn/console/api-keys",
	},
	"minimax": {
		BaseURL: "https://api.minimax.chat/v1",
		Model:   "MiniMax-M3",
		KeyEnv:  "MINIMAX_API_KEY",
		Vision:  true,
		Console: "https://platform.minimaxi.com/user-center/basic-information/interface-key",
	},
	"zhipu": {
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model:   "glm-5v-turbo",
		KeyEnv:  "ZHIPU_API_KEY",
		Vision:  true,
		Console: "https://bigmodel.cn/usercenter/apikeys",
	},
	"doubao": {
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Model:   "doubao-seed-2-1-turbo-260628",
		KeyEnv:  "ARK_API_KEY",
		Vision:  true,
		Console: "https://console.volcengine.com/ark/region:ark+cn-beijing/apiKey",
	},
	"spark": {
		BaseURL: "https://spark-api-open.xf-yun.com/v1",
		Model:   "4.0Ultra", // 图片理解在星火是另一个接口，这条路上走不了
		KeyEnv:  "SPARK_API_KEY",
		Console: "https://console.xfyun.cn/services/cbm",
	},
	"ollama": {
		BaseURL: "http://localhost:11434/v1",
		Model:   "qwen2.5", // 本地装的是什么就填什么，能不能看图取决于你拉的权重
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

// SupportsVision 报告这套配置认不认得图片，以及这个判断靠不靠得住。
//
// 只有用着预设默认模型的时候才敢说死——用户自己填了个模型名，我们无从
// 知道它是哪一档，这时候 known 为假，调用方该提醒而不是拦。**宁可漏报
// 也不能误拦**：表是会过期的（服务商改名、出新模型都不打招呼），拿一张
// 旧表去拦住一个其实看得见图的模型，比让人试一次再发现更糟。
func SupportsVision(provider, model string) (vision, known bool) {
	p, ok := Presets[provider]
	if !ok {
		return false, false
	}
	if model == "" || model == p.Model {
		return p.Vision, true
	}
	return false, false
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

	// Images 是随这条消息一起发出去的图片（data URL，形如
	// `data:image/jpeg;base64,...`）。
	//
	// 有图片时，**发给模型**的 content 要从一个字符串摊成「文本块 + 图片块」
	// 的数组，这是 OpenAI 兼容接口对多模态的规定格式。那件事在 toWire 里做，
	// 不做成 Message 的 MarshalJSON——方法是挂在类型上的，凡是序列化这个
	// 类型的地方都会走它，而 History 还要原样交给 其他客户端 存档和回传。
	// 早先就是这么写的，结果手机上发一张图必报「出参解不开」：Swift 那边
	// content 声明的是 String，收到数组整个出参就解不动了。
	Images []string `json:"-"`
}

// contentPart 是多模态 content 数组里的一块。
type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageRef `json:"image_url,omitempty"`
}

type imageRef struct {
	URL string `json:"url"`
}

// wireMessages 是发给模型的那串消息。
//
// **MarshalJSON 挂在这个切片类型上，不是挂在 Message 上。** 「发给模型的
// 样子」和「存档回传的样子」是两件事：带图片的消息，发出去时 content 要摊成
// 「文本块 + 图片块」的数组（OpenAI 兼容接口对多模态的规定格式），而同一条
// 消息留在 History 里还要原样交给 其他客户端——Swift 那边 content 声明的是 String，
// 收到数组整个出参就解不动了。
//
// 早先图省事写成了 Message.MarshalJSON，方法挂在类型上，凡是序列化这个类型
// 的地方都会走它，于是手机上发一张图必报「出参解不开」。
type wireMessages []Message

// modelMessage 是**发给模型**的那几个字段，照 OpenAI 的 wire 格式列全。
//
// 显式列出来而不是直接复用 Message：往 Message 上加字段是迟早的事，而这儿
// 一旦跟着漏出去，多余字段有的服务商忍了，有的直接 400——那种错读起来完全
// 指不到这儿。发给模型的形状就该在这儿写死。
type modelMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
}

func (ms wireMessages) MarshalJSON() ([]byte, error) {
	out := make([]any, 0, len(ms))
	for _, m := range ms {
		// 没图片的一个字节都不变：绝大多数请求都不带图片，不能让多模态
		// 这条路把普通对话的线上格式也改了（有的服务商对数组形式的
		// content 支持得并不好）。
		if len(m.Images) == 0 {
			out = append(out, modelMessage{
				Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls,
				ToolCallID: m.ToolCallID, Name: m.Name,
				ReasoningContent: m.ReasoningContent,
			})
			continue
		}
		parts := make([]contentPart, 0, len(m.Images)+1)
		if m.Content != "" {
			parts = append(parts, contentPart{Type: "text", Text: m.Content})
		}
		for _, img := range m.Images {
			parts = append(parts, contentPart{
				Type: "image_url", ImageURL: &imageRef{URL: img}})
		}
		out = append(out, struct {
			Role       string        `json:"role"`
			Content    []contentPart `json:"content"`
			ToolCallID string        `json:"tool_call_id,omitempty"`
			Name       string        `json:"name,omitempty"`
		}{Role: m.Role, Content: parts, ToolCallID: m.ToolCallID, Name: m.Name})
	}
	return json.Marshal(out)
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
	Model       string       `json:"model"`
	Messages    wireMessages `json:"messages"`
	Tools       []ToolDef    `json:"tools,omitempty"`
	ToolChoice  string       `json:"tool_choice,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
	Stream      bool         `json:"stream,omitempty"`
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
	// 默认 transport 通常是 *http.Transport，但不保证——测试或别的库
	// 换过它的时候，硬断言会直接 panic。认不出来就原样用，代理设置
	// 也就无从谈起了。
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport, nil
	}
	tr := base.Clone()
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

// ModelInfo 是服务商自己报上来的一个模型。
//
// 只有 ID 是 OpenAI 协议规定的，后面几个是各家的扩展（deepseek 给得最全）。
// 所以除了 ID 一律当「可能没有」处理——拿不到就少显示一行，不能因此整张列表作废。
type ModelInfo struct {
	// ID 是调用时要填的那个名字。
	ID string `json:"id"`
	// Name 是给人看的名字（「DeepSeek-V4.1-Flash」）。没有就显示 ID。
	Name string `json:"name"`
	// Modalities 是它吃得下什么。含 "image" 就是认得图片——**这件事原先是
	// 硬编码在 Presets 里的**（见 Preset.Vision 那段），而厂商自己就说了。
	Modalities []string `json:"input_modalities"`
	// Context 是上下文窗口，0 表示没报。
	Context int `json:"context_window"`
}

// Vision 报告这个模型认不认得图片。厂商没报 modalities 时返回 false——
// 宁可不显示那个标记，也别猜一个出来：猜错的后果是用户拍完小票才发现白拍。
func (m ModelInfo) Vision() bool {
	for _, x := range m.Modalities {
		if x == "image" {
			return true
		}
	}
	return false
}

// Models 问服务商它都有哪些模型。
//
// **为的是别让人手打模型名。** 打错一个字换来的是一个看不懂的 400，而第一次配
// 的人根本不知道该填什么——预设里那个名字也会过时（CLAUDE.md 里写着「宁可几个月
// 更新一次」）。从厂商那儿现拉一份，挑就是了。
//
// 拿不到不是错：有的服务商压根没实现 /models，有的要另一套鉴权。调用方一律降级回
// 「自己填」那条路，别把配置流程卡死在这儿。
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	body, err := c.getModels(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("模型列表解不开：%w", err)
	}
	// 把明显不是聊天模型的滤掉。列表里混着嵌入、重排、语音那些，挑的人
	// 看见一堆用不上的名字，反而比自己打字更难。
	kept := out.Data[:0]
	for _, m := range out.Data {
		if m.ID == "" || notChat(m.ID) {
			continue
		}
		kept = append(kept, m)
	}
	return kept, nil
}

// notChat 按名字认出那些不是拿来聊天的模型。
//
// 按名字认是粗糙的，但协议没给别的线索（object 一律是 "model"）。认错的代价
// 只是少一个选项，而列表里留着 embedding 的代价是人挑不出来。
func notChat(id string) bool {
	low := strings.ToLower(id)
	for _, bad := range []string{
		"embed", "rerank", "whisper", "tts", "speech", "audio",
		"moderation", "image", "vision-ocr", "ocr",
	} {
		if strings.Contains(low, bad) {
			return true
		}
	}
	return false
}

// getModels 发那一次 GET /models，并把各种失败翻译成人话。
func (c *Client) getModels(ctx context.Context) ([]byte, error) {
	url := strings.TrimSuffix(c.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接 %s：%w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	// 上限放宽到 64KB：只看状态码时 4KB 够了，要解列表就不够——
	// 服务商多的能报几十个模型，每个还带一段元数据。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("密钥被拒绝（HTTP %d）：%s", resp.StatusCode,
			truncate(strings.TrimSpace(string(body)), 200))
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("服务端异常（HTTP %d）", resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("拿不到模型列表（HTTP %d）：%s", resp.StatusCode,
			truncate(strings.TrimSpace(string(body)), 200))
	}
	return body, nil
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
