package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/session"
)

// Host 是装配那一侧交进来的东西：怎么造秘书、有哪些模型可选。
type Host interface {
	// NewAgent 造一个全新的秘书。每个会话一个，互不共享历史。
	NewAgent() (*agent.Agent, error)
	// Models 是可选的几套模型（名字）和当前配置在用的那套；没存备选时返回空。
	Models() (names []string, current string)
	// UseModel 让这个秘书改用另一套模型。只影响这一个会话，不改配置文件。
	UseModel(a *agent.Agent, name string) error
}

// Server 是一条 ACP 连接：一个客户端，任意多个会话。
type Server struct {
	host    Host
	store   *session.Store
	version string
	// home 是没记目录的会话（终端里开的）归属的目录。Jarvis 本来没有
	// 「项目目录」这回事，客户端却按目录列会话，总得给它们找个家。
	home string

	c        *conn
	mu       sync.Mutex
	sessions map[string]*sess
}

// sess 是一个活着的会话。
type sess struct {
	id  string
	cwd string
	ag  *agent.Agent

	mu sync.Mutex
	// cancel 非空表示这一轮还在答。
	cancel context.CancelFunc
	// model 是这个会话选的那套模型，空表示跟配置走。
	model string
	// afterTurn 是这一轮答完才能办的事：答到一半往历史里插 system 消息，
	// 会把 assistant 的 tool_calls 和对应的 tool 结果隔开，模型接口直接报错。
	afterTurn []func()
	title     string
}

// New 造一个服务端。store 是会话目录（和终端界面共用，`jarvis -c` 也能接上
// ACP 里聊过的会话），version 填进 initialize 的 agentInfo。
func New(host Host, store *session.Store, version string) *Server {
	home, _ := os.UserHomeDir()
	return &Server{host: host, store: store, version: version, home: home, sessions: map[string]*sess{}}
}

// Serve 读 in、写 out，直到 in 关掉。
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	s.c = newConn(in, out)
	for s.c.in.Scan() {
		var m message
		if err := json.Unmarshal(s.c.in.Bytes(), &m); err != nil {
			_ = s.c.fail(json.RawMessage("null"), codeParse, "不是合法的 JSON")
			continue
		}
		switch {
		case m.Method == "" && len(m.ID) > 0:
			s.c.deliver(m)
		case len(m.ID) == 0:
			s.handleNotification(m)
		default:
			s.handleRequest(m)
		}
	}
	s.mu.Lock()
	for _, ss := range s.sessions {
		ss.stop()
	}
	s.mu.Unlock()
	return s.c.in.Err()
}

