package acp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/session"
)

// 这些用例从客户端一侧驱动整条 ACP 连接：真的 JSON 进出，模型换成按脚本作答的
// 假 Transport，工具换成一个记次数的假工具。测的是协议的形状和生命周期——
// 每个工具卡片都收尾、审批往返、取消、会话存了能列能回放能删。

type scripted struct {
	mu      sync.Mutex
	replies []string
	// gate 非空时，每次请求前先等它放行（用来卡住一轮、测取消）。
	gate chan struct{}
}

func (t *scripted) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.gate != nil {
		select {
		case <-t.gate:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.replies) == 0 {
		return nil, fmt.Errorf("脚本用完了")
	}
	body := t.replies[0]
	t.replies = t.replies[1:]
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)),
		Header: http.Header{"Content-Type": []string{"application/json"}}}, nil
}

func say(text string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":%q}}]}`, text)
}

func callTool(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":"我先办一下。","tool_calls":[
		{"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`, id, name, args)
}

type sendTool struct{ runs int }

func (t *sendTool) Spec() agent.ToolSpec {
	return agent.ToolSpec{Name: "send_draft", Description: "发信", Parameters: map[string]any{}}
}
func (t *sendTool) Risk() agent.Risk { return agent.RiskIrreversible }
func (t *sendTool) Preview(string) (string, string, error) {
	return "发信给张三", "主题：周报", nil
}
func (t *sendTool) Run(string) (string, bool, error) { t.runs++; return "已发出", true, nil }

type skill struct{ tools []agent.Tool }

func (skill) Name() string                { return "邮件" }
func (skill) Describe(b *strings.Builder) {}
func (skill) Instructions() string        { return "" }
func (s skill) Tools() []agent.Tool       { return s.tools }

type fakeHost struct {
	tr   *scripted
	tool *sendTool
}

func (h *fakeHost) NewAgent() (*agent.Agent, error) {
	c := &agent.Client{BaseURL: "http://模型不存在/v1", Model: "测试", SupportsTools: true,
		HTTP: &http.Client{Transport: h.tr}}
	a := agent.NewAgent(c, []agent.Skill{skill{tools: []agent.Tool{h.tool}}}, 10, agent.AutoOff)
	a.Stream = false
	a.AutoCompact = false
	return a, nil
}
func (h *fakeHost) Models() ([]string, string)          { return []string{"快", "省"}, "快" }
func (h *fakeHost) UseModel(*agent.Agent, string) error { return nil }

// client 是测试里的 ACP 客户端。
type client struct {
	t   *testing.T
	in  *bufio.Scanner
	out io.Writer
	id  int
	// onRequest 答复服务端发来的请求（审批）。
	onRequest func(method string, params map[string]any) any
	updates   []map[string]any
}

func start(t *testing.T, h *fakeHost) (*client, *session.Store) {
	t.Helper()
	store := session.New(t.TempDir())
	toServer, clientOut := io.Pipe()
	clientIn, fromServer := io.Pipe()
	srv := New(h, store, "test")
	srv.home = "/家"
	done := make(chan struct{})
	go func() { _ = srv.Serve(toServer, fromServer); _ = fromServer.Close(); close(done) }()
	t.Cleanup(func() {
		_ = clientOut.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("服务端没退出")
		}
	})
	sc := bufio.NewScanner(clientIn)
	sc.Buffer(make([]byte, 1<<16), 1<<22)
	return &client{t: t, in: sc, out: clientOut}, store
}

func (c *client) send(v any) {
	raw, _ := json.Marshal(v)
	_, _ = c.out.Write(append(raw, '\n'))
}

// call 发一个请求，一路收通知、答复服务端的请求，直到拿到自己的响应。
func (c *client) call(method string, params any) map[string]any {
	c.t.Helper()
	c.id++
	c.send(map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method, "params": params})
	return c.await(c.id)
}

// await 收消息直到 id 为 my 的响应到达。
func (c *client) await(my int) map[string]any {
	c.t.Helper()
	for c.in.Scan() {
		var m map[string]any
		if err := json.Unmarshal(c.in.Bytes(), &m); err != nil {
			c.t.Fatalf("服务端吐出了不是 JSON 的东西：%s", c.in.Text())
		}
		method, _ := m["method"].(string)
		params, _ := m["params"].(map[string]any)
		switch {
		case method != "" && m["id"] != nil:
			var result any = map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
			if c.onRequest != nil {
				result = c.onRequest(method, params)
			}
			c.send(map[string]any{"jsonrpc": "2.0", "id": m["id"], "result": result})
		case method == "session/update":
			c.updates = append(c.updates, params["update"].(map[string]any))
		case method != "":
		default:
			if id, _ := m["id"].(float64); int(id) == my {
				return m
			}
		}
	}
	c.t.Fatal("连接断了")
	return nil
}

