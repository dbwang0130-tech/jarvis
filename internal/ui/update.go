package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/org"
)

// agentEventMsg 是秘书工作过程中的一条进度。
type agentEventMsg agent.Event

// compactDoneMsg 是手动压缩上下文的结果。
type compactDoneMsg struct {
	before, after int
	done          bool
	err           error
}

// mailSyncMsg 是邮箱增量同步的结果。
type mailSyncMsg struct{ res mail.FetchResult }

// mailLoadMsg 是往前翻一页的结果。
type mailLoadMsg struct{ res mail.FetchResult }

// Update 实现 tea.Model。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = maxInt(20, msg.Width-10)
		return m, nil

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.mouseScroll(wheelDelta(-3))
		case tea.MouseButtonWheelDown:
			m.mouseScroll(wheelDelta(3))
		case tea.MouseButtonLeft:
			if msg.Action == tea.MouseActionPress && m.detail == nil && m.view != viewChat {
				return m.clickRow(msg.Y)
			}
		}
		return m, nil

	case spinner.TickMsg:
		if !m.busy && !m.verifying {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case editDoneMsg:
		m.applyEdit(msg)
		if m.view == viewChat && !m.chatBrowse && !m.busy {
			return m, m.focusChat()
		}
		return m, nil

	case compactDoneMsg:
		m.busy = false
		switch {
		case msg.err != nil:
			m.setError(msg.err)
		case !msg.done:
			m.chatNote("对话还不长，没有可压缩的部分。")
		default:
			m.chatNote(fmt.Sprintf("上下文已压缩：约 %d → %d tokens", msg.before, msg.after))
		}
		if m.view == viewChat && !m.chatBrowse {
			return m, m.focusChat()
		}
		return m, nil

	case mailSyncMsg:
		return m.onMailSync(msg.res)
	case mailLoadMsg:
		return m.onMailLoad(msg.res)

	case agentEventMsg:
		return m.onAgentEvent(msg)

	case loginResultMsg:
		m.verifying = false
		if msg.err != nil {
			m.chat = append(m.chat, chatEntry{role: "error", text: "密钥校验未通过：" + msg.err.Error()})
			m.setError(msg.err)
		} else {
			m.chatNote(fmt.Sprintf("密钥可用，当前模型 %s。现在可以直接吩咐了。", msg.model))
		}
		return m, nil

	case tea.KeyMsg:
		if m.agentPending != nil {
			return m.handleAgentConfirm(msg)
		}
		if key.Matches(msg, keys.ToggleMouse) {
			return m.toggleMouse()
		}
		if m.prompt == promptConfirm {
			return m.updateConfirm(msg)
		}
		if m.detail != nil {
			return m.updateDetail(msg)
		}
		if m.prompt != promptNone {
			return m.updatePrompt(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

// onAgentEvent 处理秘书进度事件。
func (m *Model) onAgentEvent(msg agentEventMsg) (tea.Model, tea.Cmd) {
	switch msg.Kind {
	case agent.EventDelta:
		m.appendAgentText(msg.Text)
	case agent.EventReasoning:
		m.setStatus("思考中…%s", lastRunes(msg.Text, 30))
	case agent.EventCompact:
		m.endStreaming()
		m.chat = append(m.chat, chatEntry{role: "note", text: msg.Text})
	case agent.EventAuto:
		m.endStreaming()
		m.chat = append(m.chat, chatEntry{role: "note", text: "已自动执行：" + msg.Text})
	case agent.EventToolCall:
		m.endStreaming()
		m.chat = append(m.chat, chatEntry{role: "toolcall", text: formatCall(agent.Event(msg))})
	case agent.EventError:
		m.endStreaming()
		m.chat = append(m.chat, chatEntry{role: "toolerror", text: msg.Text})
	case agent.EventPending:
		m.endStreaming()
		m.agentPending = msg.Pending
		m.chat = append(m.chat, chatEntry{role: "confirm", text: msg.Pending.Title + "\n" + msg.Pending.Detail})
		return m, nil
	case agent.EventDone:
		m.busy = false
		m.cancelAgent = nil
		streamed := m.endStreaming()
		switch {
		case agent.IsCanceled(msg.Err):
			m.chatNote("（已打断）")
			m.setStatus("已打断秘书")
		case msg.Err != nil:
			m.chat = append(m.chat, chatEntry{role: "error", text: msg.Err.Error()})
		case msg.Text != "" && strings.TrimSpace(streamed) != strings.TrimSpace(msg.Text):
			m.chat = append(m.chat, chatEntry{role: "agent", text: msg.Text})
		}
		m.rebuild()
		if n := len(m.orgWS.Changes); n > 0 {
			m.setStatus("秘书做了 %d 处改动，按 u 可撤销", n)
		}
		return m, nil
	default:
		m.endStreaming()
		m.chat = append(m.chat, chatEntry{role: "toolresult", text: msg.Text})
	}
	if msg.Changed {
		switch msg.Tool {
		case "set_config", "remove_mail_account":
			m.reloadConfig()
		default:
			m.rebuild()
		}
	}
	return m, waitEvent(m.agentEvents)
}

// handleAgentConfirm 处理秘书动作的确认。
func (m *Model) handleAgentConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.agentPending
	m.agentPending = nil
	switch strings.ToLower(k.String()) {
	case "y":
		m.chat = append(m.chat, chatEntry{role: "note", text: "✓ 已确认"})
		p.Reply <- true
	default:
		m.chat = append(m.chat, chatEntry{role: "note", text: "✗ 已取消"})
		p.Reply <- false
	}
	return m, waitEvent(m.agentEvents)
}

// syncMailCmd 后台同步邮箱；未配置账号时返回 nil。
func (m *Model) syncMailCmd() tea.Cmd {
	if m.mailS == nil || !m.mailS.HasAccounts() {
		return nil
	}
	return func() tea.Msg { return mailSyncMsg{res: m.mailS.Sync()} }
}

func (m *Model) loadMailCmd() tea.Cmd {
	if m.mailS == nil || !m.mailS.HasAccounts() {
		return nil
	}
	return func() tea.Msg { return mailLoadMsg{res: m.mailS.LoadMore(false)} }
}

func (m *Model) onMailSync(res mail.FetchResult) (tea.Model, tea.Cmd) {
	for name, err := range res.Errors {
		m.chat = append(m.chat, chatEntry{role: "error", text: name + "：" + err.Error()})
	}
	for _, name := range res.FirstSync {
		line := name + " 是首次同步，把收件箱整个过了一遍"
		if n, ok := res.Remaining[name]; ok {
			line += fmt.Sprintf("（还有 %d 封更早的没拉，Ctrl+N 继续往前）", n)
		}
		m.chatNote(line)
	}
	for name, t := range res.Since {
		if t.IsZero() {
			continue
		}
		m.chatNote(fmt.Sprintf("%s 上次同步 %s", name, humanSince(t)))
	}
	if len(res.New) == 0 {
		m.chatNote("没有新邮件。")
		m.rebuild()
		return m, nil
	}
	m.chatNote(fmt.Sprintf("新到 %d 封，正在整理…", len(res.New)))
	m.rebuild()
	return m, m.briefNewMail(res.New)
}

func (m *Model) onMailLoad(res mail.FetchResult) (tea.Model, tea.Cmd) {
	for name, err := range res.Errors {
		m.chat = append(m.chat, chatEntry{role: "error", text: name + " 加载失败：" + err.Error()})
	}
	if res.Total == 0 {
		m.chatNote("再往前没有更多邮件了。")
	} else {
		m.chatNote(fmt.Sprintf("又加载了 %d 封更早的邮件（编号接着往下排）。", res.Total))
	}
	m.rebuild()
	return m, nil
}

// briefNewMail 让秘书给新到的邮件做 triage 并写简报。
func (m *Model) briefNewMail(newItems []*mail.Item) tea.Cmd {
	if m.agent == nil || m.mailS == nil {
		return nil
	}
	digest := m.mailS.DigestOf(newItems)
	return m.askAgent("这是刚拉取到的收件箱内容：\n\n" + digest +
		"\n\n先用 triage 给所有邮件打标签，然后结合我的日程写一份简报。")
}

// updatePrompt 处理底部输入框激活时的按键。
func (m *Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.cancelAgent != nil {
			m.cancelAgent()
		}
		return m, tea.Quit
	case "tab":
		return m.switchView((m.view + 1) % 5)
	case "esc":
		if m.busy {
			m.interruptAgent()
			return m, nil
		}
		m.prompt = promptNone
		m.input.Blur()
		m.input.SetValue("")
		if m.view == viewChat {
			m.chatBrowse = true
			m.setStatus("已退出输入，按 i 继续说话")
		}
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		kind := m.prompt
		m.prompt = promptNone
		m.input.Blur()
		m.input.SetValue("")
		return m.submitPrompt(kind, value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.prompt == promptSearch {
		m.query = m.input.Value()
		m.rebuild()
	}
	return m, cmd
}

// submitPrompt 处理输入框提交的内容。
func (m *Model) submitPrompt(kind prompt, value string) (tea.Model, tea.Cmd) {
	m.status, m.statusErr = "", false
	switch kind {
	case promptSearch:
		m.query = value
		m.rebuild()
		if value == "" {
			m.setStatus("已清除过滤")
		} else {
			m.setStatus("过滤：%s", value)
		}
	case promptSched:
		m.applyPlanning("SCHEDULED", value)
	case promptDead:
		m.applyPlanning("DEADLINE", value)
	case promptState:
		m.applyState(strings.ToUpper(value))
	case promptPrio:
		m.applyPriority(strings.ToUpper(value))
	case promptRepeat:
		m.applyRepeat(value)
	case promptCapture:
		m.applyCapture(value)
	case promptMail:
		return m.advanceMailWizard(value)
	case promptChat:
		switch {
		case value == "":
		case m.busy:
			m.setError(fmt.Errorf("秘书还在处理上一条，稍等一下"))
		case isCommand(value):
			_, cmd := m.runCommand(value)
			if m.view != viewChat || m.prompt != promptNone || m.quitting {
				return m, cmd
			}
			return m, tea.Batch(cmd, m.focusChat())
		default:
			return m, tea.Batch(m.askAgent(value), m.focusChat())
		}
		return m, m.focusChat()
	case promptLogin:
		_, cmd := m.applyLogin(value)
		return m, tea.Batch(cmd, m.focusChat())
	}
	return m, nil
}

// switchView 切换视图，进入秘书面板时自动聚焦输入框。
func (m *Model) switchView(v view) (tea.Model, tea.Cmd) {
	m.view = v
	m.rebuild()
	if v == viewChat && !m.busy {
		return m, m.focusChat()
	}
	if v != viewChat {
		m.prompt = promptNone
		m.input.Blur()
	}
	return m, nil
}

// updateNormal 处理列表模式下的按键。
func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	switch {
	case key.Matches(msg, keys.Cancel) && m.busy:
		m.interruptAgent()
	case key.Matches(msg, keys.Quit):
		if m.cancelAgent != nil {
			m.cancelAgent()
		}
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.showHelp = true
	case key.Matches(msg, keys.Down):
		m.move(1)
	case key.Matches(msg, keys.Up):
		m.move(-1)
	case key.Matches(msg, keys.Top):
		m.cursor = 0
		m.clampCursor()
	case key.Matches(msg, keys.Bottom):
		m.cursor = len(m.rows) - 1
		m.clampCursor()
	case key.Matches(msg, keys.PgDown):
		for i := 0; i < 10; i++ {
			m.move(1)
		}
	case key.Matches(msg, keys.PageUp):
		for i := 0; i < 10; i++ {
			m.move(-1)
		}

	case key.Matches(msg, keys.NextView):
		return m.switchView((m.view + 1) % 5)
	case key.Matches(msg, keys.Agenda):
		return m.switchView(viewAgenda)
	case key.Matches(msg, keys.Todos):
		return m.switchView(viewTodos)
	case key.Matches(msg, keys.Mail):
		return m.switchView(viewMail)
	case key.Matches(msg, keys.Ledger):
		return m.switchView(viewLedger)
	case key.Matches(msg, keys.Chat), key.Matches(msg, keys.Say):
		return m.enterChat()

	case key.Matches(msg, keys.DayView):
		m.days = 1
		return m.switchView(viewAgenda)
	case key.Matches(msg, keys.WeekView):
		m.days = 7
		return m.switchView(viewAgenda)
	case key.Matches(msg, keys.MonthView):
		m.days = 30
		return m.switchView(viewAgenda)
	case key.Matches(msg, keys.Forward):
		m.start = m.start.AddDate(0, 0, m.days)
		m.rebuild()
	case key.Matches(msg, keys.Back):
		m.start = m.start.AddDate(0, 0, -m.days)
		m.rebuild()
	case key.Matches(msg, keys.Today):
		m.start = m.today()
		m.rebuild()
		m.setStatus("已回到今天")

	case key.Matches(msg, keys.SyncMail):
		if m.mailS != nil && m.mailS.HasAccounts() {
			m.chatNote("正在同步新邮件…")
			return m, tea.Batch(m.spinner.Tick, m.syncMailCmd())
		}
		m.setStatus("未配置邮箱，用 /mail 添加")
	case key.Matches(msg, keys.LoadMoreMail):
		if m.mailS != nil && m.mailS.HasAccounts() {
			m.chatNote("正在往前加载更早的邮件…")
			return m, tea.Batch(m.spinner.Tick, m.loadMailCmd())
		}
		m.setStatus("未配置邮箱，用 /mail 添加")
	case key.Matches(msg, keys.AutoLevel):
		if m.agent != nil {
			next := agent.AutoLevel((int(m.agent.Auto) + 1) % 3)
			m.agent.SetAuto(next)
			m.setStatus("已切换到「%s」档", next.Label())
		}

	case key.Matches(msg, keys.Cycle):
		m.cycleState()
	case key.Matches(msg, keys.State):
		return m.startPrompt(promptState, "改为状态", m.stateHint())
	case key.Matches(msg, keys.Sched):
		return m.startPrompt(promptSched, "计划时间", "如 2006-01-02 09:00-10:30，留空清除")
	case key.Matches(msg, keys.Dead):
		return m.startPrompt(promptDead, "截止时间", "如 2006-01-02，留空清除")
	case key.Matches(msg, keys.Prio):
		return m.startPrompt(promptPrio, "优先级", "A / B / C，留空清除")
	case key.Matches(msg, keys.Repeat):
		return m.startPrompt(promptRepeat, "循环规则", m.repeatHint())
	case key.Matches(msg, keys.Capture):
		return m.startPrompt(promptCapture, "新建条目", "标题 [@ 计划时间]")
	case key.Matches(msg, keys.Search):
		m.view = viewTodos
		m.rebuild()
		return m.startPrompt(promptSearch, "搜索", "输入关键词")

	case key.Matches(msg, keys.ClockIn):
		m.clock(true)
	case key.Matches(msg, keys.ClockOut):
		m.clock(false)
	case key.Matches(msg, keys.LogMode):
		m.showLog = !m.showLog
		m.rebuild()
		if m.showLog {
			m.setStatus("已显示当天完成的条目")
		} else {
			m.setStatus("已隐藏当天完成的条目")
		}
	case key.Matches(msg, keys.Detail):
		if m.view == viewChat {
			return m.enterChat()
		}
		if m.view == viewMail {
			return m.openMailDetail()
		}
		m.openDetail()
	case key.Matches(msg, keys.Delete):
		return m.confirmDelete()
	case key.Matches(msg, keys.Edit):
		return m.editCurrent()
	case key.Matches(msg, keys.Refresh):
		m.reload()
	case key.Matches(msg, keys.Undo):
		m.undoAgent()
	}
	return m, nil
}

func (m *Model) stateHint() string {
	if it := m.current(); it != nil {
		return strings.Join(it.Doc.AllKeywords(), " / ")
	}
	return ""
}

// startPrompt 激活底部输入框。
func (m *Model) startPrompt(kind prompt, label, placeholder string) (tea.Model, tea.Cmd) {
	if kind != promptSearch && kind != promptCapture && kind != promptChat &&
		kind != promptLogin && kind != promptMail && m.current() == nil {
		m.setError(fmt.Errorf("请先选中一个条目"))
		return m, nil
	}
	m.prompt = kind
	m.input.Placeholder = placeholder
	m.input.EchoMode = textinput.EchoNormal
	if kind == promptLogin {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.SetValue("")
	if kind == promptSearch {
		m.input.SetValue(m.query)
	}
	m.input.CursorEnd()
	m.status = label
	m.statusErr = false
	return m, m.input.Focus()
}

// withCurrent 对当前选中条目执行一次修改并落盘。
func (m *Model) withCurrent(fn func(doc *org.Document, n *org.Node) (*org.Node, error)) {
	m.status, m.statusErr = "", false
	doc, node := m.currentNode()
	if node == nil {
		m.setError(fmt.Errorf("请先选中一个条目"))
		return
	}
	line := node.Line
	if _, err := fn(doc, node); err != nil {
		m.setError(err)
		return
	}
	if err := doc.Save(); err != nil {
		m.setError(err)
		return
	}
	if m.detail != nil {
		if moved := doc.NodeAt(line); moved != nil {
			m.detail.line = moved.Line
		}
	}
	m.rebuild()
}

func (m *Model) cycleState() {
	var title, from, to string
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title, from = n.Title, n.TODO
		next, err := doc.SetTodo(n, doc.NextTodo(n.TODO))
		if err == nil {
			to = next.TODO
		}
		return next, err
	})
	if title != "" {
		m.setStatus("「%s」%s → %s", title, orNone(from), orNone(to))
	}
}

func (m *Model) applyState(state string) {
	var title string
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title = n.Title
		return doc.SetTodo(n, state)
	})
	if !m.statusErr {
		m.setStatus("「%s」状态改为 %s", title, orNone(state))
	}
}

