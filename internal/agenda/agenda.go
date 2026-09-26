// Package agenda 把 org 文件里的计划、截止与事件投影到具体日期上。
package agenda

import (
	"sort"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

// Kind 描述一条 agenda 记录为什么出现在某一天。
type Kind int

const (
	KindScheduled Kind = iota // 当天计划执行
	KindOverdue               // 计划日已过且仍未完成
	KindDeadline              // 截止日或其提醒期内
	KindEvent                 // 正文中的活动时间戳（约会、事件）
	KindClosed                // 当天完成
	KindTodo                  // 全局 TODO 列表中的待办（不绑定具体日期）
)

// Label 返回该类型在列表中的前缀标签。
func (k Kind) Label() string {
	switch k {
	case KindScheduled:
		return "计划"
	case KindOverdue:
		return "逾期"
	case KindDeadline:
		return "截止"
	case KindEvent:
		return "事件"
	case KindClosed:
		return "完成"
	case KindTodo:
		return "待办"
	}
	return ""
}

// Item 是 agenda 中的一条记录。
type Item struct {
	Node *org.Node
	Doc  *org.Document
	Kind Kind
	// Time 是触发这条记录的时间戳。
	Time *org.Timestamp
	// At 是这条记录归属的日期（零点）。
	At time.Time
	// Start 是当天的具体时刻；HasTime 为 false 时无意义。
	Start   time.Time
	HasTime bool
	// DaysLeft 对截止项为距离截止的天数（负数表示已逾期），
	// 对逾期计划项为已拖延的天数（负数）。
	DaysLeft int
}

// Category 返回记录所属类别，用于左栏显示。
func (it Item) Category() string { return it.Node.Category() }

// Options 控制 agenda 的生成规则。
type Options struct {
	// DeadlineWarnDays 是截止项提前多少天开始出现（时间戳自带 -Nd 时以后者为准）。
	DeadlineWarnDays int
	// ShowDone 为真时把当天完成的条目也列出来。
	ShowDone bool
	// HidePastOverdue 为真时不再把过期的计划项堆到今天。
	HidePastOverdue bool
	// Now 用于判断"今天"，零值表示使用当前时间。
	Now time.Time
}

// DefaultOptions 返回与 org-agenda 默认行为接近的配置。
// 和 org 一样，当天完成的条目默认不列出来，免得刷屏；需要时开 log 模式。
func DefaultOptions() Options {
	return Options{DeadlineWarnDays: 14}
}

func (o Options) now() time.Time {
	if o.Now.IsZero() {
		return time.Now()
	}
	return o.Now
}

// Day 是 agenda 中的一天。
type Day struct {
	Date  time.Time
	Items []Item
}

// Agenda 是一段连续日期上的日程视图。
type Agenda struct {
	Start time.Time
	Days  []Day
	Opts  Options
}

// Build 生成从 start 起 days 天的 agenda。
func Build(c *org.Collection, start time.Time, days int, opts Options) *Agenda {
	if days < 1 {
		days = 1
	}
	if opts.DeadlineWarnDays == 0 {
		opts.DeadlineWarnDays = 14
	}
	start = dayOf(start)
	end := start.AddDate(0, 0, days-1)
	today := dayOf(opts.now())

	ag := &Agenda{Start: start, Opts: opts}
	buckets := make(map[time.Time][]Item, days)

	add := func(it Item) {
		d := dayOf(it.At)
		if d.Before(start) || d.After(end) {
			return
		}
		buckets[d] = append(buckets[d], it)
	}

	for _, doc := range c.Docs {
		for _, n := range doc.Nodes {
			collect(n, doc, start, end, today, opts, add)
		}
	}

	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i)
		items := buckets[d]
		sortItems(items)
		ag.Days = append(ag.Days, Day{Date: d, Items: items})
	}
	return ag
}

