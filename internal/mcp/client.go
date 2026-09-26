// Package mcp 是一个最小的 MCP 客户端（stdio 传输）。
//
// **为什么要它**：现在加一项能力必须写 Go、重新编译，还要在装配表里登记。
// `skills/` 下的 markdown 能贡献「这件事怎么办」，但贡献不了新动作。MCP 是那件
// 事的标准答案——一个 server 就是一组工具，而 `agent.Skill` 这个接口本来就是
// 「一组工具加一段说明」，两边天然对得上。
//
// **不引依赖**：stdio 上的 MCP 就是一行一条 JSON-RPC 2.0，手写比引一个 SDK 划算
// （这个仓库直接依赖只有 5 个，而且官方 SDK 会把整个 schema 体系一起拖进来）。
// 只实现真用得上的四件事：initialize、tools/list、tools/call、关掉。
//
// **传输是接口**，所以能离线测：测试拿一对内存管道当 server，不 spawn 进程、
// 不碰网络——跟 agent 那边用假 http.RoundTripper 跑脚本是同一个路子。
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// protocolVersion 是我们声明支持的 MCP 版本。
//
// server 回一个不同的版本是允许的（协议自己说要协商），所以**不拿它当门槛**：
// 版本对不上就拒绝连接，等于每次 server 升级都要先改这儿，而绝大多数情况下
// tools/list 和 tools/call 照样能用。
const protocolVersion = "2025-06-18"

// Tool 是 server 报上来的一个工具。
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Transport 是跟 server 之间那根管子。
//
// 真用的时候是一个子进程的 stdin/stdout（见 Stdio）；测试里是一对内存管道。
type Transport interface {
	io.ReadWriteCloser
}

// Client 是一个 server 的连接。
//
// **不是并发安全的收发多路复用**：请求是一问一答串着发的（同一时刻只有一个
// in-flight），因为工具调用本来就是串行的，而多路复用要额外一套 id 路由和读循环，
// 代价不值。锁只用来保证不会有两个调用交叉写同一根管子。
type Client struct {
	name string
	tr   Transport
	br   *bufio.Reader

	mu     sync.Mutex
	nextID int
	// ready 为真表示 initialize 已经握过手。
	ready bool
	// Info 是 server 自报的名字和版本，出错时说得出是谁。
	Info ServerInfo
}

// ServerInfo 是 server 的自我介绍。
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// New 包一根已经连上的管子。name 只用于报错时说清是哪个 server。
func New(name string, tr Transport) *Client {
	return &Client{name: name, tr: tr, br: bufio.NewReader(tr)}
}

// Name 是这个 server 在配置里的名字。
func (c *Client) Name() string { return c.name }

// Handshake 跑一遍 initialize，并按协议回一条 initialized 通知。
//
// **通知那一条不能省**：不少 server 在收到它之前不会响应 tools/list，表现是
// 第一次列工具就卡住直到超时——而超时的错误信息里看不出缺了这一步。
func (c *Client) Handshake(ctx context.Context) error {
	var res struct {
		ProtocolVersion string     `json:"protocolVersion"`
		ServerInfo      ServerInfo `json:"serverInfo"`
	}
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "jarvis", "version": "1"},
	}
	if err := c.call(ctx, "initialize", params, &res); err != nil {
		return err
	}
	c.Info = res.ServerInfo
	if err := c.notify("notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
	return nil
}

