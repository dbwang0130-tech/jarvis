package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/org"
	"github.com/mattn/go-runewidth"
)

// view 是底部状态栏上显示的当前视图。视图只影响「看什么」，
// 任何时候打字都还是在跟秘书说话。
type view int

const (
	viewChat view = iota
	viewAgenda
	viewTodos
	viewMail
	viewLedger
)

var viewNames = []string{"Jarvis", "日程", "待办", "邮件", "财务"}

func (v view) String() string {
	if int(v) < len(viewNames) {
		return viewNames[v]
	}
	return "Jarvis"
}

// next 返回 tab 轮换到的下一个视图。
func (v view) next() view { return view((int(v) + 1) % len(viewNames)) }

// prev 返回 shift+tab 轮换到的上一个视图。
func (v view) prev() view {
	return view((int(v) + len(viewNames) - 1) % len(viewNames))
}

// viewLines 渲染出某个视图当前该显示的内容。秘书视图返回 nil——
// 那一路显示的是对话历史，由 screen 自己管。
func (s *Session) viewLines(v view) []string {
	var body string
	switch v {
	case viewAgenda:
		body = s.renderAgenda()
	case viewTodos:
		body = s.renderTodos()
	case viewMail:
		body = s.renderMail()
	case viewLedger:
		body = s.renderLedger()
	default:
		return nil
	}
	w := width() - 1
	outs := []string{sectionTitle(v.String()), ""}
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		outs = append(outs, wrapANSI(l, w)...)
	}
	return outs
}

// sectionTitle 是视图快照的标题行。
func sectionTitle(name string) string {
	w := width() - 2
	if w > 76 {
		w = 76
	}
	line := strings.Repeat("─", max(0, w-runewidth.StringWidth(name)-3))
	return cyan + "── " + bold + name + reset + " " + cyan + line + reset
}

func (s *Session) agendaOpts() agenda.Options {
	o := agenda.DefaultOptions()
	if s.cfg.DeadlineWarnDays > 0 {
		o.DeadlineWarnDays = s.cfg.DeadlineWarnDays
	}
	return o
}

// renderAgenda 画未来几天的日程。
func (s *Session) renderAgenda() string {
	days := s.cfg.AgendaDays
	if days < 1 {
		days = 7
	}
	ag := agenda.Build(s.coll, time.Now(), days, s.agendaOpts())

	var all []agenda.Item
	for _, d := range ag.Days {
		all = append(all, d.Items...)
	}
	showCat := manyCategories(all)

	var b strings.Builder
	for _, d := range ag.Days {
		if len(d.Items) == 0 && !d.IsToday() {
			continue
		}
		head := d.Date.Format("01-02") + " " + weekday(d.Date)
		if d.IsToday() {
			b.WriteString(bold + green + head + " 今天" + reset + "\n")
		} else {
			b.WriteString(gray + head + reset + "\n")
		}
		if len(d.Items) == 0 {
			b.WriteString(dim + "  （空）" + reset + "\n")
			continue
		}
		for _, it := range d.Items {
			b.WriteString("  " + itemLine(it, showCat) + "\n")
		}
	}
	if b.Len() == 0 {
		return dim + "  未来几天没有安排。" + reset + "\n"
	}
	return b.String()
}

// renderTodos 画全部未完成条目。
func (s *Session) renderTodos() string {
	items := agenda.Todos(s.coll, agenda.TodoFilter{})
	if len(items) == 0 {
		return dim + "  没有待办。" + reset + "\n"
	}
	showCat := manyCategories(items)
	var b strings.Builder
	for i, it := range items {
		if i >= 30 {
			b.WriteString(dim + fmt.Sprintf("  …… 还有 %d 条", len(items)-30) + reset + "\n")
			break
		}
		b.WriteString("  " + itemLine(it, showCat) + "\n")
	}
	return b.String()
}

// manyCategories 报告这批条目是不是来自多个文件。只有一个的时候
// 类别列每行都一样，纯属占地方。
func manyCategories(items []agenda.Item) bool {
	if len(items) < 2 {
		return false
	}
	first := items[0].Category()
	for _, it := range items[1:] {
		if it.Category() != first {
			return true
		}
	}
	return false
}