// collect 把单个节点投影到窗口内的各天。
func collect(n *org.Node, doc *org.Document, start, end, today time.Time, opts Options, add func(Item)) {
	// 归档和 COMMENT 子树不参与日程，与 org-agenda 的默认行为一致。
	if !n.Agendable() {
		return
	}
	done := n.IsDone()

	if done && opts.ShowDone && n.Closed != nil {
		add(Item{Node: n, Doc: doc, Kind: KindClosed, Time: n.Closed,
			At: dayOf(n.Closed.Start), Start: n.Closed.Start, HasTime: n.Closed.HasTime})
	}

	if !done && n.Scheduled != nil {
		for _, at := range occurrences(n.Scheduled, start, end) {
			add(Item{Node: n, Doc: doc, Kind: KindScheduled, Time: n.Scheduled,
				At: dayOf(at), Start: at, HasTime: n.Scheduled.HasTime})
		}
		// 计划日已过且仍未完成：堆到今天，并标出拖延天数。
		if !opts.HidePastOverdue && n.Scheduled.Repeat == nil {
			if d := dayOf(n.Scheduled.Start); d.Before(today) {
				add(Item{Node: n, Doc: doc, Kind: KindOverdue, Time: n.Scheduled,
					At: today, DaysLeft: daysBetween(today, d)})
			}
		}
	}

	if !done && n.Deadline != nil {
		warn := opts.DeadlineWarnDays
		if n.Deadline.Warn != nil {
			warn = warnDays(*n.Deadline.Warn)
		}
		// 截止日落在 [start, end+warn] 内的，其提醒期才可能与窗口相交。
		for _, at := range occurrences(n.Deadline, start, end.AddDate(0, 0, warn)) {
			due := dayOf(at)
			first := due.AddDate(0, 0, -warn)
			if first.Before(start) {
				first = start
			}
			for d := first; !d.After(due) && !d.After(end); d = d.AddDate(0, 0, 1) {
				add(Item{Node: n, Doc: doc, Kind: KindDeadline, Time: n.Deadline,
					At: d, Start: at, HasTime: n.Deadline.HasTime, DaysLeft: daysBetween(d, due)})
			}
		}
		// 已过期的截止项无论多久之前，都堆到今天。
		if due := dayOf(n.Deadline.Start); n.Deadline.Repeat == nil && due.Before(today) {
			add(Item{Node: n, Doc: doc, Kind: KindDeadline, Time: n.Deadline,
				At: today, DaysLeft: daysBetween(today, due)})
		}
	}

	if !done {
		for _, ts := range n.Timestamps {
			last := dayOf(ts.Start)
			if ts.HasEnd() {
				last = dayOf(ts.End)
			}
			for d := dayOf(ts.Start); !d.After(last); d = d.AddDate(0, 0, 1) {
				at := d
				if sameDay(d, ts.Start) {
					at = ts.Start
				}
				add(Item{Node: n, Doc: doc, Kind: KindEvent, Time: ts,
					At: dayOf(d), Start: at, HasTime: ts.HasTime && sameDay(d, ts.Start)})
			}
		}
	}
}

// occurrences 返回时间戳在 [from, to] 窗口内的全部发生时间，展开重复器。
func occurrences(ts *org.Timestamp, from, to time.Time) []time.Time {
	if ts.Repeat == nil {
		if d := dayOf(ts.Start); !d.Before(dayOf(from)) && !d.After(dayOf(to)) {
			return []time.Time{ts.Start}
		}
		return nil
	}
	var out []time.Time
	cur := ts.Start
	for i := 0; i < 512; i++ {
		d := dayOf(cur)
		if d.After(dayOf(to)) {
			break
		}
		if !d.Before(dayOf(from)) {
			out = append(out, cur)
		}
		next := ts.Repeat.Delay.Add(cur)
		if !next.After(cur) { // 防御零步长
			break
		}
		cur = next
	}
	return out
}

// warnDays 把提醒量折算成天数。
func warnDays(d org.Delay) int {
	switch d.Unit {
	case org.UnitHour:
		return 0
	case org.UnitWeek:
		return d.N * 7
	case org.UnitMonth:
		return d.N * 30
	case org.UnitYear:
		return d.N * 365
	default:
		return d.N
	}
}

// sortItems 按「有时刻的在前、按时刻升序；其余按类型与优先级」排序。
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.HasTime != b.HasTime {
			return a.HasTime
		}
		if a.HasTime && !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		if a.Kind != b.Kind {
			return kindRank(a.Kind) < kindRank(b.Kind)
		}
		if pa, pb := prioRank(a.Node.Priority), prioRank(b.Node.Priority); pa != pb {
			return pa < pb
		}
		return a.Node.Title < b.Node.Title
	})
}

func kindRank(k Kind) int {
	switch k {
	case KindOverdue:
		return 0
	case KindDeadline:
		return 1
	case KindScheduled:
		return 2
	case KindEvent:
		return 3
	default:
		return 4
	}
}

func prioRank(p string) int {
	switch p {
	case "A":
		return 0
	case "B":
		return 1
	case "C":
		return 2
	case "":
		return 3
	}
	return 4
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool { return dayOf(a).Equal(dayOf(b)) }

// daysBetween 返回 to 相对 from 的天数差（to 在 from 之后为正）。
func daysBetween(from, to time.Time) int {
	return int(dayOf(to).Sub(dayOf(from)).Hours() / 24)
}
