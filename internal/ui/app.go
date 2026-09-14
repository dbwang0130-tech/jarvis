// Package ui 实现 jarvis 的终端界面。
package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

type view int

const (
	// viewChat 是主视图：启动即进入，输入框常驻聚焦。
	viewChat view = iota
	viewAgenda
	viewTodos
	viewMail
	viewLedger
)

// prompt 描述底部输入框当前在收集什么。
type prompt int

const (
	promptNone prompt = iota
	promptSched
	promptDead
	promptState
	promptPrio
	promptSearch
	promptCapture
	promptChat
	promptLogin
	promptRepeat
	promptConfirm
	promptMail
)

// row 是列表里的一行，item / mailItem 为 nil 且 selectable 为假时是不可选中的分组标题。
type row struct {
	text       string
	item       *agenda.Item
	mailItem   *mail.Item
	selectable bool
}

// chatEntry 是秘书面板里的一条记录。
type chatEntry struct {
	role string // user / agent / tool / note / error / confirm
	text string
	// streaming 为真表示这条还在逐字生成，后续增量会接在它后面。
	streaming bool
}

// mailWizard 是 /mail 命令的逐步收集状态。
type mailWizard struct {
	step int
	acc  config.MailAccount
}

// Model 是 Bubble Tea 的应用状态。
type Model struct {
	cfgPath string
	cfg     config.Config
	coll    *org.Collection
	orgWS   *orgtools.Workspace
	mailS   *mail.Session
	ledger  *ledger.Skill
	skills  []agent.Skill
	agent   *agent.Agent
	// agentErr 记录秘书不可用的原因（例如缺少 API key）。
	agentErr error

	view          view
	width, height int

	start time.Time
	days  int
	query string

	rows           []row
	cursor, offset int
	detail         *detailState

	chat        []chatEntry
	input       textinput.Model
	prompt      prompt
	spinner     spinner.Model
	busy        bool
	verifying   bool
	agentEvents chan agent.Event
	cancelAgent context.CancelFunc

	// agentPending 是秘书在等用户确认的危险动作。
	agentPending *agent.Pending
	// mailWizard 是 /mail 配置向导的当前状态。
	mailWizard *mailWizard

	chatBrowse bool
	showLog    bool
	quitting   bool
	confirm    *pendingAction

	// chatOffset 是对话从底部往上滚了多少行（鼠标滚轮滚动历史）。
	chatOffset int
	// mouseEnabled 表示终端鼠标捕获是否开启；关掉后可用鼠标直接选择文字。
	mouseEnabled bool

	status    string
	statusErr bool
	showHelp  bool

	now func() time.Time
}

// New 构造界面模型。
func New(cfgPath string, cfg config.Config, coll *org.Collection, orgWS *orgtools.Workspace,
	mailS *mail.Session, ledgerSkill *ledger.Skill, skills []agent.Skill, ag *agent.Agent, agentErr error) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 500

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &Model{
		cfgPath: cfgPath, cfg: cfg, coll: coll, orgWS: orgWS, mailS: mailS,
		ledger: ledgerSkill, skills: skills, agent: ag, agentErr: agentErr,
		days:  cfg.AgendaDays,
		input: in, spinner: sp,
		width: 80, height: 24,
		mouseEnabled: true,
	}
	m.start = m.today()
	m.rebuild()
	return m
}

// focusChat 聚焦秘书输入框。
func (m *Model) focusChat() tea.Cmd {
	m.chatBrowse = false
	m.prompt = promptChat
	m.input.EchoMode = textinput.EchoNormal
	m.input.SetValue("")
	m.input.Placeholder = chatPlaceholder(m.agent != nil)
	return m.input.Focus()
}

func chatPlaceholder(ready bool) string {
	if !ready {
		return "先敲 /login 配置密钥，/help 看命令"
	}
	return "说点什么：安排日程、整理邮件，或「看看我今天有什么」"
}

func (m *Model) nowTime() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *Model) today() time.Time {
	n := m.nowTime()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
}

// Init 实现 tea.Model。启动后聚焦输入框；邮件不自动拉取，需要时再同步。
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.focusChat())
}

func (m *Model) agendaOpts() agenda.Options {
	o := agenda.DefaultOptions()
	o.DeadlineWarnDays = m.cfg.DeadlineWarnDays
	o.ShowDone = m.showLog
	o.Now = m.nowTime()
	return o
}