// itemLine 把一条 agenda 记录压成一行：类别 / 时间 / 状态 / 标题 / 标签。
func itemLine(it agenda.Item, showCat bool) string {
	var b strings.Builder
	if showCat {
		b.WriteString(dim + pad(runewidth.Truncate(it.Category(), 10, "…")+":", 11) + reset)
	}

	slot := ""
	if it.HasTime {
		slot = it.Start.Format("15:04")
		if it.Time != nil && it.Time.HasEnd() {
			slot += "-" + it.Time.End.Format("15:04")
		}
	}
	late := false
	switch it.Kind {
	case agenda.KindOverdue:
		slot, late = fmt.Sprintf("拖延%d天", -it.DaysLeft), true
	case agenda.KindDeadline:
		switch {
		case it.DaysLeft > 0:
			slot = fmt.Sprintf("剩%d天", it.DaysLeft)
		case it.DaysLeft == 0:
			slot, late = "今天到期", true
		default:
			slot, late = fmt.Sprintf("逾期%d天", -it.DaysLeft), true
		}
	}
	if late {
		b.WriteString(red + pad(slot, 12) + reset)
	} else {
		b.WriteString(cyan + pad(slot, 12) + reset)
	}

	if it.Node.TODO != "" {
		c := warn
		if it.Node.IsDone() {
			c = gray
		}
		b.WriteString(c + it.Node.TODO + reset + " ")
	}
	if it.Node.Priority != "" {
		b.WriteString(coral + "[#" + it.Node.Priority + "]" + reset + " ")
	}

	// 子任务前面带上父任务的路径，不然光看标题不知道它挂在哪儿。
	if crumbs := it.Node.Breadcrumb(); len(crumbs) > 1 {
		parts := make([]string, 0, len(crumbs)-1)
		for _, p := range crumbs[:len(crumbs)-1] {
			parts = append(parts, org.Plain(p))
		}
		b.WriteString(dim + runewidth.Truncate(strings.Join(parts, " / "), 30, "…") + " / " + reset)
	}

	title := org.Plain(it.Node.Title)
	if it.Node.IsDone() {
		b.WriteString(dim + title + reset)
	} else {
		b.WriteString(title)
	}
	// 有勾选子项的，把完成进度跟在后面。
	if done, total := it.Node.Progress(); total > 0 {
		c := dim
		if done == total {
			c = green
		}
		b.WriteString(c + fmt.Sprintf("  %d/%d", done, total) + reset)
	}
	if r := it.Node.Repeater(); r != nil {
		b.WriteString(blue + "  ↻" + r.Describe() + reset)
	}
	if len(it.Node.Tags) > 0 {
		b.WriteString(blue + "  :" + strings.Join(it.Node.Tags, ":") + ":" + reset)
	}
	if it.Node.RunningClock() != nil {
		b.WriteString(green + "  ⏱ 计时中" + reset)
	}
	return b.String()
}

// renderMail 画本次会话已经拉到的邮件。
func (s *Session) renderMail() string {
	items := s.mailS.Items()
	if len(items) == 0 {
		return dim + "  还没同步邮件，跟 Jarvis 说「看看新邮件」。" + reset + "\n"
	}
	subjW := max(10, width()-40)
	var b strings.Builder
	for _, it := range items {
		mark := " "
		if it.Msg.Unread {
			mark = green + "●" + reset
		}
		label := ""
		if l := it.Triage.Verdict.Label(); l != "" {
			label = dim + "[" + l + "]" + reset + " "
		}
		from := runewidth.Truncate(it.Msg.From(), 18, "…")
		b.WriteString(fmt.Sprintf("  %s %s%s %s%s%s %s%s\n",
			mark, dim, pad(fmt.Sprintf("%d", it.No), 3),
			gray, pad(from, 18), reset,
			label, runewidth.Truncate(it.Msg.Subject, subjW, "…")))
	}
	return b.String()
}

// renderLedger 画净资产概览、分类账户表和最近几笔流水。
func (s *Session) renderLedger() string {
	path := s.cfg.LedgerFile
	if strings.TrimSpace(path) == "" {
		return dim + "  还没设置账本，用 /ledger <路径> 指定。" + reset + "\n"
	}
	book, err := ledger.Load(path)
	if err != nil {
		return red + "  " + err.Error() + reset + "\n"
	}
	// 余额表算一次就够，下面几处汇总都在它上面走。
	bal := book.Balances()
	assets := ledger.SumPrefix(bal, "Assets")
	debts := ledger.SumPrefix(bal, "Liabilities")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("  %s净资产%s %s%s%s    %s资产%s %s    %s负债%s %s\n",
		gray, reset, bold, money(assets+debts), reset,
		gray, reset, money(assets), gray, reset, money(-debts)))

	b.WriteString(accountTable(bal))

	txs := book.Transactions
	if len(txs) == 0 {
		b.WriteString("\n" + dim + "  账本还是空的。" + reset + "\n")
		return b.String()
	}
	b.WriteString("\n" + gray + "  最近流水" + reset + "\n")
	for i := len(txs) - 1; i >= 0 && i > len(txs)-11; i-- {
		tx := txs[i]
		amt := 0.0
		for _, p := range tx.Postings {
			if p.Amount > amt {
				amt = p.Amount
			}
		}
		b.WriteString(fmt.Sprintf("  %s%s%s  %s  %s\n",
			dim, tx.Date.Format("01-02"), reset,
			pad(runewidth.Truncate(tx.Payee, 24, "…"), 24), padLeft(money(amt), 10)))
	}
	return b.String()
}

// accountTable 把有余额的账户列成两列：账户名靠左，金额靠右。
func accountTable(bal map[string]float64) string {
	names := make([]string, 0, len(bal))
	nameW := 0
	for _, n := range ledger.SortedFrom(bal) {
		if v := bal[n]; v == 0 {
			continue // 结平的账户不占地方
		}
		names = append(names, n)
		if w := runewidth.StringWidth(n); w > nameW {
			nameW = w
		}
	}
	if len(names) == 0 {
		return ""
	}
	if max := width() - 20; nameW > max {
		nameW = max
	}

	var b strings.Builder
	b.WriteString("\n" + gray + "  " + pad("账户", nameW) + "      余额" + reset + "\n")
	b.WriteString(gray + "  " + strings.Repeat("─", nameW) + "  ──────────" + reset + "\n")
	for _, n := range names {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			pad(runewidth.Truncate(n, nameW, "…"), nameW), padLeft(money(bal[n]), 10)))
	}
	return b.String()
}

// money 把金额格式化成两位小数，顺手把 -0.00 拍平成 0.00。
func money(f float64) string {
	if f == 0 {
		f = 0
	}
	return fmt.Sprintf("%.2f", f)
}

func weekday(t time.Time) string {
	return []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(t.Weekday())]
}
