package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/org"
)

func formatDate(t time.Time) string { return org.FormatDate(t) }

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// padTo 按显示宽度把字符串补齐到 n 列（中日文按两列计）。
func padTo(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

// padLeft 按显示宽度把字符串在左侧补齐到 n 列。
func padLeft(s string, n int) string {
	if w := lipgloss.Width(s); w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

// formatMoney 把金额格式成带千位分隔符、两位小数的字符串。
func formatMoney(f float64) string {
	neg := f < 0
	v := f
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%.2f", v)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	if len(intPart) > 3 {
		var b strings.Builder
		for i, r := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				b.WriteByte(',')
			}
			b.WriteRune(r)
		}
		intPart = b.String()
	}
	out := intPart
	if len(parts) > 1 {
		out += "." + parts[1]
	}
	if neg {
		out = "-" + out
	}
	return out
}

// money 返回带正负配色的金额。
func money(f float64) string {
	if f < 0 {
		return styFinanceNeg.Render(formatMoney(f))
	}
	return styFinancePos.Render(formatMoney(f))
}

// ledgerAccountRow 渲染账户一行：账户名左对齐，余额右对齐。
func ledgerAccountRow(name string, amount float64) string {
	return padTo(name, 34) + padLeft(money(amount), 16)
}

// ledgerTxRow 渲染一条流水：日期、摘要、借贷明细（金额按正负着色）。
func ledgerTxRow(tx ledger.Transaction) string {
	var posts []string
	for _, p := range tx.Postings {
		if p.HasAmount {
			posts = append(posts, styDim.Render(p.Account)+" "+money(p.Amount))
		} else {
			posts = append(posts, styDim.Render(p.Account))
		}
	}
	return padTo(styDim.Render(tx.Date.Format("01-02")), 6) +
		padTo(truncateWidth(tx.Payee, 26), 28) +
		strings.Join(posts, "  ")
}

// renderItem 把一条记录渲染成列表中的一行。
func renderItem(it agenda.Item, v view) string {
	var b strings.Builder

	// 类别列
	b.WriteString(styDim.Render(padTo(truncateWidth(it.Category(), 10)+":", 11)))

	// 时间 / 类型列
	slot := ""
	if it.HasTime {
		slot = it.Start.Format("15:04")
		if it.Time != nil && it.Time.HasEnd() && sameDay(it.Time.End, it.Start) {
			slot += "-" + it.Time.End.Format("15:04")
		}
	}
	switch it.Kind {
	case agenda.KindOverdue:
		slot = fmt.Sprintf("拖延%d天", -it.DaysLeft)
	case agenda.KindDeadline:
		switch {
		case it.DaysLeft > 0:
			slot = fmt.Sprintf("剩%d天", it.DaysLeft)
		case it.DaysLeft == 0:
			slot = "今天到期"
		default:
			slot = fmt.Sprintf("逾期%d天", -it.DaysLeft)
		}
	case agenda.KindTodo:
		if it.Time != nil {
			slot = it.At.Format("01-02")
		}
	}
	sty := styTime
	if it.Kind == agenda.KindOverdue || (it.Kind == agenda.KindDeadline && it.DaysLeft <= 0) {
		sty = styOverdue
	}
	b.WriteString(sty.Render(padTo(slot, 12)))

	// 状态与优先级
	if it.Node.TODO != "" {
		b.WriteString(keywordStyle(it.Node.TODO, it.Node.IsDone()).Render(it.Node.TODO) + " ")
	}
	if it.Node.Priority != "" {
		b.WriteString(styPriority.Render("[#"+it.Node.Priority+"]") + " ")
	}

	// 标题里的链接和强调标记按 org 的语义渲染成纯文本
	title := org.Plain(it.Node.Title)
	if it.Node.IsDone() {
		b.WriteString(styDim.Render(title))
	} else {
		b.WriteString(styBase.Render(title))
	}
	if done, total := it.Node.Progress(); total > 0 {
		style := styDim
		if done == total {
			style = styOK
		}
		b.WriteString(style.Render(fmt.Sprintf("  %d/%d", done, total)))
	}

	if r := it.Node.Repeater(); r != nil {
		b.WriteString(styRepeat.Render("  ↻" + r.Describe()))
	}
	if tags := it.Node.Tags; len(tags) > 0 {
		b.WriteString(styTag.Render("  :" + strings.Join(tags, ":") + ":"))
	}
	if c := it.Node.RunningClock(); c != nil {
		b.WriteString(styOK.Render("  ⏱ 计时中"))
	}
	return b.String()
}