func (m *Model) applyPlanning(key, value string) {
	var ts *org.Timestamp
	if value != "" {
		var err error
		if ts, err = org.ParseUserTime(value, m.nowTime()); err != nil {
			m.setError(err)
			return
		}
	}
	label := "计划"
	if key == "DEADLINE" {
		label = "截止"
	}
	var title string
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title = n.Title
		old := n.Scheduled
		if key == "DEADLINE" {
			old = n.Deadline
		}
		if ts != nil && old != nil {
			ts.Repeat, ts.Warn = old.Repeat, old.Warn
		}
		return doc.SetPlanning(n, key, ts), nil
	})
	if m.statusErr {
		return
	}
	if ts == nil {
		m.setStatus("已清除「%s」的%s时间", title, label)
	} else {
		m.setStatus("「%s」%s %s", title, label, ts.String())
	}
}

func (m *Model) repeatHint() string {
	it := m.current()
	if it == nil {
		return "如 +1w、每周、.+3d"
	}
	if r := it.Node.Repeater(); r != nil {
		return fmt.Sprintf("当前 %s（%s），留空取消循环", r.String(), r.Describe())
	}
	if it.Node.RepeatTarget() == "" {
		return "得先用 s 或 D 给它一个时间"
	}
	return "如 +1w、每周、每天、.+3d（完成后隔三天）"
}

