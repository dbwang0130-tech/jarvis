package agenda

import (
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

// TodoFilter 描述全局待办列表的筛选条件。
type TodoFilter struct {
	// Keywords 限定 TODO 状态；为空表示全部未完成状态。
	Keywords []string
	// Tags 限定标签，条目需带有其中任意一个（含继承标签）。
	Tags []string
	// Query 是标题的不区分大小写子串匹配。
	Query string
	// Category 限定所属类别。
	Category string
	// IncludeDone 为真时把完成态条目也列出来。
	IncludeDone bool
}

func (f TodoFilter) match(n *org.Node) bool {
	if n.TODO == "" || !n.Agendable() {
		return false
	}
	if n.IsDone() && !f.IncludeDone {
		return false
	}
	if len(f.Keywords) > 0 && !contains(f.Keywords, n.TODO) {
		return false
	}
	if f.Category != "" && !strings.EqualFold(f.Category, n.Category()) {
		return false
	}
	if f.Query != "" && !strings.Contains(strings.ToLower(n.Title), strings.ToLower(f.Query)) {
		return false
	}
	if len(f.Tags) > 0 {
		ok := false
		for _, t := range f.Tags {
			if n.HasTag(t) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Todos 返回集合中符合筛选条件的待办，按「优先级 -> 截止紧迫度 -> 标题」排序。
func Todos(c *org.Collection, f TodoFilter) []Item {
	now := time.Now()
	var out []Item
	for _, doc := range c.Docs {
		for _, n := range doc.Nodes {
			if !f.match(n) {
				continue
			}
			it := Item{Node: n, Doc: doc, Kind: KindTodo}
			switch {
			case n.Deadline != nil:
				it.Time = n.Deadline
				it.At = dayOf(n.Deadline.Start)
				it.DaysLeft = daysBetween(now, n.Deadline.Start)
			case n.Scheduled != nil:
				it.Time = n.Scheduled
				it.At = dayOf(n.Scheduled.Start)
				it.DaysLeft = daysBetween(now, n.Scheduled.Start)
			}
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if pa, pb := prioRank(a.Node.Priority), prioRank(b.Node.Priority); pa != pb {
			return pa < pb
		}
		// 有期限的排在无期限之前，紧迫的在前。
		if a.Time == nil != (b.Time == nil) {
			return a.Time != nil
		}
		if a.Time != nil && a.DaysLeft != b.DaysLeft {
			return a.DaysLeft < b.DaysLeft
		}
		return a.Node.Title < b.Node.Title
	})
	return out
}
