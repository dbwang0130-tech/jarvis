package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/dabao/jarvis/internal/org"
)

// detailState 是条目详情的浏览状态。节点用「文件 + 行号」定位，
// 这样文件被改写重解析之后仍然指得准。
type detailState struct {
	doc  *org.Document
	line int
	// box 是当前选中的复选框序号。
	box    int
	scroll int
}

// detailNode 返回详情正在展示的节点。
func (m *Model) detailNode() *org.Node {
	if m.detail == nil || m.detail.doc == nil {
		return nil
	}
	return m.detail.doc.NodeAt(m.detail.line)
}

// openDetail 打开当前选中条目的详情。
func (m *Model) openDetail() {
	it := m.current()
	if it == nil {
		m.setError(fmt.Errorf("请先选中一个条目"))
		return
	}
	m.detail = &detailState{doc: it.Doc, line: it.Node.Line}
}

// closeDetail 关闭详情。
func (m *Model) closeDetail() {
	m.detail = nil
}

// toggleBox 切换详情里选中的复选框。
func (m *Model) toggleBox() {
	n := m.detailNode()
	if n == nil {
		return
	}
	if len(n.Boxes) == 0 {
		m.setError(fmt.Errorf("这个条目没有复选框"))
		return
	}
	doc := m.detail.doc
	updated, err := doc.ToggleCheckbox(n, m.detail.box)
	if err != nil {
		m.setError(err)
		return
	}
	if err := doc.Save(); err != nil {
		m.setError(err)
		return
	}
	m.detail.line = updated.Line
	m.rebuild()
	done, total := updated.Progress()
	m.setStatus("「%s」进度 %d/%d", org.Plain(updated.Title), done, total)
}

// moveBox 在复选框之间移动选中位置；没有复选框时退化成滚动。
func (m *Model) moveBox(delta int) {
	n := m.detailNode()
	if n == nil || len(n.Boxes) == 0 {
		m.scrollDetail(delta)
		return
	}
	m.detail.box = (m.detail.box + delta + len(n.Boxes)) % len(n.Boxes)
}

// scrollDetail 滚动详情内容，用于看复选框以外的部分。
func (m *Model) scrollDetail(delta int) {
	if m.detail == nil {
		return
	}
	m.detail.scroll = maxInt(0, m.detail.scroll+delta)
	// 上界由渲染时按实际行数收敛，这里只防止负数。
}