func (m *Model) applyRepeat(value string) {
	r, err := org.ParseRepeat(value)
	if err != nil {
		m.setError(err)
		return
	}
	var title, target string
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title, target = n.Title, n.RepeatTarget()
		return doc.SetRepeat(n, "", r)
	})
	if m.statusErr {
		return
	}
	label := map[string]string{"SCHEDULED": "计划", "DEADLINE": "截止"}[target]
	if r == nil {
		m.setStatus("「%s」不再循环", title)
	} else {
		m.setStatus("「%s」%s时间将%s重复（%s）", title, label, r.Describe(), r.String())
	}
}

func (m *Model) applyPriority(prio string) {
	if prio != "" && (len(prio) != 1 || prio[0] < 'A' || prio[0] > 'Z') {
		m.setError(fmt.Errorf("优先级应为单个字母，如 A"))
		return
	}
	var title string
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title = n.Title
		n.Priority = prio
		doc.Lines[n.Line] = n.String()
		doc.Reparse()
		return doc.NodeAt(n.Line), nil
	})
	if !m.statusErr {
		m.setStatus("「%s」优先级 %s", title, orNone(prio))
	}
}

func (m *Model) applyCapture(value string) {
	if value == "" {
		return
	}
	spec := org.NewNodeSpec{TODO: "TODO", Title: value}
	if at := strings.LastIndex(value, "@"); at > 0 {
		title := strings.TrimSpace(value[:at])
		ts, err := org.ParseUserTime(strings.TrimSpace(value[at+1:]), m.nowTime())
		if err != nil {
			m.setError(err)
			return
		}
		spec.Title, spec.Scheduled = title, ts
	}

	doc := m.coll.Doc(m.cfg.InboxFile())
	if doc == nil {
		if len(m.coll.Docs) == 0 {
			m.setError(fmt.Errorf("没有可写入的 org 文件"))
			return
		}
		doc = m.coll.Docs[0]
	}
	doc.AddNode(spec)
	if err := doc.Save(); err != nil {
		m.setError(err)
		return
	}
	m.rebuild()
	m.setStatus("已在 %s 新建「%s」", doc.Category(), spec.Title)
}