func (s *Server) handleNotification(m message) {
	switch m.Method {
	case "session/cancel":
		var p struct {
			SessionID string `json:"sessionId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		if ss := s.lookup(p.SessionID); ss != nil {
			ss.stop()
		}
	}
}

func (s *Server) handleRequest(m message) {
	var result any
	var err error
	switch m.Method {
	case "initialize":
		result = s.initialize()
	case "authenticate":
		result = struct{}{}
	case "session/new":
		result, err = s.newSession(m.Params)
	case "session/load":
		result, err = s.loadSession(m.Params)
	case "session/list":
		result, err = s.listSessions(m.Params)
	case "session/prompt":
		// 一轮要答很久，中间还要反过来问客户端要不要批准——不能占着读循环。
		p, err := s.beginPrompt(m)
		if err != nil {
			s.answer(m.ID, nil, err)
			return
		}
		go s.runPrompt(p)
		return
	case "session/set_mode":
		result, err = s.setMode(m.Params)
	case "session/set_config_option":
		result, err = s.setConfigOption(m.Params)
	case "session/close":
		result, err = s.closeSession(m.Params)
	case "session/delete":
		result, err = s.deleteSession(m.Params)
	default:
		err = &rpcError{Code: codeMethodNotFound, Message: "不支持 " + m.Method}
	}
	s.answer(m.ID, result, err)
}

func (s *Server) answer(id json.RawMessage, result any, err error) {
	if err == nil {
		_ = s.c.reply(id, result)
		return
	}
	var re *rpcError
	if errors.As(err, &re) {
		_ = s.c.fail(id, re.Code, re.Message)
		return
	}
	_ = s.c.fail(id, codeInternal, err.Error())
}

// MARK: 握手

func (s *Server) initialize() any {
	return map[string]any{
		"protocolVersion": 1,
		"agentCapabilities": map[string]any{
			"loadSession": true,
			// 图片随这一轮发给模型；看不看得懂取决于配的模型。
			"promptCapabilities":  map[string]any{"image": true, "audio": false, "embeddedContext": true},
			"mcpCapabilities":     map[string]any{"http": false, "sse": false},
			"sessionCapabilities": map[string]any{"list": map[string]any{}, "close": map[string]any{}, "delete": map[string]any{}},
		},
		"agentInfo":   map[string]any{"name": "jarvis", "title": "Jarvis", "version": s.version},
		"authMethods": []any{},
	}
}

// MARK: 会话

// validID 防的是客户端塞个 "../x" 进来：会话 id 直接拼成了文件名。
var validID = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._-]*$`)

func (s *Server) lookup(id string) *sess {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) mustLookup(id string) (*sess, error) {
	if ss := s.lookup(id); ss != nil {
		return ss, nil
	}
	return nil, &rpcError{Code: codeNotFound, Message: "没有这个会话：" + id}
}

func (s *Server) open(id, cwd string, history []agent.Message) (*sess, error) {
	ag, err := s.host.NewAgent()
	if err != nil {
		// 多半是还没配模型。原样说给客户端，用户在那边就能看到怎么办。
		return nil, &rpcError{Code: codeInternal, Message: err.Error()}
	}
	ag.History = history
	ss := &sess{id: id, cwd: cwd, ag: ag}
	s.mu.Lock()
	if old := s.sessions[id]; old != nil {
		old.stop()
	}
	s.sessions[id] = ss
	s.mu.Unlock()
	return ss, nil
}

type sessionParams struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

func (s *Server) newSession(raw json.RawMessage) (any, error) {
	var p sessionParams
	_ = json.Unmarshal(raw, &p)
	id := s.store.NewID()
	ss, err := s.open(id, p.CWD, nil)
	if err != nil {
		return nil, err
	}
	// 目录先记下；会话文件要等第一轮答完才有，没说过话的会话不会出现在列表里。
	if err := s.store.SaveMeta(id, session.Meta{CWD: p.CWD}); err != nil {
		return nil, err
	}
	return s.sessionState(ss), nil
}

func (s *Server) loadSession(raw json.RawMessage) (any, error) {
	var p sessionParams
	_ = json.Unmarshal(raw, &p)
	if !validID.MatchString(p.SessionID) || !s.store.Exists(p.SessionID) {
		return nil, &rpcError{Code: codeNotFound, Message: "没有这个会话：" + p.SessionID}
	}
	history, err := s.store.Load(p.SessionID)
	if err != nil {
		return nil, err
	}
	meta := s.store.LoadMeta(p.SessionID)
	cwd := meta.CWD
	if cwd == "" {
		cwd = p.CWD
	}
	ss, err := s.open(p.SessionID, cwd, history)
	if err != nil {
		return nil, err
	}
	ss.title = meta.Title
	// 规范要求：先把历史整段用 session/update 推一遍，再回这个请求。
	for _, u := range replay(history) {
		s.update(ss.id, u)
	}
	return s.sessionState(ss), nil
}

func (s *Server) listSessions(raw json.RawMessage) (any, error) {
	var p struct {
		CWD string `json:"cwd"`
	}
	_ = json.Unmarshal(raw, &p)
	out := []map[string]any{}
	for _, info := range s.store.List() {
		cwd := info.Meta.CWD
		if cwd == "" {
			cwd = s.home
		}
		if p.CWD != "" && filepath.Clean(cwd) != filepath.Clean(p.CWD) {
			continue
		}
		title := info.Meta.Title
		if title == "" {
			// 终端里开的会话没存标题，现读一遍拿第一句话。
			if h, err := s.store.Load(info.ID); err == nil {
				title = titleOf(h)
			}
		}
		entry := map[string]any{"sessionId": info.ID, "cwd": cwd,
			"updatedAt": info.UpdatedAt.UTC().Format(time.RFC3339)}
		if title != "" {
			entry["title"] = title
		}
		out = append(out, entry)
	}
	return map[string]any{"sessions": out}, nil
}

func (s *Server) closeSession(raw json.RawMessage) (any, error) {
	var p sessionParams
	_ = json.Unmarshal(raw, &p)
	s.mu.Lock()
	ss := s.sessions[p.SessionID]
	delete(s.sessions, p.SessionID)
	s.mu.Unlock()
	if ss != nil {
		ss.stop()
	}
	return struct{}{}, nil
}

func (s *Server) deleteSession(raw json.RawMessage) (any, error) {
	var p sessionParams
	_ = json.Unmarshal(raw, &p)
	if !validID.MatchString(p.SessionID) {
		return nil, &rpcError{Code: codeInvalidParams, Message: "会话 id 不对：" + p.SessionID}
	}
	if _, err := s.closeSession(raw); err != nil {
		return nil, err
	}
	return struct{}{}, s.store.Delete(p.SessionID)
}

// MARK: 档位与模型

// 档位就是 ACP 的「模式」。id 用配置文件里的写法，客户端切过来的值
// 直接能交给 agent.ParseAutoLevel。
var modes = []struct {
	id    string
	level agent.AutoLevel
	desc  string
}{
	{"off", agent.AutoOff, "有风险的动作（清理、发信）都先问"},
	{"clean", agent.AutoClean, "可逆的整理直接做，发信仍要确认"},
	{"all", agent.AutoAll, "清理和发信都不再问"},
}

func modeID(l agent.AutoLevel) string {
	for _, m := range modes {
		if m.level == l {
			return m.id
		}
	}
	return "off"
}

func (s *Server) sessionState(ss *sess) map[string]any {
	available := make([]map[string]any, 0, len(modes))
	for _, m := range modes {
		available = append(available, map[string]any{"id": m.id, "name": m.level.Label(), "description": m.desc})
	}
	ss.mu.Lock()
	cur := ss.ag.Auto
	ss.mu.Unlock()
	state := map[string]any{
		"sessionId": ss.id,
		"modes":     map[string]any{"currentModeId": modeID(cur), "availableModes": available},
	}
	if opts := s.configOptions(ss); len(opts) > 0 {
		state["configOptions"] = opts
	}
	return state
}

func (s *Server) configOptions(ss *sess) []map[string]any {
	names, current := s.host.Models()
	if len(names) == 0 {
		return nil
	}
	ss.mu.Lock()
	if ss.model != "" {
		current = ss.model
	}
	ss.mu.Unlock()
	choices := make([]map[string]any, 0, len(names))
	for _, n := range names {
		choices = append(choices, map[string]any{"value": n, "name": n})
	}
	return []map[string]any{{"id": "model", "name": "模型", "category": "model", "type": "select",
		"currentValue": current, "options": choices}}
}

func (s *Server) setMode(raw json.RawMessage) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
		ModeID    string `json:"modeId"`
	}
	_ = json.Unmarshal(raw, &p)
	ss, err := s.mustLookup(p.SessionID)
	if err != nil {
		return nil, err
	}
	known := false
	for _, m := range modes {
		known = known || m.id == p.ModeID
	}
	if !known {
		return nil, &rpcError{Code: codeInvalidParams, Message: "没有这个档位：" + p.ModeID}
	}
	lv := agent.ParseAutoLevel(p.ModeID)
	ss.whenIdle(func() { ss.ag.SetAuto(lv) })
	return struct{}{}, nil
}

