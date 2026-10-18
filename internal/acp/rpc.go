// Package acp 让别的程序（UULink、Zed 这类 ACP 客户端）经 stdio 驱动 Jarvis。
//
// 协议是 Agent Client Protocol：换行分隔的 JSON-RPC 2.0，客户端发 initialize /
// session/* 请求，我们用 session/update 通知推送回复过程，要用户点头时反过来向
// 客户端发 session/request_permission 请求。规范见 https://agentclientprotocol.com。
//
// 这个包只管协议，不管装配：秘书怎么造、会话存在哪，由 Host 交进来。
// 这样终端界面那边的改动碰不到这里，这里也不碰终端。
package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// message 是一条 JSON-RPC 消息：请求（有 method 有 id）、通知（有 method 没 id）、
// 响应（没 method 有 id）共用一个结构，按字段区分。
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("%d %s", e.Code, e.Message) }

// JSON-RPC 与 ACP 约定的错误码。
const (
	codeParse          = -32700
	codeInvalidParams  = -32602
	codeMethodNotFound = -32601
	codeInternal       = -32603
	// codeNotFound 是 ACP 的 resource not found：会话不存在时客户端据此改为新建。
	codeNotFound = -32002
)

// conn 负责收发：写要串行（多个会话的回复会同时往外推），收到的响应按 id 交还
// 给等着它的那次 call。
type conn struct {
	in  *bufio.Scanner
	out io.Writer

	mu      sync.Mutex // 护着 out
	pmu     sync.Mutex // 护着 next 和 waiting
	next    int64
	waiting map[string]chan message
}

func newConn(in io.Reader, out io.Writer) *conn {
	sc := bufio.NewScanner(in)
	// 图片是 base64 塞在 prompt 里的，一行能有好几兆。
	sc.Buffer(make([]byte, 64<<10), 64<<20)
	return &conn{in: sc, out: out, waiting: map[string]chan message{}}
}

func (c *conn) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = c.out.Write(append(raw, '\n'))
	return err
}

func (c *conn) reply(id json.RawMessage, result any) error {
	if result == nil {
		result = struct{}{}
	}
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *conn) fail(id json.RawMessage, code int, msg string) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": rpcError{Code: code, Message: msg}})
}

func (c *conn) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call 向客户端发一个请求，返回一个等响应的通道（只会收到一条）。
// 不在这里阻塞：等的那头还得同时看着取消。
func (c *conn) call(method string, params any) (<-chan message, func(), error) {
	c.pmu.Lock()
	c.next++
	id := fmt.Sprintf("%d", c.next)
	ch := make(chan message, 1)
	c.waiting[id] = ch
	c.pmu.Unlock()
	forget := func() {
		c.pmu.Lock()
		delete(c.waiting, id)
		c.pmu.Unlock()
	}
	err := c.write(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params})
	if err != nil {
		forget()
		return nil, nil, err
	}
	return ch, forget, nil
}

// deliver 把客户端发回的响应交给对应的 call。
func (c *conn) deliver(m message) {
	c.pmu.Lock()
	ch := c.waiting[string(m.ID)]
	delete(c.waiting, string(m.ID))
	c.pmu.Unlock()
	if ch != nil {
		ch <- m
	}
}
