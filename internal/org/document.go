package org

import (
	"path/filepath"
	"strings"
	"time"
)

// 默认 TODO 关键字，可被文件内的 #+TODO: 覆盖。
var (
	DefaultTodoKeywords = []string{"TODO", "NEXT", "WAITING"}
	DefaultDoneKeywords = []string{"DONE", "CANCELLED"}
)

// Checkbox 是正文里的一个复选框项，如 "- [ ] 买菜"。
type Checkbox struct {
	Line int
	Done bool
	Text string
}

// LogKind 区分 LOGBOOK 里几种常见的记录。
type LogKind int

const (
	// LogState 是状态变更，形如 - State "DONE" from "TODO" [2026-09-14 Mon 11:30]
	LogState LogKind = iota
	// LogNote 是手写的笔记。
	LogNote
	// LogOther 是其他形式的记录，如 Rescheduled、Refiled。
	LogOther
)

// LogEntry 是 LOGBOOK 抽屉里的一条非计时记录。
type LogEntry struct {
	Line int
	Kind LogKind
	// From 与 To 只在状态变更时有值。
	From, To string
	Time     *Timestamp
	// Text 是去掉前导 "- " 之后的原始内容。
	Text string
}

// Clock 是 LOGBOOK 抽屉里的一条计时记录。
type Clock struct {
	Start *Timestamp
	End   *Timestamp // nil 表示计时仍在进行
	Line  int        // 该记录所在行号（0 起）
}

// Running 报告这条计时是否尚未结束。
func (c *Clock) Running() bool { return c.End == nil }

// Duration 返回这条计时的时长；仍在进行时按当前时间计算。
func (c *Clock) Duration() time.Duration {
	if c.Start == nil {
		return 0
	}
	end := time.Now()
	if c.End != nil {
		end = c.End.Start
	}
	if d := end.Sub(c.Start.Start); d > 0 {
		return d
	}
	return 0
}

// OverlapWith 返回这条计时落在 [from, to) 区间内的时长。
func (c *Clock) OverlapWith(from, to time.Time) time.Duration {
	if c.Start == nil {
		return 0
	}
	start := c.Start.Start
	end := time.Now()
	if c.End != nil {
		end = c.End.Start
	}
	if start.Before(from) {
		start = from
	}
	if end.After(to) {
		end = to
	}
	if d := end.Sub(start); d > 0 {
		return d
	}
	return 0
}

// Node 是一个 org 标题及其 subtree。
type Node struct {
	Doc      *Document
	Level    int
	TODO     string
	Priority string // "A" / "B" / "C"，空表示未设置
	Title    string
	Tags     []string

	Line int // 标题行号（0 起）
	End  int // subtree 结束行号，不含该行

	Scheduled *Timestamp
	Deadline  *Timestamp
	Closed    *Timestamp
	// Timestamps 是正文中出现的活动时间戳（不含 planning 行），用于 agenda 事件。
	Timestamps []*Timestamp

	Props  map[string]string
	Clocks []Clock
	// Logs 是 LOGBOOK 里的状态变更与笔记，新的排在前面。
	Logs []LogEntry
	// Boxes 是该标题自身正文里的复选框项。
	Boxes []Checkbox
	// Commented 为真表示标题带 COMMENT，整个子树不参与 agenda。
	Commented bool
	// Cookie 是标题上的进度标记，如 "[2/5]" 或 "[40%]"。
	Cookie string

	Parent   *Node
	Children []*Node
}

// IsTodo 报告节点是否处于未完成的 TODO 状态。
func (n *Node) IsTodo() bool {
	return n.TODO != "" && !n.IsDone()
}

// IsDone 报告节点是否处于完成态关键字。
func (n *Node) IsDone() bool {
	if n.TODO == "" {
		return false
	}
	for _, k := range n.Doc.DoneKeywords {
		if k == n.TODO {
			return true
		}
	}
	return false
}

// Category 返回节点用于 agenda 分组的类别：
// 优先 :CATEGORY: 属性（含继承），其次文件的 #+CATEGORY:，最后是文件名。
func (n *Node) Category() string {
	for cur := n; cur != nil; cur = cur.Parent {
		if c, ok := cur.Props["CATEGORY"]; ok && c != "" {
			return c
		}
	}
	return n.Doc.Category()
}