func (s *Server) setConfigOption(raw json.RawMessage) (any, error) {
	var p struct {
		SessionID string `json:"sessionId"`
		ConfigID  string `json:"configId"`
		Value     string `json:"value"`
	}
	_ = json.Unmarshal(raw, &p)
	ss, err := s.mustLookup(p.SessionID)
	if err != nil {
		return nil, err
	}
	if p.ConfigID != "model" {
		return nil, &rpcError{Code: codeInvalidParams, Message: "没有这个设置：" + p.ConfigID}
	}
	names, _ := s.host.Models()
	found := false
	for _, n := range names {
		found = found || n == p.Value
	}
	if !found {
		return nil, &rpcError{Code: codeInvalidParams, Message: "没有这套模型：" + p.Value}
	}
	var useErr error
	ss.whenIdle(func() { useErr = s.host.UseModel(ss.ag, p.Value) })
	if useErr != nil {
		return nil, useErr
	}
	ss.mu.Lock()
	ss.model = p.Value
	ss.mu.Unlock()
	return map[string]any{"configOptions": s.configOptions(ss)}, nil
}

// whenIdle 在会话空着时立刻办，正在答就留到这一轮答完。
func (ss *sess) whenIdle(f func()) {
	ss.mu.Lock()
	if ss.cancel != nil {
		ss.afterTurn = append(ss.afterTurn, f)
		ss.mu.Unlock()
		return
	}
	ss.mu.Unlock()
	f()
}

func (ss *sess) stop() {
	ss.mu.Lock()
	cancel := ss.cancel
	ss.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// MARK: 推送

func (s *Server) update(sessionID string, u map[string]any) {
	_ = s.c.notify("session/update", map[string]any{"sessionId": sessionID, "update": u})
}

func textChunk(kind, text string) map[string]any {
	return map[string]any{"sessionUpdate": kind, "content": map[string]any{"type": "text", "text": text}}
}

// titleOf 取第一句用户的话当标题。
func titleOf(h []agent.Message) string {
	for _, m := range h {
		if m.Role != "user" {
			continue
		}
		t := strings.Join(strings.Fields(m.Content), " ")
		if t == "" {
			continue
		}
		if utf8.RuneCountInString(t) > 40 {
			t = string([]rune(t)[:40]) + "…"
		}
		return t
	}
	return ""
}
