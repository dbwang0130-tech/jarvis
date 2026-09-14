package org

import (
	"testing"
	"time"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		in      string
		wantY   int
		wantM   time.Month
		wantD   int
		hasTime bool
		hour    int
		active  bool
		repeat  string
		warn    string
		hasEnd  bool
	}{
		{in: "<2026-09-14 Mon>", wantY: 2026, wantM: 9, wantD: 14, active: true},
		{in: "[2026-09-14 Mon 10:12]", wantY: 2026, wantM: 9, wantD: 14, hasTime: true, hour: 10},
		{in: "<2026-09-14 Mon 09:00-11:00>", wantY: 2026, wantM: 9, wantD: 14, hasTime: true, hour: 9, active: true, hasEnd: true},
		{in: "<2026-09-14 Mon 15:00 +1w>", wantY: 2026, wantM: 9, wantD: 14, hasTime: true, hour: 15, active: true, repeat: "+1w"},
		{in: "<2026-09-20 Sun -3d>", wantY: 2026, wantM: 9, wantD: 20, active: true, warn: "3d"},
		{in: "<2026-09-14 一>", wantY: 2026, wantM: 9, wantD: 14, active: true},
		{in: "<2026-09-14 Mon 10:00 ++2d>", wantY: 2026, wantM: 9, wantD: 14, hasTime: true, hour: 10, active: true, repeat: "++2d"},
		{in: "<2026-09-14 Mon .+1m>", wantY: 2026, wantM: 9, wantD: 14, active: true, repeat: ".+1m"},
	}
	for _, tc := range tests {
		ts := ParseTimestamp(tc.in)
		if ts == nil {
			t.Errorf("%s: 解析失败", tc.in)
			continue
		}
		if ts.Start.Year() != tc.wantY || ts.Start.Month() != tc.wantM || ts.Start.Day() != tc.wantD {
			t.Errorf("%s: 日期 = %v", tc.in, ts.Start)
		}
		if ts.HasTime != tc.hasTime || (tc.hasTime && ts.Start.Hour() != tc.hour) {
			t.Errorf("%s: 时刻 hasTime=%v hour=%d", tc.in, ts.HasTime, ts.Start.Hour())
		}
		if ts.Active != tc.active {
			t.Errorf("%s: Active = %v", tc.in, ts.Active)
		}
		if tc.repeat != "" && (ts.Repeat == nil || ts.Repeat.String() != tc.repeat) {
			t.Errorf("%s: Repeat = %v，期望 %s", tc.in, ts.Repeat, tc.repeat)
		}
		if tc.repeat == "" && ts.Repeat != nil {
			t.Errorf("%s: 不该有 Repeat，得到 %v", tc.in, ts.Repeat)
		}
		if tc.warn != "" && (ts.Warn == nil || ts.Warn.String() != tc.warn) {
			t.Errorf("%s: Warn = %v，期望 %s", tc.in, ts.Warn, tc.warn)
		}
		if ts.HasEnd() != tc.hasEnd {
			t.Errorf("%s: HasEnd = %v", tc.in, ts.HasEnd())
		}
	}
}

func TestParseTimestampRange(t *testing.T) {
	ts := ParseTimestamp("<2026-09-14 Mon 09:00>--<2026-09-16 Wed 17:00>")
	if ts == nil || !ts.HasEnd() {
		t.Fatalf("跨日区间解析失败: %+v", ts)
	}
	if ts.End.Day() != 16 || ts.End.Hour() != 17 {
		t.Errorf("结束时间 = %v", ts.End)
	}
}

func TestTimestampString(t *testing.T) {
	for _, in := range []string{
		"<2026-09-14 Mon>",
		"<2026-09-14 Mon 09:00-11:00>",
		"[2026-09-13 Sun 14:00]",
		"<2026-09-14 Mon 15:00 +1w>",
	} {
		if got := ParseTimestamp(in).String(); got != in {
			t.Errorf("往返: 输入 %s，输出 %s", in, got)
		}
	}
}

func TestRepeaterNext(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.Local)
	now := time.Date(2026, 9, 20, 8, 0, 0, 0, time.Local)

	cum := Repeater{Kind: RepeatCumulate, Delay: Delay{N: 1, Unit: UnitWeek}}
	if got := cum.Next(base, now); got.Day() != 8 {
		t.Errorf("+1w = %v，期望 9-08", got)
	}
	catch := Repeater{Kind: RepeatCatchUp, Delay: Delay{N: 1, Unit: UnitWeek}}
	if got := catch.Next(base, now); !got.After(now) {
		t.Errorf("++1w = %v，应晚于 now", got)
	}
	restart := Repeater{Kind: RepeatRestart, Delay: Delay{N: 3, Unit: UnitDay}}
	if got := restart.Next(base, now); got.Day() != 23 {
		t.Errorf(".+3d = %v，期望 9-23", got)
	}
}