func (m *Model) clock(in bool) {
	var title string
	var total time.Duration
	m.withCurrent(func(doc *org.Document, n *org.Node) (*org.Node, error) {
		title = n.Title
		if in {
			return doc.ClockIn(n, m.nowTime())
		}
		out, err := doc.ClockOut(n, m.nowTime())
		if err == nil {
			total = out.ClockedTotal()
		}
		return out, err
	})
	if m.statusErr {
		return
	}
	if in {
		m.setStatus("开始为「%s」计时", title)
	} else {
		m.setStatus("「%s」计时结束，累计 %s", title, humanDuration(total))
	}
}

func (m *Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prompt != promptNone {
		return m.updatePrompt(msg)
	}
	switch {
	case key.Matches(msg, keys.Cancel), key.Matches(msg, keys.Detail), key.Matches(msg, keys.Quit):
		m.closeDetail()
		return m, nil
	case key.Matches(msg, keys.Toggle):
		m.toggleBox()
		return m, nil
	case key.Matches(msg, keys.Down):
		m.moveBox(1)
		return m, nil
	case key.Matches(msg, keys.Up):
		m.moveBox(-1)
		return m, nil
	case key.Matches(msg, keys.PgDown):
		m.scrollDetail(5)
		return m, nil
	case key.Matches(msg, keys.PageUp):
		m.scrollDetail(-5)
		return m, nil
	}
	return m.updateNormal(msg)
}