// rebuild 按当前视图重新生成行列表，并尽量保持选中的条目不变。
func (m *Model) rebuild() {
	var keepFile, keepTitle string
	if it := m.current(); it != nil {
		keepFile, keepTitle = it.Doc.Path, it.Node.Title
	}

	m.rows = nil
	switch m.view {
	case viewAgenda:
		ag := agenda.Build(m.coll, m.start, m.days, m.agendaOpts())
		m.rows = append(m.rows, row{text: m.agendaRangeLabel()})
		for _, day := range ag.Days {
			head := formatDate(day.Date)
			if sameDay(day.Date, m.nowTime()) {
				head += "  ← 今天"
			}
			m.rows = append(m.rows, row{text: head})
			for i := range day.Items {
				it := day.Items[i]
				m.rows = append(m.rows, row{text: renderItem(it, m.view), item: &it})
			}
		}
	case viewTodos:
		items := agenda.Todos(m.coll, agenda.TodoFilter{Query: m.query})
		head := fmt.Sprintf("待办 %d 条", len(items))
		if m.query != "" {
			head += fmt.Sprintf("  过滤：%s", m.query)
		}
		m.rows = append(m.rows, row{text: head})
		for i := range items {
			it := items[i]
			m.rows = append(m.rows, row{text: renderItem(it, m.view), item: &it})
		}
	case viewMail:
		m.rebuildMail()
	case viewLedger:
		m.rebuildLedger()
	}

	if keepTitle != "" {
		for i, r := range m.rows {
			if r.item != nil && r.item.Node.Title == keepTitle && r.item.Doc.Path == keepFile {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
}

func (m *Model) rebuildMail() {
	if m.mailS == nil {
		m.rows = append(m.rows, row{text: "未配置邮箱，输入 /mail 添加账号"})
		return
	}
	items := m.mailS.Items()
	counts := m.mailS.Counts()
	head := fmt.Sprintf("邮件 %d 封", len(items))
	var parts []string
	for _, v := range []mail.Verdict{mail.VerdictImportant, mail.VerdictInterest, mail.VerdictPromo, mail.VerdictSpam} {
		if n := counts[v]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", v.Label(), n))
		}
	}
	if accts := m.cfg.Enabled(); len(accts) > 0 {
		names := make([]string, 0, len(accts))
		for _, a := range accts {
			names = append(names, a.Name)
		}
		parts = append(parts, "账号 "+strings.Join(names, "/"))
	}
	if len(parts) > 0 {
		head += "  " + strings.Join(parts, " · ")
	}
	m.rows = append(m.rows, row{text: head})
	for i := range items {
		it := items[i]
		m.rows = append(m.rows, row{text: renderMailItem(it, m.width), mailItem: it})
	}
	if len(items) == 0 {
		m.rows = append(m.rows, row{text: "尚未同步邮件，按 ctrl+r 同步，或回秘书让它刷新"})
	}
}

// rebuildLedger 生成财务页：净资产概览、分类账户表、最近流水。
func (m *Model) rebuildLedger() {
	if m.ledger == nil || m.ledger.Book == nil {
		m.rows = append(m.rows, row{text: "账本未初始化"})
		return
	}
	book := m.ledger.Book

	// 专业报表：资产负债表（余额）+ 损益表（本期发生额）。
	// ledger 里负债/收入为负数，这里统一转成报表读数（正数）。
	assets := book.SumByPrefix("Assets:")
	debt := -book.SumByPrefix("Liabilities:")
	netWorth := assets - debt
	income, expense := book.IncomeExpense(m.startOfMonth())
	surplus := income - expense

	m.rows = append(m.rows, row{text: styFinanceNet.Render(
		fmt.Sprintf("净资产 %s   资产 %s   负债 %s   本月结余 %s",
			money(netWorth), money(assets), money(debt), money(surplus)))})

	bal := book.Balances()
	addGroup := func(label, prefix string, sign float64) {
		names := accountsIn(book, prefix)
		if len(names) == 0 {
			return
		}
		sub := book.SumByPrefix(prefix) * sign
		m.rows = append(m.rows, row{text: styFinanceGroup.Render(
			padTo(label, 8) + padTo(truncateWidth(prefix, 16), 18) + padLeft(money(sub), 16))})
		for _, name := range names {
			m.rows = append(m.rows, row{
				text:       "  " + ledgerAccountRow(name, bal[name]*sign),
				selectable: true,
			})
		}
	}

	// 资产负债表：期末余额。
	m.rows = append(m.rows, row{text: styFinanceSection.Render("资产负债表")})
	addGroup("资产", "Assets:", 1)
	addGroup("负债", "Liabilities:", -1)
	m.rows = append(m.rows, row{text: styFinanceGroup.Render(
		padTo("净资产", 24) + padLeft(money(netWorth), 16))})

	// 损益表：本月发生额。
	m.rows = append(m.rows, row{text: styFinanceSection.Render("损益表（本月）")})
	addGroup("收入", "Income:", -1)
	addGroup("支出", "Expenses:", 1)
	m.rows = append(m.rows, row{text: styFinanceGroup.Render(
		padTo("本月结余", 24) + padLeft(money(surplus), 16))})

	// 未归类的账户（Equity 等）
	var other []string
	for name := range bal {
		if !strings.HasPrefix(name, "Assets:") && !strings.HasPrefix(name, "Liabilities:") &&
			!strings.HasPrefix(name, "Income:") && !strings.HasPrefix(name, "Expenses:") {
			other = append(other, name)
		}
	}
	if len(other) > 0 {
		m.rows = append(m.rows, row{text: styFinanceGroup.Render("其他")})
		for _, name := range other {
			m.rows = append(m.rows, row{text: "  " + ledgerAccountRow(name, bal[name]), selectable: true})
		}
	}

	// 最近流水
	m.rows = append(m.rows, row{text: styFinanceSection.Render("最近流水")})
	txs := book.Transactions
	const maxTx = 100
	start := 0
	if len(txs) > maxTx {
		start = len(txs) - maxTx
	}
	for i := len(txs) - 1; i >= start; i-- {
		m.rows = append(m.rows, row{text: ledgerTxRow(txs[i]), selectable: true})
	}
	if len(txs) == 0 {
		m.rows = append(m.rows, row{text: styDim.Render("  还没有交易。回秘书说「记一笔：午餐 50」即可。")})
	}
}

func (m *Model) startOfMonth() time.Time {
	n := m.nowTime()
	return time.Date(n.Year(), n.Month(), 1, 0, 0, 0, 0, n.Location())
}

func accountsIn(book *ledger.Book, prefix string) []string {
	var out []string
	for _, name := range book.SortedAccounts() {
		if strings.HasPrefix(name, prefix) {
			out = append(out, name)
		}
	}
	return out
}

// agendaRangeLabel 描述日程视图当前的时间跨度。
func (m *Model) agendaRangeLabel() string {
	span := map[int]string{1: "日视图", 7: "周视图", 30: "月视图"}[m.days]
	if span == "" {
		span = fmt.Sprintf("%d 天", m.days)
	}
	end := m.start.AddDate(0, 0, m.days-1)
	if m.days == 1 {
		return fmt.Sprintf("%s  %s", span, m.start.Format("2006-01-02"))
	}
	return fmt.Sprintf("%s  %s → %s", span, m.start.Format("2006-01-02"), end.Format("01-02"))
}

// current 返回当前选中的 agenda 记录。
func (m *Model) current() *agenda.Item {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].item
	}
	return nil
}

