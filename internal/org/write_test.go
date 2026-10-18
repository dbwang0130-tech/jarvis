package org

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustNode(t *testing.T, d *Document, title string) *Node {
	t.Helper()
	for _, n := range d.Nodes {
		if n.Title == title {
			return n
		}
	}
	t.Fatalf("找不到标题为 %q 的节点", title)
	return nil
}

func TestSetTodoMarksClosed(t *testing.T) {
	d := Parse("* TODO 写文档\nSCHEDULED: <2026-09-14 Mon>\n正文\n")
	n, err := d.SetTodo(mustNode(t, d, "写文档"), "DONE")
	if err != nil {
		t.Fatal(err)
	}
	if n.TODO != "DONE" || n.Closed == nil {
		t.Fatalf("状态 = %q, CLOSED = %v", n.TODO, n.Closed)
	}
	if n.Scheduled == nil {
		t.Error("SCHEDULED 不应丢失")
	}
	planning := d.Lines[n.Line+1]
	if !strings.HasPrefix(planning, "CLOSED:") || !strings.Contains(planning, "SCHEDULED:") {
		t.Errorf("planning 行 = %q", planning)
	}
	if d.Lines[n.Line+2] != "正文" {
		t.Errorf("正文被破坏: %q", d.Lines[n.Line+2])
	}

	// 退回未完成态应清除 CLOSED
	n, _ = d.SetTodo(n, "TODO")
	if n.Closed != nil || strings.Contains(d.Lines[n.Line+1], "CLOSED") {
		t.Errorf("CLOSED 未清除: %q", d.Lines[n.Line+1])
	}
}

func TestSetTodoAdvancesRepeater(t *testing.T) {
	d := Parse("* TODO 每周例会\nSCHEDULED: <2026-09-14 Mon 15:00-16:00 +1w>\n")
	n, err := d.SetTodo(mustNode(t, d, "每周例会"), "DONE")
	if err != nil {
		t.Fatal(err)
	}
	if n.TODO != "TODO" {
		t.Errorf("重复任务完成后状态应保持未完成，得到 %q", n.TODO)
	}
	if n.Scheduled.Start.Day() != 21 {
		t.Errorf("SCHEDULED 应推进到 9-21，得到 %v", n.Scheduled.Start)
	}
	if !n.Scheduled.HasEnd() || n.Scheduled.End.Day() != 21 || n.Scheduled.End.Hour() != 16 {
		t.Errorf("结束时间应同步推进，得到 %v", n.Scheduled.End)
	}
	if n.Scheduled.Repeat == nil {
		t.Error("重复器应保留")
	}
	if n.Props["LAST_REPEAT"] == "" {
		t.Errorf("应记录 LAST_REPEAT，属性 = %v", n.Props)
	}
	if !strings.Contains(strings.Join(d.Lines, "\n"), ":PROPERTIES:") {
		t.Error("应创建 PROPERTIES 抽屉")
	}
}

func TestSetPlanning(t *testing.T) {
	d := Parse("* TODO 买菜\n")
	n := mustNode(t, d, "买菜")
	ts := ParseTimestamp("<2026-09-15 Tue 18:00>")
	n = d.SetPlanning(n, "SCHEDULED", ts)
	if n.Scheduled == nil || n.Scheduled.Start.Day() != 15 {
		t.Fatalf("SCHEDULED = %v", n.Scheduled)
	}
	if d.Lines[1] != "SCHEDULED: <2026-09-15 Tue 18:00>" {
		t.Errorf("planning 行 = %q", d.Lines[1])
	}
	n = d.SetPlanning(n, "DEADLINE", ParseTimestamp("<2026-09-16 Wed>"))
	if !strings.HasPrefix(d.Lines[1], "DEADLINE:") {
		t.Errorf("DEADLINE 应排在 SCHEDULED 之前: %q", d.Lines[1])
	}
	n = d.SetPlanning(n, "SCHEDULED", nil)
	if n.Scheduled != nil || strings.Contains(d.Lines[1], "SCHEDULED") {
		t.Errorf("SCHEDULED 未清除: %q", d.Lines[1])
	}
	d.SetPlanning(n, "DEADLINE", nil)
	if len(d.Lines) != 1 {
		t.Errorf("planning 行应被删除: %v", d.Lines)
	}
}