func (m *Model) confirmDelete() (tea.Model, tea.Cmd) {
	doc, n := m.currentNode()
	if n == nil {
		m.setError(fmt.Errorf("请先选中一个条目"))
		return m, nil
	}
	title := org.Plain(n.Title)
	question := fmt.Sprintf("删除「%s」？", title)
	if kids := n.DescendantCount(); kids > 0 {
		question = fmt.Sprintf("删除「%s」及其 %d 个子条目？", title, kids)
	}
	line := n.Line
	return m.askConfirm(question, func() {
		target := doc.NodeAt(line)
		if target == nil {
			m.setError(fmt.Errorf("条目已不在了"))
			return
		}
		m.orgWS.Snapshot(doc)
		doc.DeleteNode(target)
		if err := doc.Save(); err != nil {
			m.setError(err)
			return
		}
		m.orgWS.Record(doc, "删除「%s」", title)
		m.closeDetail()
		m.rebuild()
		m.setStatus("已删除「%s」，按 u 可恢复", title)
	})
}

func (m *Model) undoAgent() {
	if !m.orgWS.HasChanges() {
		m.setStatus("没有可撤销的改动")
		return
	}
	n := len(m.orgWS.Changes)
	if err := m.orgWS.Undo(); err != nil {
		m.setError(err)
		return
	}
	m.closeDetail()
	m.rebuild()
	m.setStatus("已撤销 %d 处改动", n)
}