func (c *client) kinds() []string {
	var out []string
	for _, u := range c.updates {
		out = append(out, u["sessionUpdate"].(string))
	}
	return out
}

func (c *client) text(kind string) string {
	var b strings.Builder
	for _, u := range c.updates {
		if u["sessionUpdate"] == kind {
			b.WriteString(u["content"].(map[string]any)["text"].(string))
		}
	}
	return b.String()
}

func result(t *testing.T, m map[string]any) map[string]any {
	t.Helper()
	if m["error"] != nil {
		t.Fatalf("请求失败：%v", m["error"])
	}
	r, _ := m["result"].(map[string]any)
	return r
}

func TestPromptWithApprovalThenListLoadDelete(t *testing.T) {
	h := &fakeHost{tr: &scripted{replies: []string{
		callTool("c1", "send_draft", `{"to":"张三"}`),
		callTool("c2", "send_draft", `{"to":"李四"}`),
		say("两封都发了，见 [周报](jarvis://mail/3)。"),
	}}, tool: &sendTool{}}
	c, store := start(t, h)

	init := result(t, c.call("initialize", map[string]any{"protocolVersion": 1}))
	if caps := init["agentCapabilities"].(map[string]any); caps["loadSession"] != true {
		t.Fatalf("该声明 loadSession：%v", caps)
	}
	created := result(t, c.call("session/new", map[string]any{"cwd": "/项目", "mcpServers": []any{}}))
	sid := created["sessionId"].(string)
	if created["modes"].(map[string]any)["currentModeId"] != "off" {
		t.Fatalf("档位不对：%v", created["modes"])
	}
	if opts := created["configOptions"].([]any); len(opts) != 1 {
		t.Fatalf("该带模型选项：%v", opts)
	}

	var asked []map[string]any
	c.onRequest = func(method string, params map[string]any) any {
		if method != "session/request_permission" {
			t.Fatalf("没想到的请求 %s", method)
		}
		asked = append(asked, params)
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "always"}}
	}
	r := result(t, c.call("session/prompt", map[string]any{"sessionId": sid,
		"prompt": []any{map[string]any{"type": "text", "text": "把两封信发了"}}}))
	if r["stopReason"] != "end_turn" {
		t.Fatalf("stopReason = %v", r["stopReason"])
	}
	// 第一封问了、选了「以后不再问」，第二封就不该再问。
	if len(asked) != 1 || h.tool.runs != 2 {
		t.Fatalf("问了 %d 次、发了 %d 封", len(asked), h.tool.runs)
	}
	call := asked[0]["toolCall"].(map[string]any)
	if call["toolCallId"] != "c1" || call["title"] != "发信给张三" {
		t.Fatalf("审批请求的卡片不对：%v", call)
	}
	opened, closed := map[string]bool{}, map[string]string{}
	for _, u := range c.updates {
		switch u["sessionUpdate"] {
		case "tool_call":
			opened[u["toolCallId"].(string)] = true
			if u["_meta"].(map[string]any)["jarvis"].(map[string]any)["toolName"] != "send_draft" {
				t.Fatalf("工具名该在 _meta.jarvis 里：%v", u)
			}
		case "tool_call_update":
			closed[u["toolCallId"].(string)] = u["status"].(string)
		}
	}
	if len(opened) != 2 || closed["c1"] != "completed" || closed["c2"] != "completed" {
		t.Fatalf("工具卡片没收干净：开了 %v，收了 %v", opened, closed)
	}
	if !contains(c.kinds(), "current_mode_update") || !contains(c.kinds(), "session_info_update") {
		t.Fatalf("少了档位或标题的推送：%v", c.kinds())
	}
	if got := c.text("agent_message_chunk"); !strings.Contains(got, "两封都发了，见 周报。") {
		t.Fatalf("答复没推出来，或者内部链接没去掉：%q", got)
	}

	// 档位那条 system 消息该排在这一轮之后，不能插在 tool_calls 和结果中间。
	h2, _ := store.Load(sid)
	for i, m := range h2 {
		if m.Role == "system" && i+1 < len(h2) {
			t.Fatalf("system 消息插在了第 %d 条（共 %d 条）", i, len(h2))
		}
	}

	list := result(t, c.call("session/list", map[string]any{"cwd": "/项目"}))
	sessions := list["sessions"].([]any)
	if len(sessions) != 1 || sessions[0].(map[string]any)["title"] != "把两封信发了" {
		t.Fatalf("列表不对：%v", sessions)
	}
	if other := result(t, c.call("session/list", map[string]any{"cwd": "/别处"})); len(other["sessions"].([]any)) != 0 {
		t.Fatalf("别的目录不该列出这个会话：%v", other)
	}

	c.updates = nil
	loaded := result(t, c.call("session/load", map[string]any{"sessionId": sid, "cwd": "/项目", "mcpServers": []any{}}))
	if loaded["modes"].(map[string]any)["currentModeId"] == nil {
		t.Fatal("load 该回会话状态")
	}
	want := []string{"user_message_chunk", "agent_message_chunk", "tool_call", "tool_call_update",
		"agent_message_chunk", "tool_call", "tool_call_update", "agent_message_chunk"}
	if got := c.kinds(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("回放顺序不对：\n%v\n想要\n%v", got, want)
	}

	result(t, c.call("session/delete", map[string]any{"sessionId": sid}))
	if store.Exists(sid) {
		t.Fatal("删了会话文件还在")
	}
	if m := c.call("session/load", map[string]any{"sessionId": sid, "cwd": "/项目"}); m["error"].(map[string]any)["code"] != float64(codeNotFound) {
		t.Fatalf("删掉的会话该回 not found：%v", m)
	}
}