func truncateWidth(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > n {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// renderMailItem 把一封邮件渲染成列表里的一行。
func renderMailItem(it *mail.Item, width int) string {
	var b strings.Builder
	b.WriteString(styDim.Render(padTo(fmt.Sprintf("[%d]", it.No), 6)))
	b.WriteString(styDim.Render(padTo(truncateWidth(it.Msg.Account, 8), 9)))
	b.WriteString(padTo(truncateWidth(it.Msg.From(), 18), 19))
	if it.Msg.Unread {
		b.WriteString(styAccent.Render("●"))
	} else {
		b.WriteString(" ")
	}
	b.WriteString(" ")
	b.WriteString(styBase.Render(it.Msg.Subject))
	if it.Verdict != "" {
		b.WriteString("  " + verdictStyle(it.Verdict).Render(it.Verdict.Label()))
	}
	b.WriteString("  " + styDim.Render(humanSince(it.Msg.Date)))
	return b.String()
}

func verdictStyle(v mail.Verdict) lipgloss.Style {
	switch v {
	case mail.VerdictImportant:
		return styWarn
	case mail.VerdictInterest:
		return styAccent
	default:
		return styDim
	}
}

// View 实现 tea.Model。
func (m *Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}

	header := m.renderHeader()
	footer := m.renderFooter()
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	if m.detail != nil {
		body = m.renderDetail(bodyHeight)
	} else if m.view == viewChat {
		brief := m.renderTodayBrief()
		// 终端太矮时把空间全留给对话。
		if len(brief) > 0 && bodyHeight > len(brief)+4 {
			body = strings.Join(brief, "\n") + "\n" + m.renderChat(bodyHeight-len(brief))
		} else {
			body = m.renderChat(bodyHeight)
		}
	} else {
		body = m.renderList(bodyHeight)
	}
	return header + "\n" + body + "\n" + footer
}

// renderHeader 是顶部常驻的标题栏：左边应用名，右边模型/自动档/上下文。
func (m *Model) renderHeader() string {
	left := styHeaderTitle.Render("jarvis")
	var right []string
	if m.agent != nil {
		right = append(right, m.agent.Client.Model)
		if _, ratio := m.agent.ContextUsage(); ratio > 0 {
			right = append(right, fmt.Sprintf("上下文 %d%%", int(ratio*100)))
		}
		right = append(right, m.agent.Auto.Label())
	} else {
		right = append(right, "未配置模型")
	}
	rightText := styDim.Render(strings.Join(right, " · "))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(rightText)
	if gap < 1 {
		gap = 1
	}
	top := left + strings.Repeat(" ", gap) + rightText
	return top + "\n" + styHeaderRule.Render(strings.Repeat("─", m.width))
}

// renderList 渲染日程与待办列表，并处理滚动。
func (m *Model) renderList(height int) string {
	if len(m.rows) == 0 {
		return styDim.Render("  没有内容。按 c 新建条目，按 a 让秘书帮忙安排。")
	}

	// 让选中行始终可见
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
	// 光标在第一个可选中行时，把视口顶到最上面，露出标题/分组头。
	if first := firstSelectable(m.rows); first >= 0 && m.cursor == first {
		m.offset = 0
	}
	if m.offset > len(m.rows)-height {
		m.offset = len(m.rows) - height
	}
	if m.offset < 0 {
		m.offset = 0
	}

	var lines []string
	for i := m.offset; i < len(m.rows) && i < m.offset+height; i++ {
		r := m.rows[i]
		if !r.selectableRow() {
			head := styDayHeader
			if strings.Contains(r.text, "今天") {
				head = styTodayHeader
			}
			lines = append(lines, head.Render(r.text))
			continue
		}
		prefix := "  "
		text := r.text
		if i == m.cursor {
			prefix = styPrompt.Render("▸ ")
			text = stySelected.Render(padTo(text, m.width-2))
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(m.width).Render(prefix+text))
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderTodayBrief 是秘书面板顶部常驻的今日概览：
// 一行摘要加上几条最近的安排，让主界面一眼能看到今天的状况。
func (m *Model) renderTodayBrief() []string {
	const maxItems = 5
	day := agenda.Build(m.coll, m.today(), 1, m.agendaOpts()).Days[0]

	var scheduled, dueToday, late int
	for _, it := range day.Items {
		switch {
		case it.Kind == agenda.KindScheduled:
			scheduled++
		case it.Kind == agenda.KindDeadline && it.DaysLeft == 0:
			dueToday++
		case it.Kind == agenda.KindDeadline && it.DaysLeft < 0, it.Kind == agenda.KindOverdue:
			late++
		}
	}

	summary := []string{fmt.Sprintf("今天 %s", formatDate(m.today()))}
	if scheduled > 0 {
		summary = append(summary, fmt.Sprintf("%d 件已排期", scheduled))
	}
	if dueToday > 0 {
		summary = append(summary, fmt.Sprintf("%d 项今天到期", dueToday))
	}
	if late > 0 {
		summary = append(summary, fmt.Sprintf("%d 项已逾期", late))
	}
	if len(summary) == 1 {
		summary = append(summary, "今天没有安排")
	}

	lines := []string{styDayHeader.Render(strings.Join(summary, " · "))}
	shown := 0
	for _, it := range day.Items {
		if it.Kind == agenda.KindClosed || shown >= maxItems {
			continue
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(m.width).Render("  "+renderItem(it, viewChat)))
		shown++
	}
	if rest := countOpen(day.Items) - shown; rest > 0 {
		lines = append(lines, styDim.Render(fmt.Sprintf("  还有 %d 条，按 2 看完整日程", rest)))
	}
	return append(lines, styDim.Render(strings.Repeat("─", m.width)))
}

// countOpen 统计概览里会展示的条目数（不含已完成）。
func countOpen(items []agenda.Item) int {
	n := 0
	for _, it := range items {
		if it.Kind != agenda.KindClosed {
			n++
		}
	}
	return n
}

// renderChat 渲染秘书对话面板。
func (m *Model) renderChat(height int) string {
	var lines []string
	if len(m.chat) == 0 {
		hint := []string{
			"",
			"直接说你想安排什么，它会读你的 org 文件并动手改：",
			"  把这周没排时间的任务安排一下",
			"  明天下午找两个小时写季度汇报",
			"  体检预约改到下周五",
			"",
			"改完不满意按 u 一次撤回。/help 看全部命令。",
		}
		if m.agent == nil {
			hint = []string{
				"",
				styError.Render("秘书还没配置：" + fmt.Sprint(m.agentErr)),
				"",
				"直接输入 /login sk-... 就能用（也可以只敲 /login 再粘贴密钥）。",
				"换服务商用 /provider，可选：" + strings.Join(sortedPresets(), "、"),
			}
		}
		for _, h := range hint {
			lines = append(lines, styDim.Render(h))
		}
	}

	for _, e := range m.chat {
		switch e.role {
		case "user":
			lines = append(lines, styUserMark.Render("❯ ")+styUserText.Render(e.text))
		case "agent":
			// 模型爱用 markdown，简单渲染一下再显示。
			lines = append(lines, renderMarkdown(e.text)...)
		case "toolcall":
			lines = append(lines, styToolCall.Render("⏺ ")+styToolLog.Render(e.text))
		case "toolresult":
			lines = append(lines, styBranch.Render("  ⎿  ")+styToolLog.Render(e.text))
		case "toolerror":
			lines = append(lines, styBranch.Render("  ⎿  ")+styError.Render(e.text))
		case "note":
			for i, l := range strings.Split(e.text, "\n") {
				if i == 0 {
					lines = append(lines, styDim.Render("✻ "+l))
					continue
				}
				lines = append(lines, "  "+styDim.Render(l))
			}
		case "error":
			lines = append(lines, styError.Render("✻ "+e.text))
		case "confirm":
			for i, l := range strings.Split(e.text, "\n") {
				if i == 0 {
					lines = append(lines, styWarn.Render("⚠ "+l))
					continue
				}
				lines = append(lines, "  "+styWarn.Render(l))
			}
		}
	}
	if m.busy {
		lines = append(lines, styDim.Render(m.spinner.View()+" 处理中…  esc 打断"))
	}
	if m.verifying {
		lines = append(lines, styDim.Render(m.spinner.View()+" 正在校验密钥…"))
	}

	// 只显示最近的内容；chatOffset 表示用鼠标滚轮往上翻了多远的对话历史。
	wrapped := wrapLines(lines, m.width-1)
	if len(wrapped) > height {
		maxScroll := len(wrapped) - height
		if m.chatOffset > maxScroll {
			m.chatOffset = maxScroll
		}
		start := len(wrapped) - height - m.chatOffset
		if start < 0 {
			start = 0
		}
		wrapped = wrapped[start : start+height]
	} else {
		m.chatOffset = 0
	}
	for len(wrapped) < height {
		wrapped = append(wrapped, "")
	}
	return strings.Join(wrapped, "\n")
}

// wrapLines 按终端宽度折行。
func wrapLines(lines []string, width int) []string {
	if width < 10 {
		width = 10
	}
	style := lipgloss.NewStyle().Width(width)
	var out []string
	for _, l := range lines {
		if lipgloss.Width(l) <= width {
			out = append(out, l)
			continue
		}
		out = append(out, strings.Split(style.Render(l), "\n")...)
	}
	return out
}

func (m *Model) renderFooter() string {
	var lines []string

	switch {
	case m.prompt == promptConfirm:
		lines = append(lines, m.inputBox(styPrompt.Render(m.confirm.question+" (y/n)")))
	case m.prompt == promptChat:
		lines = append(lines, m.inputBox(styInputMark.Render("❯ ")+m.input.View()))
		if m.status != "" {
			lines = append(lines, "  "+m.statusLine())
		}
	case m.prompt != promptNone:
		lines = append(lines, m.inputBox(styPrompt.Render(m.status+" ")+
			styInputMark.Render("> ")+m.input.View()))
	default:
		if line := m.statusLine(); line != "" {
			lines = append(lines, styDim.Render(strings.Repeat("─", m.width))+"\n"+line)
		} else {
			lines = append(lines, styDim.Render(strings.Repeat("─", m.width)))
		}
	}

	hints := "j/k 移动 · enter 详情 · t 状态 · s 计划 · c 新建 · X 删除 · a 回秘书 · ? 帮助"
	switch {
	case m.agentPending != nil:
		hints = "y 确认 · 其他任意键取消"
	case m.prompt == promptConfirm:
		hints = "y 确认 · 其他任意键取消"
	case m.detail != nil:
		hints = "x 勾选 · ctrl+d/u 滚动 · t 状态 · s 计划 · R 循环 · X 删除 · esc 返回"
	case m.view != viewChat:
	case m.prompt == promptLogin:
		hints = "粘贴后回车保存 · esc 取消"
	case m.busy:
		hints = "esc 打断 · 生成中可以继续看日程（tab）"
	case m.chatBrowse:
		hints = "i 继续说话 · 2 日程 · 3 待办 · 4 邮件 · u 撤销改动 · ? 帮助 · q 退出"
	default:
		hints = "回车发送 · tab 切换 · /help 命令 · esc 松开输入框 · ctrl+c 退出"
	}
	if u := m.contextHint(); u != "" {
		hints += " · " + u
	}
	lines = append(lines, "  "+styDim.Render(truncateWidth(hints, m.width-2)))
	return strings.Join(lines, "\n")
}

// contextHint 在上下文用掉一半以上时提示占用比例，接近上限时变成警告。
func (m *Model) contextHint() string {
	if m.agent == nil || m.view != viewChat {
		return ""
	}
	_, ratio := m.agent.ContextUsage()
	if ratio < 0.5 {
		return ""
	}
	text := fmt.Sprintf("上下文 %d%%", int(ratio*100))
	if ratio >= 0.8 {
		return styWarn.Render(text + "，即将自动压缩")
	}
	return text
}

// inputBox 把内容裹进一个圆角框里，作为界面底部的输入区。
// 秘书忙着的时候边框转暗，提示这会儿轮不到你说话。
func (m *Model) inputBox(content string) string {
	width := m.width - 2
	if width < 10 {
		width = 10
	}
	style := styInputBox
	if m.busy {
		style = style.BorderForeground(colDim)
	}
	return style.Width(width - 2).Render(content)
}

// statusLine 渲染状态栏：没有消息时给出与当前视图相称的默认提示。
func (m *Model) statusLine() string {
	status := m.status
	if status == "" {
		if it := m.current(); it != nil {
			status = strings.Join(it.Node.Breadcrumb(), " / ")
		}
	}
	if status == "" {
		return ""
	}
	if m.statusErr {
		return styError.Render(truncateWidth(status, m.width))
	}
	return styStatus.Render(truncateWidth(status, m.width))
}

func (m *Model) renderHelp() string {
	var b strings.Builder
	b.WriteString(styTitle.Render("orgtui 快捷键") + "\n\n")
	for _, r := range helpRows {
		b.WriteString("  " + styPrompt.Render(padTo(r[0], 12)) + styBase.Render(r[1]) + "\n")
	}
	b.WriteString("\n" + styDim.Render("  时间格式：2006-01-02、2006-01-02 15:04、2006-01-02 15:04-16:30，也可以写「明天 09:00」"))
	b.WriteString("\n" + styDim.Render("  按任意键返回"))
	return b.String()
}