// renderDetail 渲染条目详情，返回已按高度裁剪好的文本。
func (m *Model) renderDetail(height int) string {
	n := m.detailNode()
	if n == nil {
		return styDim.Render("  条目已不存在，按 esc 返回")
	}

	var lines []string
	// 记下每个复选框落在第几行，便于滚动时保证选中项可见。
	boxLines := map[int]int{}
	add := func(s string) { lines = append(lines, s) }

	add(styTitle.Render(org.Plain(n.Title)))
	if crumbs := n.Breadcrumb(); len(crumbs) > 1 {
		add(styDim.Render(strings.Join(plainAll(crumbs[:len(crumbs)-1]), " / ")))
	}

	var head []string
	if n.TODO != "" {
		head = append(head, keywordStyle(n.TODO, n.IsDone()).Render(n.TODO))
	}
	if n.Priority != "" {
		head = append(head, styPriority.Render("[#"+n.Priority+"]"))
	}
	if tags := n.AllTags(); len(tags) > 0 {
		head = append(head, styTag.Render(":"+strings.Join(tags, ":")+":"))
	}
	head = append(head, styDim.Render(n.Category()))
	add(strings.Join(head, " "))
	add("")

	field := func(label, value string) {
		if value != "" {
			add(styDim.Render(padTo(label, 6)) + value)
		}
	}
	if n.Scheduled != nil {
		field("计划", planningText(n.Scheduled))
	}
	if n.Deadline != nil {
		field("截止", planningText(n.Deadline))
	}
	if n.Closed != nil {
		field("完成", n.Closed.Start.Format("2006-01-02 15:04"))
	}
	if r := n.Repeater(); r != nil {
		field("循环", styRepeat.Render(r.Describe()+"（"+r.String()+"）"))
	}
	if total := n.ClockedTotal(); total > 0 {
		text := humanDuration(total)
		if c := n.RunningClock(); c != nil {
			text += styOK.Render("  ⏱ 计时中")
		}
		field("投入", text)
	}
	if done, all := n.Progress(); all > 0 {
		field("进度", fmt.Sprintf("%d/%d  %s", done, all, progressBar(done, all)))
	}
	for _, kv := range [][2]string{{"EFFORT", "预估"}, {"ID", "标识"}} {
		if v := n.Props[kv[0]]; v != "" {
			field(kv[1], v)
		}
	}

	// 正文：复选框行可以选中，代码块原样保留。
	body := detailBody(m.detail.doc, n)
	if len(body) > 0 {
		add("")
		boxIdx := 0
		for _, bl := range body {
			if bl.box < 0 {
				add(bl.text)
				continue
			}
			mark, style := "[ ]", styBase
			if bl.done {
				mark, style = "[X]", styDim
			}
			prefix := "  "
			if boxIdx == m.detail.box {
				prefix = styPrompt.Render("▸ ")
			}
			boxLines[boxIdx] = len(lines)
			add(prefix + styOK.Render(mark) + " " + style.Render(bl.text))
			boxIdx++
		}
	}

	if len(n.Clocks) > 0 || len(n.Logs) > 0 {
		add("")
		add(styDim.Render("记录"))
		for i := range n.Clocks {
			add("  " + clockLine(&n.Clocks[i]))
		}
		for _, e := range n.Logs {
			add("  " + logLine(e))
		}
	}

	if links := org.Links(strings.Join(append([]string{n.Title}, rawBody(m.detail.doc, n)...), "\n")); len(links) > 0 {
		add("")
		add(styDim.Render("链接"))
		for _, l := range links {
			add("  " + styAccent.Render(l.Text) + styDim.Render("  "+l.URL))
		}
	}

	if len(n.Children) > 0 {
		add("")
		add(styDim.Render("子条目"))
		for _, c := range n.Children {
			line := "  "
			if c.TODO != "" {
				line += keywordStyle(c.TODO, c.IsDone()).Render(c.TODO) + " "
			}
			line += org.Plain(c.Title)
			if d, a := c.Progress(); a > 0 {
				line += styDim.Render(fmt.Sprintf("  %d/%d", d, a))
			}
			add(line)
		}
	}

	// 让选中的复选框保持在可见范围内
	if want, ok := boxLines[m.detail.box]; ok {
		if want < m.detail.scroll {
			m.detail.scroll = want
		}
		if want >= m.detail.scroll+height {
			m.detail.scroll = want - height + 1
		}
	}
	if m.detail.scroll > len(lines)-height {
		m.detail.scroll = len(lines) - height
	}
	if m.detail.scroll < 0 {
		m.detail.scroll = 0
	}

	end := minInt(len(lines), m.detail.scroll+height)
	view := lines[m.detail.scroll:end]
	out := make([]string, 0, height)
	for _, l := range view {
		out = append(out, lipgloss.NewStyle().MaxWidth(m.width).Render(l))
	}
	for len(out) < height {
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// planningText 把计划或截止时间渲染成中文可读的形式。
func planningText(ts *org.Timestamp) string {
	text := org.FormatDate(ts.Start)
	if ts.HasTime {
		text += " " + ts.Start.Format("15:04")
		if ts.HasEnd() {
			text += "-" + ts.End.Format("15:04")
		}
	}
	if ts.Warn != nil {
		text += styDim.Render(fmt.Sprintf("  提前 %s 提醒", ts.Warn.String()))
	}
	return text
}

func progressBar(done, total int) string {
	const width = 12
	if total <= 0 {
		return ""
	}
	filled := done * width / total
	return styOK.Render(strings.Repeat("█", filled)) +
		styDim.Render(strings.Repeat("░", width-filled))
}

// bodyLine 是详情正文里的一行，box >= 0 表示这是第几个复选框。
type bodyLine struct {
	text string
	box  int
	done bool
}

// detailBody 取出节点自身的正文，过滤掉 planning 行与抽屉，并标出复选框。
func detailBody(doc *org.Document, n *org.Node) []bodyLine {
	var out []bodyLine
	boxIdx := 0
	for _, raw := range rawBody(doc, n) {
		if i := boxIdx; i < len(n.Boxes) && n.Boxes[i].Text != "" &&
			strings.Contains(raw, n.Boxes[i].Text) && isCheckboxLine(raw) {
			out = append(out, bodyLine{text: org.Plain(n.Boxes[i].Text), box: i, done: n.Boxes[i].Done})
			boxIdx++
			continue
		}
		out = append(out, bodyLine{text: "  " + org.Plain(raw), box: -1})
	}
	// 去掉首尾空行
	for len(out) > 0 && strings.TrimSpace(out[0].text) == "" {
		out = out[1:]
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1].text) == "" {
		out = out[:len(out)-1]
	}
	return out
}

func isCheckboxLine(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "- [") || strings.HasPrefix(t, "+ [") || strings.HasPrefix(t, "* [")
}