func TestRejectAndCancel(t *testing.T) {
	gate := make(chan struct{}, 10)
	h := &fakeHost{tr: &scripted{gate: gate, replies: []string{
		callTool("c1", "send_draft", `{}`), say("那就不发了。"),
		callTool("c2", "send_draft", `{}`),
	}}, tool: &sendTool{}}
	c, _ := start(t, h)
	sid := result(t, c.call("session/new", map[string]any{"cwd": "/项目"}))["sessionId"].(string)

	// 拒绝：卡片以 failed 收尾，工具没跑。
	gate <- struct{}{}
	gate <- struct{}{}
	c.onRequest = func(string, map[string]any) any {
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "reject"}}
	}
	result(t, c.call("session/prompt", map[string]any{"sessionId": sid,
		"prompt": []any{map[string]any{"type": "text", "text": "发信"}}}))
	if h.tool.runs != 0 {
		t.Fatal("拒绝了还是发了")
	}
	var status any
	for _, u := range c.updates {
		if u["sessionUpdate"] == "tool_call_update" {
			status = u["status"]
		}
	}
	if status != "failed" {
		t.Fatalf("拒绝后卡片该以 failed 收尾：%v", c.updates)
	}

	// 取消：审批挂着的时候发 session/cancel，这一轮以 cancelled 结束。
	gate <- struct{}{}
	c.onRequest = func(string, map[string]any) any {
		c.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
		time.Sleep(50 * time.Millisecond)
		return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
	}
	r := result(t, c.call("session/prompt", map[string]any{"sessionId": sid,
		"prompt": []any{map[string]any{"type": "text", "text": "再发一次"}}}))
	if r["stopReason"] != "cancelled" {
		t.Fatalf("stopReason = %v", r["stopReason"])
	}
}

func TestBusyAndUnknown(t *testing.T) {
	gate := make(chan struct{})
	h := &fakeHost{tr: &scripted{gate: gate, replies: []string{say("好")}}, tool: &sendTool{}}
	c, _ := start(t, h)
	sid := result(t, c.call("session/new", map[string]any{"cwd": "/项目"}))["sessionId"].(string)

	// 第一轮卡在模型那儿，第二轮该被拒；set_mode 留到答完再办。
	c.send(map[string]any{"jsonrpc": "2.0", "id": 100, "method": "session/prompt",
		"params": map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "一"}}}})
	time.Sleep(50 * time.Millisecond)
	if m := c.call("session/prompt", map[string]any{"sessionId": sid,
		"prompt": []any{map[string]any{"type": "text", "text": "二"}}}); m["error"] == nil {
		t.Fatal("上一轮没答完，该拒")
	}
	result(t, c.call("session/set_mode", map[string]any{"sessionId": sid, "modeId": "all"}))
	if m := c.call("session/set_mode", map[string]any{"sessionId": sid, "modeId": "乱写"}); m["error"] == nil {
		t.Fatal("不认识的档位该报错")
	}
	if m := c.call("没有这个方法", map[string]any{}); m["error"].(map[string]any)["code"] != float64(codeMethodNotFound) {
		t.Fatalf("不认识的方法该回 method not found：%v", m)
	}
	close(gate)
	result(t, c.await(100))
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