func TestClockInOut(t *testing.T) {
	d := Parse("* TODO 写代码\nSCHEDULED: <2026-09-14 Mon>\n")
	n := mustNode(t, d, "写代码")
	start := time.Date(2026, 9, 14, 10, 0, 0, 0, time.Local)

	n, err := d.ClockIn(n, start)
	if err != nil {
		t.Fatal(err)
	}
	if n.RunningClock() == nil {
		t.Fatal("应有进行中的计时")
	}
	if _, err := d.ClockIn(n, start); err == nil {
		t.Error("重复 clock in 应报错")
	}

	n, err = d.ClockOut(n, start.Add(95*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if n.RunningClock() != nil {
		t.Error("计时应已结束")
	}
	if got := n.ClockedTotal(); got != 95*time.Minute {
		t.Errorf("总时长 = %v", got)
	}
	logbook := strings.Join(d.Lines, "\n")
	if !strings.Contains(logbook, "=>  1:35") {
		t.Errorf("时长格式错误:\n%s", logbook)
	}
	if !strings.Contains(logbook, ":LOGBOOK:") || !strings.Contains(logbook, ":END:") {
		t.Errorf("LOGBOOK 抽屉缺失:\n%s", logbook)
	}
	if _, err := d.ClockOut(n, start); err == nil {
		t.Error("无进行中计时时 clock out 应报错")
	}
}

func TestAddNode(t *testing.T) {
	d := Parse("* 项目\n** TODO 已有任务\n")
	parent := mustNode(t, d, "项目")
	n := d.AddNode(NewNodeSpec{
		Title: "新任务", TODO: "TODO", Priority: "A",
		Tags:      []string{"work"},
		Scheduled: ParseTimestamp("<2026-09-15 Tue 09:00>"),
		Body:      []string{"细节说明"},
		Parent:    parent,
	})
	if n == nil {
		t.Fatal("AddNode 返回 nil")
	}
	if n.Level != 2 || n.Parent == nil || n.Parent.Title != "项目" {
		t.Errorf("层级/父节点错: level=%d parent=%v", n.Level, n.Parent)
	}
	if n.Scheduled == nil || !n.HasTag("work") || n.Priority != "A" {
		t.Errorf("新节点属性错: %+v", n)
	}
	// AddNode 之后此前持有的节点指针已失效，需要重新取。
	if parent = mustNode(t, d, "项目"); len(parent.Children) != 2 {
		t.Errorf("父节点子数 = %d", len(parent.Children))
	}
	if got := d.Lines[n.Line]; got != "** TODO [#A] 新任务 :work:" {
		t.Errorf("标题行 = %q", got)
	}

	top := d.AddNode(NewNodeSpec{Title: "独立条目", TODO: "TODO"})
	if top == nil || top.Level != 1 {
		t.Fatalf("顶层节点 = %+v", top)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	src := "#+TITLE: 测\n\n* TODO 条目 :a:\nSCHEDULED: <2026-09-14 Mon>\n正文\n"
	d := Parse(src)
	path := t.TempDir() + "/out.org"
	d.Path = path
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	back, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(back.Lines, "\n") != strings.Join(d.Lines, "\n") {
		t.Errorf("往返不一致:\n%q\n%q", back.Lines, d.Lines)
	}
}

func TestSetRepeat(t *testing.T) {
	d := Parse("* TODO 倒垃圾\nSCHEDULED: <2026-09-14 Mon 20:00>\n")
	n := mustNode(t, d, "倒垃圾")

	r, err := ParseRepeat("每天")
	if err != nil {
		t.Fatal(err)
	}
	n, err = d.SetRepeat(n, "", r)
	if err != nil {
		t.Fatal(err)
	}
	if n.Scheduled.Repeat == nil || n.Scheduled.Repeat.String() != "+1d" {
		t.Fatalf("重复规则 = %v", n.Scheduled.Repeat)
	}
	if got := d.Lines[1]; got != "SCHEDULED: <2026-09-14 Mon 20:00 +1d>" {
		t.Errorf("planning 行 = %q", got)
	}
	if n.Scheduled.Start.Hour() != 20 {
		t.Error("设置重复不应改动时间本身")
	}

	// 标记完成应推进到明天而不是变成 DONE
	n, _ = d.SetTodo(n, "DONE")
	if n.TODO != "TODO" || n.Scheduled.Start.Day() != 15 {
		t.Errorf("完成后 = %s %v", n.TODO, n.Scheduled.Start)
	}

	// 清除
	n, err = d.SetRepeat(n, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n.Repeater() != nil || strings.Contains(d.Lines[1], "+1d") {
		t.Errorf("未清除: %q", d.Lines[1])
	}
}

func TestSetRepeatNeedsTime(t *testing.T) {
	d := Parse("* TODO 没有时间的条目\n")
	n := mustNode(t, d, "没有时间的条目")
	r, _ := ParseRepeat("+1w")
	if _, err := d.SetRepeat(n, "", r); err == nil {
		t.Error("没有计划或截止时间时应报错")
	}
}

func TestSetRepeatOnDeadline(t *testing.T) {
	d := Parse("* TODO 交房租\nDEADLINE: <2026-10-01 Thu>\n")
	n := mustNode(t, d, "交房租")
	r, _ := ParseRepeat("每月")
	n, err := d.SetRepeat(n, "", r)
	if err != nil {
		t.Fatal(err)
	}
	if n.Deadline.Repeat == nil || n.Deadline.Repeat.String() != "+1m" {
		t.Fatalf("重复规则挂错了: %v", n.Deadline)
	}
	if got := d.Lines[1]; got != "DEADLINE: <2026-10-01 Thu +1m>" {
		t.Errorf("planning 行 = %q", got)
	}
}

func TestParseRepeatForms(t *testing.T) {
	cases := map[string]string{
		"+1w": "+1w", "每周": "+1w", "每天": "+1d", "每月": "+1m",
		"每两周": "+2w", "++1w": "++1w", ".+3d": ".+3d", "+2y": "+2y",
	}
	for in, want := range cases {
		r, err := ParseRepeat(in)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if r.String() != want {
			t.Errorf("%s -> %s，期望 %s", in, r.String(), want)
		}
	}
	if r, err := ParseRepeat(""); r != nil || err != nil {
		t.Error("空输入应表示清除")
	}
	for _, bad := range []string{"-3d", "每隔一阵子", "1w"} {
		if _, err := ParseRepeat(bad); err == nil {
			t.Errorf("%q 应解析失败", bad)
		}
	}
}

func TestParseUserTimeWithRepeat(t *testing.T) {
	base := time.Date(2026, 9, 14, 8, 0, 0, 0, time.Local)
	cases := map[string]string{
		"2026-09-20 +1w":             "<2026-09-20 Sun +1w>",
		"2026-09-20 09:00 .+2d":      "<2026-09-20 Sun 09:00 .+2d>",
		"2026-09-20 14:00-16:00 +1w": "<2026-09-20 Sun 14:00-16:00 +1w>",
		"明天 07:00 +2d":               "<2026-09-15 Tue 07:00 +2d>",
		"2026-09-20 -3d":             "<2026-09-20 Sun -3d>",
	}
	for in, want := range cases {
		ts, err := ParseUserTime(in, base)
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if got := ts.String(); got != want {
			t.Errorf("%s -> %s，期望 %s", in, got, want)
		}
	}
	if _, err := ParseUserTime("+1w", base); err == nil {
		t.Error("只有重复规则没有日期时应报错")
	}
}

func TestRepeaterDescribe(t *testing.T) {
	cases := map[string]string{
		"+1w":  "每周",
		"+2d":  "每 2天",
		".+3d": "完成后隔 3天",
		"++1m": "每 1个月（跳过错过的）",
	}
	for in, want := range cases {
		r, err := ParseRepeat(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := r.Describe(); got != want {
			t.Errorf("%s 描述为 %q，期望 %q", in, got, want)
		}
	}
}

func TestToggleCheckbox(t *testing.T) {
	d := Parse("* TODO 出门清单 [1/3]\n- [X] 钥匙\n- [ ] 钱包\n- [ ] 耳机\n")
	n := mustNode(t, d, "出门清单")
	if done, total := n.Progress(); done != 1 || total != 3 {
		t.Fatalf("初始进度 = %d/%d", done, total)
	}

	n, err := d.ToggleCheckbox(n, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !n.Boxes[1].Done {
		t.Error("第二个复选框应被勾上")
	}
	if d.Lines[2] != "- [X] 钱包" {
		t.Errorf("行内容 = %q", d.Lines[2])
	}
	if n.Cookie != "[2/3]" {
		t.Errorf("cookie = %q", n.Cookie)
	}
	if d.Lines[0] != "* TODO 出门清单 [2/3]" {
		t.Errorf("标题行 = %q", d.Lines[0])
	}

	// 再切一次应还原
	n, _ = d.ToggleCheckbox(n, 1)
	if n.Boxes[1].Done || n.Cookie != "[1/3]" {
		t.Errorf("未还原: done=%v cookie=%q", n.Boxes[1].Done, n.Cookie)
	}
	if _, err := d.ToggleCheckbox(n, 9); err == nil {
		t.Error("越界应报错")
	}
}

func TestCookiePercent(t *testing.T) {
	d := Parse("* TODO 任务 [0%]\n- [ ] 甲\n- [ ] 乙\n")
	n := mustNode(t, d, "任务")
	n, _ = d.ToggleCheckbox(n, 0)
	if n.Cookie != "[50%]" {
		t.Errorf("百分比 cookie = %q", n.Cookie)
	}
	if !strings.Contains(d.Lines[0], "[50%]") {
		t.Errorf("标题行 = %q", d.Lines[0])
	}
}

func TestDeleteChildKeepsParent(t *testing.T) {
	d := Parse("* 项目\n** TODO 甲\n** TODO 乙\n")
	d.DeleteNode(mustNode(t, d, "甲"))
	parent := mustNode(t, d, "项目")
	if len(parent.Children) != 1 || parent.Children[0].Title != "乙" {
		t.Errorf("父节点子条目 = %v", parent.Children)
	}
	if parent.End != 2 {
		t.Errorf("父节点范围 = %d", parent.End)
	}
}

func TestDeleteLastNode(t *testing.T) {
	d := Parse("* 只有这一个\n正文\n")
	d.DeleteNode(d.Nodes[0])
	if len(d.Nodes) != 0 || len(d.Lines) != 0 {
		t.Errorf("应删空: nodes=%d lines=%v", len(d.Nodes), d.Lines)
	}
}

func TestLogStateChanges(t *testing.T) {
	d := Parse("* TODO 写文档\n")
	d.LogStates = true
	n := mustNode(t, d, "写文档")

	n, err := d.SetTodo(n, "NEXT")
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Logs) != 1 {
		t.Fatalf("日志条数 = %d: %+v", len(n.Logs), n.Logs)
	}
	log := n.Logs[0]
	if log.Kind != LogState || log.From != "TODO" || log.To != "NEXT" {
		t.Errorf("日志内容 = %+v", log)
	}
	if log.Time == nil || log.Time.Active {
		t.Errorf("日志时间应是 inactive 时间戳: %+v", log.Time)
	}

	// 新的记录排在前面
	n, _ = d.SetTodo(n, "DONE")
	if len(n.Logs) != 2 || n.Logs[0].To != "DONE" {
		t.Errorf("最新记录应在最前: %+v", n.Logs)
	}
	text := strings.Join(d.Lines, "\n")
	if !strings.Contains(text, `- State "DONE"`) || !strings.Contains(text, `from "NEXT"`) {
		t.Errorf("写入格式不对:\n%s", text)
	}
	if !strings.Contains(text, ":LOGBOOK:") {
		t.Errorf("应写进 LOGBOOK 抽屉:\n%s", text)
	}
	// CLOSED 仍然照常记
	if n.Closed == nil {
		t.Error("完成时间不该丢")
	}
}

func TestLogStatesOff(t *testing.T) {
	d := Parse("* TODO 写文档\n")
	n, _ := d.SetTodo(mustNode(t, d, "写文档"), "DONE")
	if len(n.Logs) != 0 {
		t.Errorf("默认不记状态变更: %+v", n.Logs)
	}
	if strings.Contains(strings.Join(d.Lines, "\n"), ":LOGBOOK:") {
		t.Error("不该凭空造出 LOGBOOK 抽屉")
	}
}

func TestStartupOverridesLogSetting(t *testing.T) {
	on := Parse("#+STARTUP: logdone\n* TODO 甲\n")
	if !on.LogStates {
		t.Error("#+STARTUP: logdone 应打开日志")
	}
	off := Parse("#+STARTUP: nologdone\n* TODO 乙\n")
	if off.LogStates {
		t.Error("#+STARTUP: nologdone 应关闭日志")
	}
	if !on.hasStartupLogSetting() || !off.hasStartupLogSetting() {
		t.Error("应能认出文件里有显式设置")
	}
	if Parse("* TODO 丙\n").hasStartupLogSetting() {
		t.Error("没写 STARTUP 的文件不该被认为有设置")
	}
}

func TestRepeatingTaskLogsStateChange(t *testing.T) {
	d := Parse("* TODO 每周例会\nSCHEDULED: <2026-09-14 Mon +1w>\n")
	d.LogStates = true
	n, _ := d.SetTodo(mustNode(t, d, "每周例会"), "DONE")
	if n.TODO != "TODO" {
		t.Fatalf("循环任务状态 = %s", n.TODO)
	}
	if len(n.Logs) != 1 || n.Logs[0].To != "DONE" {
		t.Errorf("循环任务完成也该记一笔: %+v", n.Logs)
	}
}

func TestAddNote(t *testing.T) {
	d := Parse("* TODO 写文档\n")
	n := d.AddNote(mustNode(t, d, "写文档"), "等设计稿定了再动手", time.Now())
	if len(n.Logs) == 0 || n.Logs[0].Kind != LogNote {
		t.Fatalf("笔记未记录: %+v", n.Logs)
	}
	text := strings.Join(d.Lines, "\n")
	if !strings.Contains(text, "Note taken on") || !strings.Contains(text, "等设计稿定了再动手") {
		t.Errorf("笔记内容不对:\n%s", text)
	}
	// 空笔记不写
	before := len(d.Lines)
	d.AddNote(d.NodeAt(n.Line), "   ", time.Now())
	if len(d.Lines) != before {
		t.Error("空笔记不该写入")
	}
}

// 整文件覆盖的写法必须配一道「文件被别人改过没有」的检查，否则两个地方
// 同时用（另一台电脑、别的 org 编辑器）就是在互相吃改动。
//
// 这条是补的真实事故：别处加了一条行程，这边手里拿着加之前的内容，
// 一保存就把它冲没了——两边都看不出异样，等发现时已经找不回来。
func TestSaveRefusesStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.org")
	if err := os.WriteFile(path, []byte("* TODO 原来的\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	coll, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := coll.Docs[0]

	// 别人在这期间改了文件。mtime 的精度有限，往后挪一点确保测得出来。
	if err := os.WriteFile(path, []byte("* TODO 原来的\n* TODO 别人加的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	doc.Lines = append(doc.Lines, "* TODO 我加的")
	err = doc.Save()
	if !errors.Is(err, ErrStale) {
		t.Fatalf("该拒绝保存，实际 err=%v", err)
	}

	// 最要紧的：别人的改动还在。
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "别人加的") {
		t.Errorf("把别人的改动冲掉了：\n%s", raw)
	}
	if strings.Contains(string(raw), "我加的") {
		t.Errorf("拒绝了却还是写进去了：\n%s", raw)
	}

	// 重新读一遍之后就该能存了——拒绝是为了让人重来，不是把路堵死。
	coll, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	coll.Docs[0].Lines = append(coll.Docs[0].Lines, "* TODO 重来一次")
	if err := coll.Docs[0].Save(); err != nil {
		t.Fatalf("重新读过之后该能存：%v", err)
	}
}

// 没有修改时间记录的（新建的文档、测试里手搓的）不该被拦住。
func TestSaveAllowsBrandNew(t *testing.T) {
	doc := Parse("* TODO 新的\n")
	doc.Path = filepath.Join(t.TempDir(), "new.org")
	if err := doc.Save(); err != nil {
		t.Fatalf("新建的文档该能存：%v", err)
	}
}

func TestDeleteNode(t *testing.T) {
	d := Parse("* 甲\n正文甲\n** 甲一\n** 甲二\n\n* 乙\n正文乙\n")
	n := mustNode(t, d, "甲")
	if got := n.DescendantCount(); got != 2 {
		t.Errorf("子条目数 = %d", got)
	}
	// 子树一直延伸到下一个同级标题之前，中间的空行也算在内
	removed := d.DeleteNode(n)
	if removed != 5 {
		t.Errorf("删除行数 = %d", removed)
	}
	if len(d.Nodes) != 1 || d.Nodes[0].Title != "乙" {
		t.Errorf("剩余节点 = %v", d.Nodes)
	}
	if strings.Join(d.Lines, "\n") != "* 乙\n正文乙" {
		t.Errorf("剩余内容 = %q", strings.Join(d.Lines, "\n"))
	}
}

func TestClockSummaries(t *testing.T) {
	d := Parse(`* 项目
:LOGBOOK:
CLOCK: [2026-09-14 Mon 09:00]--[2026-09-14 Mon 10:30] =>  1:30
:END:
** 子任务
:LOGBOOK:
CLOCK: [2026-09-13 Sun 14:00]--[2026-09-13 Sun 15:00] =>  1:00
CLOCK: [2026-09-14 Mon 16:00]--[2026-09-14 Mon 16:30] =>  0:30
:END:
`)
	parent := mustNode(t, d, "项目")
	if got := parent.ClockedTotal(); got != 90*time.Minute {
		t.Errorf("自身计时 = %v", got)
	}
	if got := parent.SubtreeClocked(); got != 3*time.Hour {
		t.Errorf("子树计时合计 = %v", got)
	}
}