func (m *Model) enterChat() (tea.Model, tea.Cmd) {
	return m.switchView(viewChat)
}

func (m *Model) openMailDetail() (tea.Model, tea.Cmd) {
	it := m.currentMail()
	if it == nil {
		return m, nil
	}
	m.chat = append(m.chat, chatEntry{role: "note", text: fmt.Sprintf("邮件详情 [%d]", it.No)})
	m.chat = append(m.chat, chatEntry{role: "note", text: fmt.Sprintf("发件人: %s <%s>", it.Msg.From(), it.Msg.FromAddr)})
	m.chat = append(m.chat, chatEntry{role: "note", text: fmt.Sprintf("主题: %s", it.Msg.Subject)})
	m.chat = append(m.chat, chatEntry{role: "note", text: fmt.Sprintf("时间: %s · 账号: %s", it.Msg.Date.Format("2006-01-02 15:04"), it.Msg.Account)})
	if it.Verdict != "" {
		m.chat = append(m.chat, chatEntry{role: "note", text: fmt.Sprintf("分类: %s（%s）", it.Verdict.Label(), it.Reason)})
	}
	m.chat = append(m.chat, chatEntry{role: "note", text: ""})
	if it.Msg.Snippet != "" {
		m.chat = append(m.chat, chatEntry{role: "agent", text: it.Msg.Snippet})
	} else {
		m.chat = append(m.chat, chatEntry{role: "note", text: "（正文未取到，可能是纯图片邮件）"})
	}
	// 切到秘书面板，把详情亮出来给用户看。
	return m.switchView(viewChat)
}

