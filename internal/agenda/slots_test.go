package agenda

import (
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

func TestBusyMergesAndUsesEffort(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = base
	busy := Busy(load(t), base, opts)
	if len(busy) == 0 {
		t.Fatal("应有忙碌区间")
	}
	// 09:00-11:00 的汇报（显式区间）
	first := busy[0]
	if first.Start.Hour() != 9 || first.End.Hour() != 11 {
		t.Errorf("首个忙碌区间 = %v-%v", first.Start, first.End)
	}
	// 15:00-16:00 的例会
	var found bool
	for _, b := range busy {
		if b.Start.Hour() == 15 && b.End.Hour() == 16 {
			found = true
		}
	}
	if !found {
		t.Errorf("例会区间缺失: %+v", busy)
	}
	// 21:00 的读书笔记没有结束时间，按默认时长占用
	for _, b := range busy {
		if b.Start.Hour() == 21 && b.Duration() != DefaultDuration {
			t.Errorf("默认时长 = %v", b.Duration())
		}
	}
}

func TestBusyMergeOverlap(t *testing.T) {
	c, err := org.Load("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	doc := c.Docs[0]
	doc.AddNode(org.NewNodeSpec{
		Title: "重叠会议", TODO: "TODO",
		Scheduled: org.ParseTimestamp("<2026-09-14 Mon 10:30-12:00>"),
	})
	opts := DefaultOptions()
	opts.Now = base
	busy := Busy(c, base, opts)
	if busy[0].Start.Hour() != 9 || busy[0].End.Hour() != 12 {
		t.Errorf("重叠区间应合并为 09:00-12:00，得到 %v-%v", busy[0].Start, busy[0].End)
	}
}

func TestFreeSlots(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = time.Date(2026, 9, 14, 7, 0, 0, 0, time.Local) // 早于工作时段
	free := Free(load(t), base, DefaultWorkHours(), 30*time.Minute, opts)
	if len(free) == 0 {
		t.Fatal("应有空档")
	}
	// 会议之间的 11:00-15:00，以及例会后到读书笔记之间的 16:00-21:00
	want := map[int]int{11: 15, 16: 21}
	got := map[int]int{}
	for _, s := range free {
		got[s.Start.Hour()] = s.End.Hour()
	}
	for start, end := range want {
		if got[start] != end {
			t.Errorf("空档 %d:00 的结束时间 = %d:00，期望 %d:00 (全部: %v)", start, got[start], end, got)
		}
	}
	for i := range free {
		if free[i].Duration() < 30*time.Minute {
			t.Errorf("不应返回短于阈值的空档: %v", free[i])
		}
		if i > 0 && free[i].Start.Before(free[i-1].End) {
			t.Error("空档不应重叠")
		}
	}
	// 空档不得越出工作时段
	if free[0].Start.Hour() < 9 || free[len(free)-1].End.Hour() > 22 {
		t.Errorf("空档越界: %v", free)
	}
}

func TestFreeSlotsStartFromNow(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = time.Date(2026, 9, 14, 16, 20, 0, 0, time.Local)
	free := Free(load(t), base, DefaultWorkHours(), 15*time.Minute, opts)
	if len(free) == 0 {
		t.Fatal("下午仍应有空档")
	}
	if free[0].Start.Before(opts.Now) {
		t.Errorf("今天的空档应从当前时刻起算，得到 %v", free[0].Start)
	}
}

func TestTodos(t *testing.T) {
	c := load(t)
	all := Todos(c, TodoFilter{})
	if len(all) == 0 {
		t.Fatal("待办列表为空")
	}
	for _, it := range all {
		if it.Node.IsDone() {
			t.Errorf("默认不应包含完成项: %s", it.Node.Title)
		}
	}
	// [#A] 的排在最前
	if all[0].Node.Priority != "A" {
		t.Errorf("首项优先级 = %q (%s)", all[0].Node.Priority, all[0].Node.Title)
	}

	tagged := Todos(c, TodoFilter{Tags: []string{"errand"}})
	if len(tagged) != 1 || tagged[0].Node.Title != "买菜" {
		t.Errorf("标签过滤 = %v", tagged)
	}
	waiting := Todos(c, TodoFilter{Keywords: []string{"WAITING"}})
	if len(waiting) != 1 || waiting[0].Node.Title != "等法务回复合同" {
		t.Errorf("状态过滤 = %v", waiting)
	}
	q := Todos(c, TodoFilter{Query: "汇报"})
	if len(q) != 1 || q[0].Node.Title != "写季度汇报" {
		t.Errorf("关键词搜索 = %v", q)
	}
	cat := Todos(c, TodoFilter{Category: "personal"})
	if len(cat) != 2 {
		t.Errorf("类别过滤 = %d 条", len(cat))
	}
	withDone := Todos(c, TodoFilter{IncludeDone: true})
	if len(withDone) <= len(all) {
		t.Error("IncludeDone 应返回更多条目")
	}
}