// currentMail 返回当前选中的邮件。
func (m *Model) currentMail() *mail.Item {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].mailItem
	}
	return nil
}

// currentNode 返回当前操作的对象：详情里的条目或列表选中项。
func (m *Model) currentNode() (*org.Document, *org.Node) {
	if m.detail != nil {
		if n := m.detailNode(); n != nil {
			return m.detail.doc, n
		}
	}
	if it := m.current(); it != nil {
		return it.Doc, it.Node
	}
	return nil, nil
}

// selectableRow 报告这一行能不能被光标选中。
func (r row) selectableRow() bool {
	return r.item != nil || r.mailItem != nil || r.selectable
}

func (m *Model) clampCursor() {
	if len(m.rows) == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if !m.rows[m.cursor].selectableRow() {
		if !m.move(1) {
			m.move(-1)
		}
	}
}

// move 把光标沿 dir 方向移到下一个可选中行。
func (m *Model) move(dir int) bool {
	for i := m.cursor + dir; i >= 0 && i < len(m.rows); i += dir {
		if m.rows[i].selectableRow() {
			m.cursor = i
			return true
		}
	}
	return false
}

// firstSelectable 返回第一个可选中行的下标，没有则返回 -1。
func firstSelectable(rows []row) int {
	for i, r := range rows {
		if r.selectableRow() {
			return i
		}
	}
	return -1
}

func (m *Model) setStatus(format string, args ...any) {
	m.status, m.statusErr = fmt.Sprintf(format, args...), false
}

func (m *Model) setError(err error) {
	if err == nil {
		return
	}
	m.status, m.statusErr = err.Error(), true
}

// reload 重新读取全部 org 文件。
func (m *Model) reload() {
	if err := m.coll.Reload(); err != nil {
		m.setError(err)
		return
	}
	m.coll.SetLogStates(m.cfg.LogStateChanges)
	m.rebuild()
	m.setStatus("已重新加载 %d 个文件", len(m.coll.Docs))
}

// reloadConfig 从磁盘重新读取配置，并热应用到运行中的界面与各项技能。
// 秘书通过 set_config 改完配置后，界面收到工具结果即调用这里。
func (m *Model) reloadConfig() {
	cfg, err := config.Load(m.cfgPath)
	if err != nil {
		m.setError(err)
		return
	}
	m.cfg = cfg

	// 日程：org 文件集合与工作区
	if m.orgWS != nil {
		if coll, lerr := LoadOrg(cfg); lerr == nil {
			m.coll = coll
			m.coll.SetLogStates(cfg.LogStateChanges)
			m.orgWS.Coll = coll
			m.orgWS.Cfg = cfg
		}
	}

	// 邮件：刷新会话持有的配置
	if m.mailS != nil {
		m.mailS.SetConfig(cfg)
	}

	// 记账：重建技能
	m.ledger = ledger.NewSkill(cfg.LedgerFile)

	// 重建技能与秘书，并同步用户自述与自动档
	m.skills = m.buildSkills()
	if m.agent != nil {
		m.agent.SetSkills(m.skills)
		m.agent.Profile = cfg.Profile.Interests
		m.agent.Auto = agent.ParseAutoLevel(cfg.Behavior.AutoConfirm)
	}

	m.rebuild()
	m.setStatus("配置已热更新")
}

// SetClock 注入时钟，用于测试中固定"今天"。
func (m *Model) SetClock(now func() time.Time) {
	m.now = now
	m.start = m.today()
	m.rebuild()
}