// askAgent 异步向秘书提问，进度通过 channel 回送。
func (m *Model) askAgent(text string) tea.Cmd {
	if m.agent == nil {
		m.setError(fmt.Errorf("秘书不可用：%v", m.agentErr))
		return nil
	}
	m.chat = append(m.chat, chatEntry{role: "user", text: text})
	m.busy = true

	events := make(chan agent.Event, 64)
	m.agentEvents = events
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	m.cancelAgent = cancel

	go func() {
		defer cancel()
		answer, err := m.agent.Ask(ctx, text, func(e agent.Event) { events <- e })
		// 最终答复也放进同一个 FIFO 事件流，保证在全部增量之后被处理。
		events <- agent.Event{Kind: agent.EventDone, Text: answer, Err: err}
		close(events)
	}()

	return tea.Batch(m.spinner.Tick, waitEvent(events))
}

// waitEvent 等待秘书的下一条进度；channel 关闭后返回 nil 结束轮询。
func waitEvent(ch chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		return agentEventMsg(e)
	}
}

func (m *Model) interruptAgent() {
	if !m.busy || m.cancelAgent == nil {
		return
	}
	m.cancelAgent()
	m.setStatus("正在打断…")
}

func (m *Model) appendAgentText(s string) {
	if n := len(m.chat); n > 0 && m.chat[n-1].streaming {
		m.chat[n-1].text += s
		return
	}
	m.chat = append(m.chat, chatEntry{role: "agent", text: s, streaming: true})
}

func (m *Model) endStreaming() string {
	n := len(m.chat)
	if n == 0 || !m.chat[n-1].streaming {
		return ""
	}
	m.chat[n-1].streaming = false
	return m.chat[n-1].text
}