func TestParseFile(t *testing.T) {
	doc, err := ParseFile("../../testdata/work.org")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "工作" || doc.Category() != "work" {
		t.Errorf("文件头: title=%q category=%q", doc.Title, doc.Category())
	}
	if len(doc.TodoKeywords) != 3 || len(doc.DoneKeywords) != 2 {
		t.Errorf("TODO 关键字: %v | %v", doc.TodoKeywords, doc.DoneKeywords)
	}
	if len(doc.Roots) != 4 {
		t.Fatalf("顶层节点数 = %d，期望 4", len(doc.Roots))
	}

	n := doc.Roots[0]
	if n.TODO != "NEXT" || n.Priority != "A" || n.Title != "写季度汇报" {
		t.Errorf("首节点解析错: todo=%q prio=%q title=%q", n.TODO, n.Priority, n.Title)
	}
	if len(n.Tags) != 2 || n.Tags[0] != "report" {
		t.Errorf("标签 = %v", n.Tags)
	}
	if n.Deadline == nil || n.Deadline.Start.Day() != 16 {
		t.Errorf("DEADLINE = %v", n.Deadline)
	}
	if n.Scheduled == nil || !n.Scheduled.HasTime || n.Scheduled.Start.Hour() != 9 {
		t.Errorf("SCHEDULED = %v", n.Scheduled)
	}
	if n.Props["EFFORT"] != "2:00" {
		t.Errorf("属性 = %v", n.Props)
	}
	if len(n.Clocks) != 1 || n.Clocks[0].Duration() != 90*time.Minute {
		t.Errorf("计时记录 = %+v", n.Clocks)
	}
	if len(n.Children) != 2 {
		t.Errorf("子节点数 = %d", len(n.Children))
	}
	if !n.IsTodo() || n.IsDone() {
		t.Error("NEXT 应是未完成状态")
	}
	if done := n.Children[1]; !done.IsDone() || done.Closed == nil {
		t.Errorf("DONE 子节点: %+v", done)
	}
	if n.Children[0].Category() != "work" {
		t.Errorf("类别继承失败: %s", n.Children[0].Category())
	}

	repeating := doc.Roots[1]
	if repeating.Scheduled == nil || repeating.Scheduled.Repeat == nil {
		t.Errorf("重复任务未解析: %+v", repeating.Scheduled)
	}
	if wait := doc.Roots[2]; wait.Deadline == nil || wait.Deadline.Warn == nil {
		t.Errorf("提前量未解析: %+v", wait.Deadline)
	}
	if plain := doc.Roots[3]; plain.TODO != "TODO" || plain.Title != "整理桌面" {
		t.Errorf("无正文节点: %+v", plain)
	}
}

func TestInheritedTagsAndBody(t *testing.T) {
	doc, err := ParseFile("../../testdata/personal.org")
	if err != nil {
		t.Fatal(err)
	}
	life := doc.Roots[0]
	buy := life.Children[0]
	if !buy.HasTag("errand") || buy.Priority != "B" {
		t.Errorf("买菜节点: tags=%v prio=%q", buy.AllTags(), buy.Priority)
	}
	if got := buy.Breadcrumb(); len(got) != 2 || got[0] != "生活" {
		t.Errorf("面包屑 = %v", got)
	}
	notes := life.Children[2]
	if len(notes.Timestamps) != 1 || notes.Timestamps[0].Start.Hour() != 21 {
		t.Errorf("正文时间戳 = %+v", notes.Timestamps)
	}
	if doc.Category() != "personal" {
		t.Errorf("类别应回退到文件名: %s", doc.Category())
	}
	if cancelled := doc.Roots[1].Children[0]; !cancelled.IsDone() {
		t.Error("CANCELLED 应属于完成态")
	}
}

func TestHeadlineRoundTrip(t *testing.T) {
	doc := Parse("* NEXT [#A] 写季度汇报 :report:urgent:\n** TODO 子任务\n")
	if got := doc.Nodes[0].String(); got != "* NEXT [#A] 写季度汇报 :report:urgent:" {
		t.Errorf("标题行重建 = %q", got)
	}
	if doc.Nodes[0].End != 2 || doc.Nodes[1].End != 2 {
		t.Errorf("subtree 范围: %d %d", doc.Nodes[0].End, doc.Nodes[1].End)
	}
}
