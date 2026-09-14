package agenda

import (
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

// 测试基准日：2026-09-14 周一，与 testdata 中的日期对应。
var base = time.Date(2026, 9, 14, 8, 0, 0, 0, time.Local)

func load(t *testing.T) *org.Collection {
	t.Helper()
	c, err := org.Load("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func titles(items []Item, kind Kind) []string {
	var out []string
	for _, it := range items {
		if it.Kind == kind {
			out = append(out, it.Node.Title)
		}
	}
	return out
}

func has(items []Item, title string, kind Kind) bool {
	for _, it := range items {
		if it.Node.Title == title && it.Kind == kind {
			return true
		}
	}
	return false
}

func TestBuildToday(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = base
	ag := Build(load(t), base, 1, opts)
	if len(ag.Days) != 1 {
		t.Fatalf("天数 = %d", len(ag.Days))
	}
	day := ag.Days[0]

	if !has(day.Items, "写季度汇报", KindScheduled) {
		t.Errorf("今天的计划项缺失: %v", titles(day.Items, KindScheduled))
	}
	if !has(day.Items, "每周例会", KindScheduled) {
		t.Error("重复任务应出现在今天")
	}
	if !has(day.Items, "买菜", KindScheduled) {
		t.Error("跨文件的计划项缺失")
	}
	if !has(day.Items, "写季度汇报", KindDeadline) {
		t.Error("两天后的截止应在提醒期内出现")
	}
	if !has(day.Items, "读书笔记", KindEvent) {
		t.Error("正文时间戳应作为事件出现")
	}
	if has(day.Items, "订机票", KindScheduled) {
		t.Error("CANCELLED 条目不应出现")
	}

	// 带时刻的条目排在最前，并按时刻升序。
	var timed []Item
	for _, it := range day.Items {
		if it.HasTime {
			timed = append(timed, it)
		}
	}
	if len(timed) < 3 {
		t.Fatalf("带时刻的条目 = %d", len(timed))
	}
	if timed[0].Node.Title != "写季度汇报" || timed[0].Start.Hour() != 9 {
		t.Errorf("首项应是 09:00 的汇报，得到 %s %v", timed[0].Node.Title, timed[0].Start)
	}
	for i := 1; i < len(timed); i++ {
		if timed[i].Start.Before(timed[i-1].Start) {
			t.Errorf("时刻未升序: %v", timed)
		}
	}
	if !day.Items[0].HasTime {
		t.Error("带时刻的条目应排在无时刻条目之前")
	}
}

func TestDeadlineWarningWindow(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = base
	c := load(t)

	// "等法务回复合同" 截止 9-20 且自带 -3d，应从 9-17 起才出现。
	ag := Build(c, base, 7, opts)
	for _, day := range ag.Days {
		want := !day.Date.Before(time.Date(2026, 9, 17, 0, 0, 0, 0, time.Local))
		if got := has(day.Items, "等法务回复合同", KindDeadline); got != want {
			t.Errorf("%s: 合同截止出现 = %v，期望 %v", day.Date.Format("01-02"), got, want)
		}
	}
	// 体检预约 9-18 截止，无自带提醒量，默认 14 天内每天都提示。
	if !has(ag.Days[0].Items, "体检预约", KindDeadline) {
		t.Error("默认提醒期内的截止项应出现在今天")
	}
	if it := findItem(ag.Days[0].Items, "体检预约", KindDeadline); it == nil || it.DaysLeft != 4 {
		t.Errorf("剩余天数 = %v", it)
	}
}

func TestOverdueAndRepeat(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = base
	c := load(t)

	// 把某个任务的计划日改到过去，它应堆到今天并标出拖延天数。
	doc := c.Doc("work.org")
	n := doc.Nodes[0]
	for _, cand := range doc.Nodes {
		if cand.Title == "收集各组数据" {
			n = cand
		}
	}
	doc.SetPlanning(n, "SCHEDULED", org.ParseTimestamp("<2026-09-10 Thu>"))

	ag := Build(c, base, 1, opts)
	it := findItem(ag.Days[0].Items, "收集各组数据", KindOverdue)
	if it == nil {
		t.Fatalf("逾期项缺失: %v", titles(ag.Days[0].Items, KindOverdue))
	}
	if it.DaysLeft != -4 {
		t.Errorf("拖延天数 = %d，期望 -4", it.DaysLeft)
	}

	// 每周例会的重复应展开到下一周。
	ag = Build(c, base, 14, opts)
	var weeks int
	for _, day := range ag.Days {
		if has(day.Items, "每周例会", KindScheduled) {
			weeks++
		}
	}
	if weeks != 2 {
		t.Errorf("两周内的例会次数 = %d，期望 2", weeks)
	}
}

func TestClosedItems(t *testing.T) {
	opts := DefaultOptions()
	opts.Now = base
	c := load(t)

	// 默认跟 org 一样不列出完成项，免得当天做完的事把日程刷满
	ag := Build(c, base.AddDate(0, 0, -2), 3, opts)
	if has(ag.Days[0].Items, "确认汇报时间", KindClosed) {
		t.Error("默认不应出现完成项")
	}

	opts.ShowDone = true
	ag = Build(c, base.AddDate(0, 0, -2), 3, opts)
	if !has(ag.Days[0].Items, "确认汇报时间", KindClosed) {
		t.Errorf("开了 log 模式后 9-12 完成的条目应出现: %v", titles(ag.Days[0].Items, KindClosed))
	}
}

func findItem(items []Item, title string, kind Kind) *Item {
	for i := range items {
		if items[i].Node.Title == title && items[i].Kind == kind {
			return &items[i]
		}
	}
	return nil
}
