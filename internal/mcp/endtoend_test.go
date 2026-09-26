package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// 真起一个子进程、真走管子、真跑一遍协议。
//
// 前面那些测试拿内存管道换掉了传输，验的是协议那一层；这一条验的是**传输本身**
// ——stdin/stdout 接没接对、EOF 怎么处理、关的时候进程收不收得掉。
//
// 扮演 server 的就是这个测试二进制自己（下面 TestFakeServerProcess），Go 测试里
// 的老办法：不用往仓库里放第二个可执行文件，也不需要装 node。
//
// **顺带验了容错**：子进程是 `go test` 起的，它自己会往 stdout 打「PASS」之类的
// 东西，那些正是客户端必须能跳过的垃圾行——这儿的垃圾是真的，不是编出来的。
func TestStdioEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tr, err := Spawn(ctx, os.Args[0],
		[]string{"-test.run=TestFakeServerProcess", "-test.v=false"},
		[]string{"JARVIS_MCP_FAKE=1"})
	if err != nil {
		t.Fatalf("起不来：%v", err)
	}
	c := New("子进程", tr)
	defer c.Close()

	if c.Name() != "子进程" {
		t.Errorf("名字不对：%q", c.Name())
	}
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("握手失败：%v（stderr：%s）", err, tr.Stderr())
	}
	if c.Info.Name != "端到端" {
		t.Errorf("server 自报的名字不对：%+v", c.Info)
	}
	tools, err := c.Tools(ctx)
	if err != nil {
		t.Fatalf("列工具失败：%v（stderr：%s）", err, tr.Stderr())
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("工具不对：%+v", tools)
	}
	out, err := c.Call(ctx, "echo", json.RawMessage(`{"text":"过了一整圈"}`))
	if err != nil {
		t.Fatalf("调用失败：%v（stderr：%s）", err, tr.Stderr())
	}
	if !strings.Contains(out, "过了一整圈") {
		t.Errorf("输出不对：%q", out)
	}
}

// TestFakeServerProcess 不是测试，是被上面那条当 MCP server 起的子进程。
//
// 没有那个环境变量就跳过——`go test ./...` 正常跑的时候它什么都不做。
func TestFakeServerProcess(t *testing.T) {
	if os.Getenv("JARVIS_MCP_FAKE") != "1" {
		t.Skip("这是给 TestStdioEndToEnd 当子进程用的，不是一条测试")
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for in.Scan() {
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &msg) != nil {
			continue
		}
		if msg.ID == nil {
			continue // 通知，不用回
		}
		var result string
		switch msg.Method {
		case "initialize":
			result = `{"protocolVersion":"` + protocolVersion +
				`","serverInfo":{"name":"端到端","version":"1"}}`
		case "tools/list":
			result = `{"tools":[{"name":"echo","description":"原样回一句",` +
				`"inputSchema":{"type":"object","properties":{"text":{"type":"string"}}}}]}`
		case "tools/call":
			var p struct {
				Args struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			b, _ := json.Marshal(p.Args.Text)
			result = `{"content":[{"type":"text","text":` + string(b) + `}]}`
		default:
			result = `{}`
		}
		fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%s}\n", *msg.ID, result)
	}
	// stdin 关掉就是「你可以退了」——Stdio.Close 先关它，就是等这一下。
}