// Tools 列出这个 server 提供的工具。
//
// 一次拉完，不翻页：协议里有 cursor，但一个 server 几十个工具是常态、几千个不是，
// 真遇到了再说——现在就写翻页是在为想象中的规模付复杂度。
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var res struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// Call 调一个工具，返回它输出的文字。
//
// **出错也返回文字，不返回 error**：工具报错（参数不对、目标不存在）是模型该读到
// 并自己纠正的东西，不是这一轮该中断的理由——agent 那边的工具也是这个约定。
// 只有连不上、协议坏了这类才是 error。
func (c *Client) Call(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	params := map[string]any{"name": name, "arguments": json.RawMessage(args)}
	if err := c.call(ctx, "tools/call", params, &res); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, part := range res.Content {
		// 非文字的部分（图片、资源引用）先只说一声有，别塞一堆 base64 进上下文。
		if part.Type != "text" {
			fmt.Fprintf(&b, "[%s]\n", part.Type)
			continue
		}
		b.WriteString(part.Text)
		if !strings.HasSuffix(part.Text, "\n") {
			b.WriteString("\n")
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if res.IsError {
		return "工具报错：" + out, nil
	}
	if out == "" {
		return "（没有输出）", nil
	}
	return out, nil
}

// Close 关掉管子。
func (c *Client) Close() error { return c.tr.Close() }

// rpcError 是 server 回的错误。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e rpcError) Error() string { return fmt.Sprintf("%s（code %d）", e.Message, e.Code) }

// call 发一条请求并等它的答复。
//
// **ctx 没有期限就自己加一个。** 读是阻塞的，而一个行为不端的 server（往 stdout
// 打了几行日志然后不回话）会让调用方永远挂在这儿——写这个包时就自己踩了一次，
// 测试挂到 120 秒超时才被发现。挂死是最难排查的失败，宁可给一句「超时了，是哪个
// server、哪个方法」。
func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		limit := DefaultTimeout
		if method == "initialize" {
			// 起 server 那一下能很慢（npx 要下包），给得宽些。
			limit = HandshakeTimeout
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.write(raw); err != nil {
		return fmt.Errorf("%s 写不进去：%w", c.name, err)
	}

	// **答复要在 ctx 的期限内到。** 读是阻塞的，所以真正的超时靠调用方关管子——
	// ctx 一到期就把它关掉，读那边立刻返回错误，不会挂死一个 goroutine。
	done := make(chan error, 1)
	go func() { done <- c.read(id, method, out) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = c.tr.Close()
		return fmt.Errorf("%s 的 %s 超时：%w", c.name, method, ctx.Err())
	}
}

// read 一直读到那条 id 的答复。
//
// 中途可能夹着 server 主动发的通知（日志、进度），一律跳过——我们没订阅它们，
// 但不能因为它们在就把答复读丢了。
func (c *Client) read(id int, method string, out any) error {
	for {
		line, err := c.br.ReadBytes('\n')
		if err != nil {
			if len(strings.TrimSpace(string(line))) == 0 {
				return fmt.Errorf("%s 断了（%s 没等到答复）：%w", c.name, method, err)
			}
			// 最后一行没换行也算一条，处理完再返回错误。
		}
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if e := json.Unmarshal(line, &msg); e != nil {
			// 不是合法 JSON：多半是 server 往 stdout 打了日志。跳过，别当致命错误
			// ——很多 server 都这么干，而因此拒绝工作会让人完全无法排查。
			if err != nil {
				return fmt.Errorf("%s 断了：%w", c.name, err)
			}
			continue
		}
		if msg.ID == nil || *msg.ID != id {
			if err != nil {
				return fmt.Errorf("%s 断了：%w", c.name, err)
			}
			continue // 通知，或者不是这一条的答复
		}
		if msg.Error != nil {
			return fmt.Errorf("%s 的 %s 失败：%w", c.name, method, msg.Error)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

func (c *Client) notify(method string, params any) error {
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": method, "params": params,
	})
	if err != nil {
		return err
	}
	return c.write(raw)
}

func (c *Client) write(raw []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.tr.Write(append(raw, '\n'))
	return err
}

// DefaultTimeout 是一次请求等多久。
//
// 起 server 那一下（npx 要下包）能慢到几十秒，所以握手那次给得比调用宽。
// 做成 var 而不是 const，是为了测试能把它临时调短——否则钉「不许挂死」那条
// 测试要真等一分钟。
var (
	DefaultTimeout   = 30 * time.Second
	HandshakeTimeout = 60 * time.Second
)