func lastRunes(s string, n int) string {
	r := []rune(strings.ReplaceAll(strings.TrimSpace(s), "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[len(r)-n:])
}

// formatCall 把一次工具调用渲染成一行。
func formatCall(e agent.Event) string {
	if strings.TrimSpace(e.Text) == "" {
		return toolLabel(e.Tool)
	}
	return toolLabel(e.Tool) + "(" + e.Text + ")"
}

// toolLabel 给工具名配上中文说明。
func toolLabel(name string) string {
	switch name {
	case "list_agenda":
		return "查看日程"
	case "list_todos":
		return "查看待办"
	case "find_free_slots":
		return "寻找空档"
	case "get_item":
		return "查看条目"
	case "schedule_item":
		return "安排时间"
	case "set_deadline":
		return "设置截止"
	case "set_todo_state":
		return "修改状态"
	case "set_priority":
		return "设置优先级"
	case "create_item":
		return "新建条目"
	case "set_repeat":
		return "设置循环"
	case "delete_item":
		return "删除条目"
	case "list_mail":
		return "查看邮件"
	case "read_mail":
		return "读邮件"
	case "triage":
		return "整理邮件"
	case "refresh_mail":
		return "同步邮件"
	case "load_more_mail":
		return "加载更早邮件"
	case "mark_read":
		return "标记已读"
	case "clean_mail":
		return "清理邮件"
	case "draft_reply":
		return "起草回信"
	case "draft_new":
		return "起草邮件"
	case "send_draft":
		return "发送邮件"
	}
	return name
}

func orNone(s string) string {
	if s == "" {
		return "无状态"
	}
	return s
}

func humanDuration(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d小时%d分钟", h, m)
	case h > 0:
		return fmt.Sprintf("%d小时", h)
	default:
		return fmt.Sprintf("%d分钟", m)
	}
}

func humanSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d 小时前", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d 天前", int(d.Hours()/24))
	}
	return t.Format("01-02 15:04")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// mouseScroll 处理鼠标滚轮：详情里滚内容，聊天里滚历史，列表里移动光标。
func (m *Model) mouseScroll(delta int) {
	if m.detail != nil {
		m.scrollDetail(delta)
		return
	}
	if m.view == viewChat {
		m.chatOffset = maxInt(0, m.chatOffset+delta)
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	steps := delta
	if steps < 0 {
		steps = -steps
	}
	for i := 0; i < steps; i++ {
		m.move(dir)
	}
}

// wheelDelta 处理 macOS 的自然滚动：默认滚动方向与 Windows/Linux 相反，
// 这里把滚轮方向反一下，让双指上推往下滚、下拉往上滚。
func wheelDelta(d int) int {
	if runtime.GOOS == "darwin" {
		return -d
	}
	return d
}

// clickRow 处理鼠标左键点击：列表里选中对应行，邮件视图里直接打开详情。
func (m *Model) clickRow(y int) (tea.Model, tea.Cmd) {
	idx := m.offset + (y - 2) // 顶部标题栏占 2 行
	if idx < 0 || idx >= len(m.rows) {
		return m, nil
	}
	m.cursor = idx
	m.clampCursor()
	if m.view == viewMail && m.currentMail() != nil {
		return m.openMailDetail()
	}
	return m, nil
}

// toggleMouse 开关终端鼠标捕获：关掉后可以用鼠标直接选择文字。
func (m *Model) toggleMouse() (tea.Model, tea.Cmd) {
	m.mouseEnabled = !m.mouseEnabled
	if m.mouseEnabled {
		m.setStatus("鼠标捕获已开启（滚轮滚动），按住 Option 可选中文字")
		return m, func() tea.Msg { return tea.EnableMouseCellMotion() }
	}
	m.setStatus("鼠标捕获已关闭，现在可以用鼠标直接选择文字（F2 恢复滚动）")
	return m, func() tea.Msg { return tea.DisableMouse() }
}
