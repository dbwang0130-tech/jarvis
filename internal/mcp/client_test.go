package mcp

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// pipe 是客户端手里那根管子：读 server 写的，写给 server 读。
type pipe struct {
	io.Reader
	io.WriteCloser
	rc io.Closer
}

func (p pipe) Close() error {
	_ = p.WriteCloser.Close()
	return p.rc.Close()
}

// fake 起一个内存里的假 server：一行一条 JSON 读进来，交给 handle 回答。
//
// handle 返回空串表示这一条不回（用来模拟 server 只发通知、或者压根不理）。
func fake(t *testing.T, handle func(method string, id *int, params json.RawMessage) string) *Client {
	t.Helper()
	toServer, fromClient := io.Pipe()
	toClient, fromServer := io.Pipe()

	go func() {
		defer fromServer.Close()
		dec := json.NewDecoder(toServer)
		for {
			var msg struct {
				ID     *int            `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := dec.Decode(&msg); err != nil {
				return
			}
			if out := handle(msg.Method, msg.ID, msg.Params); out != "" {
				if _, err := io.WriteString(fromServer, out); err != nil {
					return
				}
			}
		}
	}()

	c := New("假的", pipe{Reader: toClient, WriteCloser: fromClient, rc: toClient})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func okLine(id *int, result string) string {
	if id == nil {
		return ""
	}
	return `{"jsonrpc":"2.0","id":` + itoa(*id) + `,"result":` + result + "}\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// 顺一遍：握手、列工具、调一个。
func TestHappyPath(t *testing.T) {
	var sawInitialized bool
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		switch method {
		case "initialize":
			return okLine(id, `{"protocolVersion":"2025-06-18","serverInfo":{"name":"记事本","version":"0.3"}}`)
		case "notifications/initialized":
			sawInitialized = true
			return ""
		case "tools/list":
			return okLine(id, `{"tools":[{"name":"note_add","description":"记一条","inputSchema":{"type":"object"}},{"name":"note_list","description":"列出来","inputSchema":{"type":"object"}}]}`)
		case "tools/call":
			var p struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)
			return okLine(id, `{"content":[{"type":"text","text":"记好了：`+p.Name+`"}]}`)
		}
		return okLine(id, `{}`)
	})

	ctx := context.Background()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("握手失败：%v", err)
	}
	if c.Info.Name != "记事本" {
		t.Errorf("server 没自报名字：%+v", c.Info)
	}
	// **这条通知不能省**：不少 server 收不到它就不响应 tools/list，
	// 表现是第一次列工具卡到超时，而错误信息里看不出缺了这一步。
	if !sawInitialized {
		t.Error("没发 notifications/initialized")
	}

	tools, err := c.Tools(ctx)
	if err != nil {
		t.Fatalf("列工具失败：%v", err)
	}
	if len(tools) != 2 || tools[0].Name != "note_add" {
		t.Fatalf("工具不对：%+v", tools)
	}

	out, err := c.Call(ctx, "note_add", json.RawMessage(`{"text":"买牛奶"}`))
	if err != nil {
		t.Fatalf("调用失败：%v", err)
	}
	if !strings.Contains(out, "note_add") {
		t.Errorf("输出不对：%q", out)
	}
}

// 答复前面夹着 server 主动发的通知，不能把答复读丢。
func TestSkipsNotifications(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		if method == "tools/list" {
			return `{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"info"}}` + "\n" +
				`{"jsonrpc":"2.0","method":"notifications/progress","params":{}}` + "\n" +
				okLine(id, `{"tools":[{"name":"x","description":"","inputSchema":{}}]}`)
		}
		return okLine(id, `{}`)
	})
	tools, err := c.Tools(context.Background())
	if err != nil {
		t.Fatalf("被通知挡住了：%v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("工具不对：%+v", tools)
	}
}

// **server 往 stdout 打日志是常态**，不能因此罢工。
//
// 一行不是合法 JSON 就拒绝工作的话，人完全无从排查——他看到的是「连不上」，
// 而真正的原因是那个 server 打了一行启动横幅。
func TestToleratesGarbageLines(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		if method == "tools/list" {
			return "listening on stdio...\n" +
				okLine(id, `{"tools":[{"name":"x","description":"","inputSchema":{}}]}`)
		}
		return okLine(id, `{}`)
	})
	if _, err := c.Tools(context.Background()); err != nil {
		t.Fatalf("被一行日志挡住了：%v", err)
	}
}

// 工具自己报错**不是这一轮的 error**：模型该读到并自己纠正。
func TestToolErrorIsText(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		if method == "tools/call" {
			return okLine(id, `{"content":[{"type":"text","text":"没有这个文件"}],"isError":true}`)
		}
		return okLine(id, `{}`)
	})
	out, err := c.Call(context.Background(), "read", nil)
	if err != nil {
		t.Fatalf("工具报错被当成了连接错误：%v", err)
	}
	if !strings.Contains(out, "工具报错") || !strings.Contains(out, "没有这个文件") {
		t.Errorf("没把原因交给模型：%q", out)
	}
}

// 非文字内容只说一声有，别把 base64 塞进上下文。
func TestNonTextContent(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		if method == "tools/call" {
			return okLine(id, `{"content":[{"type":"text","text":"截好了"},{"type":"image","data":"`+strings.Repeat("A", 500)+`"}]}`)
		}
		return okLine(id, `{}`)
	})
	out, _ := c.Call(context.Background(), "shot", nil)
	if strings.Contains(out, "AAAA") {
		t.Errorf("把图片数据塞进来了：%d 字节", len(out))
	}
	if !strings.Contains(out, "[image]") || !strings.Contains(out, "截好了") {
		t.Errorf("输出不对：%q", out)
	}
}

// server 一声不响地断了，要给一句说得清的错，不能挂死。
func TestServerGone(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		return "" // 什么都不回，然后 handle 返回后 goroutine 继续等下一条
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := c.Handshake(ctx)
	if err == nil {
		t.Fatal("server 不回话却成功了")
	}
	if !strings.Contains(err.Error(), "假的") {
		t.Errorf("错误里没说是哪个 server：%v", err)
	}
}

// server 回 JSON-RPC 错误，要原样带出原因。
func TestRPCError(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		if id == nil {
			return ""
		}
		return `{"jsonrpc":"2.0","id":` + itoa(*id) +
			`,"error":{"code":-32601,"message":"Method not found"}}` + "\n"
	})
	err := c.Handshake(context.Background())
	if err == nil {
		t.Fatal("该报错")
	}
	if !strings.Contains(err.Error(), "Method not found") || !strings.Contains(err.Error(), "-32601") {
		t.Errorf("错误信息丢了细节：%v", err)
	}
}

// **ctx 没给期限也不许挂死。**
//
// 写这个包时自己踩过：一个 server 往 stdout 打了两行日志、然后不回话，
// 拿 context.Background() 调就永远挂着——测试挂到 120 秒超时才被发现。
// 挂死是最难排查的失败，所以客户端自己兜一个期限。
func TestNeverHangsWithoutDeadline(t *testing.T) {
	c := fake(t, func(method string, id *int, params json.RawMessage) string {
		return "listening on stdio...\n" // 只打日志，永远不回答
	})
	// 把兜底期限临时调短，否则这条测试要等 60 秒。
	old := HandshakeTimeout
	HandshakeTimeout = 300 * time.Millisecond
	defer func() { HandshakeTimeout = old }()

	done := make(chan error, 1)
	go func() { done <- c.Handshake(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("该超时的")
		}
		if !strings.Contains(err.Error(), "超时") {
			t.Errorf("错误里没说是超时：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("挂死了——ctx 没给期限时客户端没自己兜住")
	}
}