// rawBody 返回节点自身正文的原始行（不含 planning 行、抽屉和子标题）。
func rawBody(doc *org.Document, n *org.Node) []string {
	end := n.End
	if len(n.Children) > 0 {
		end = n.Children[0].Line
	}
	var out []string
	inDrawer := false
	for i := n.Line + 1; i < end && i < len(doc.Lines); i++ {
		line := doc.Lines[i]
		trimmed := strings.TrimSpace(line)
		if inDrawer {
			if strings.EqualFold(trimmed, ":END:") {
				inDrawer = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") && strings.HasSuffix(trimmed, ":") && len(trimmed) > 2 {
			inDrawer = true
			continue
		}
		if strings.HasPrefix(trimmed, "SCHEDULED:") || strings.HasPrefix(trimmed, "DEADLINE:") ||
			strings.HasPrefix(trimmed, "CLOSED:") || strings.HasPrefix(trimmed, "CLOCK:") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func plainAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = org.Plain(s)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// clockLine 渲染一条计时记录。
func clockLine(c *org.Clock) string {
	if c.Start == nil {
		return ""
	}
	text := c.Start.Start.Format("01-02 15:04")
	if c.End != nil {
		text += "-" + c.End.Start.Format("15:04")
	} else {
		text += "-  ⏱ "
	}
	line := styDim.Render(padTo(text, 18)) + humanDuration(c.Duration())
	if c.Running() {
		return styOK.Render("⏱ ") + line + styOK.Render("  计时中")
	}
	return styDim.Render("· ") + line
}

// logLine 渲染一条 LOGBOOK 记录。
func logLine(e org.LogEntry) string {
	when := ""
	if e.Time != nil {
		when = e.Time.Start.Format("01-02 15:04")
	}
	switch e.Kind {
	case org.LogState:
		from := e.From
		if from == "" {
			from = "无状态"
		}
		return styDim.Render("· "+padTo(when, 18)) +
			keywordStyleFor(e.To) + styDim.Render(" ← "+from)
	case org.LogNote:
		return styDim.Render("· "+padTo(when, 18)) + styDim.Render("笔记")
	default:
		return styDim.Render("· " + padTo(when, 18) + truncateWidth(e.Text, 40))
	}
}

// keywordStyleFor 给日志里的状态名上色，完成态与未完成态区分开。
func keywordStyleFor(kw string) string {
	done := kw == "DONE" || kw == "CANCELLED"
	return keywordStyle(kw, done).Render(kw)
}