// AllTags 返回节点自身的标签、从祖先继承的标签以及文件级标签。
func (n *Node) AllTags() []string {
	var out []string
	seen := map[string]bool{}
	add := func(tags []string) {
		for _, t := range tags {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	for cur := n; cur != nil; cur = cur.Parent {
		add(cur.Tags)
	}
	add(n.Doc.FileTags)
	return out
}

// Archived 报告节点自身或其祖先是否带 ARCHIVE 标签。
func (n *Node) Archived() bool {
	for cur := n; cur != nil; cur = cur.Parent {
		for _, t := range cur.Tags {
			if strings.EqualFold(t, ArchiveTag) {
				return true
			}
		}
	}
	return false
}

// InCommentTree 报告节点自身或其祖先是否带 COMMENT 标记。
func (n *Node) InCommentTree() bool {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Commented {
			return true
		}
	}
	return false
}

// Agendable 报告节点是否应当参与 agenda：归档和 COMMENT 子树都要排除。
func (n *Node) Agendable() bool {
	return !n.Archived() && !n.InCommentTree()
}

// Progress 统计该节点及其直接子条目的完成进度。
// 优先按正文里的复选框计，没有复选框时按子条目的 TODO 状态计。
func (n *Node) Progress() (done, total int) {
	for _, b := range n.Boxes {
		total++
		if b.Done {
			done++
		}
	}
	if total > 0 {
		return done, total
	}
	for _, c := range n.Children {
		if c.TODO == "" {
			continue
		}
		total++
		if c.IsDone() {
			done++
		}
	}
	return done, total
}

// HasTag 报告节点是否带有标签 tag（含继承）。
func (n *Node) HasTag(tag string) bool {
	for _, t := range n.AllTags() {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// Breadcrumb 返回从顶层祖先到该节点的标题路径。
func (n *Node) Breadcrumb() []string {
	var parts []string
	for cur := n; cur != nil; cur = cur.Parent {
		parts = append([]string{cur.Title}, parts...)
	}
	return parts
}

// ClockedTotal 返回该节点自身所有计时记录的总时长。
func (n *Node) ClockedTotal() time.Duration {
	var total time.Duration
	for i := range n.Clocks {
		total += n.Clocks[i].Duration()
	}
	return total
}

// ClockedBetween 返回该节点在 [from, to) 区间内的计时时长。
func (n *Node) ClockedBetween(from, to time.Time) time.Duration {
	var total time.Duration
	for i := range n.Clocks {
		total += n.Clocks[i].OverlapWith(from, to)
	}
	return total
}

// SubtreeClocked 返回该节点及其全部子条目的计时总和。
func (n *Node) SubtreeClocked() time.Duration {
	total := n.ClockedTotal()
	for _, c := range n.Children {
		total += c.SubtreeClocked()
	}
	return total
}

// RunningClock 返回尚未结束的计时记录，没有则返回 nil。
func (n *Node) RunningClock() *Clock {
	for i := range n.Clocks {
		if n.Clocks[i].Running() {
			return &n.Clocks[i]
		}
	}
	return nil
}

// Document 是一个已解析的 org 文件。Lines 保存原始行，是写回时的事实来源。
type Document struct {
	Path    string
	Lines   []string
	ModTime time.Time

	Title        string
	CategoryName string
	// FileTags 是 #+FILETAGS: 声明的文件级标签，所有条目都继承。
	FileTags []string
	// LogStates 为真时，切换 TODO 状态会往 LOGBOOK 里记一笔。
	// 由全局配置设置，也能被文件内的 #+STARTUP: logdone / nologdone 覆盖。
	LogStates    bool
	TodoKeywords []string // 未完成态关键字
	DoneKeywords []string // 完成态关键字

	Nodes []*Node // 扁平列表，按文件顺序
	Roots []*Node // 顶层节点
}

// Category 返回文件级类别：#+CATEGORY: 优先，否则用不带扩展名的文件名。
func (d *Document) Category() string {
	if d.CategoryName != "" {
		return d.CategoryName
	}
	base := filepath.Base(d.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// AllKeywords 返回未完成态与完成态关键字的拼接，顺序即状态循环顺序。
func (d *Document) AllKeywords() []string {
	out := make([]string, 0, len(d.TodoKeywords)+len(d.DoneKeywords))
	out = append(out, d.TodoKeywords...)
	return append(out, d.DoneKeywords...)
}

// IsDoneKeyword 报告 kw 是否是该文件的完成态关键字。
func (d *Document) IsDoneKeyword(kw string) bool {
	for _, k := range d.DoneKeywords {
		if k == kw {
			return true
		}
	}
	return false
}
