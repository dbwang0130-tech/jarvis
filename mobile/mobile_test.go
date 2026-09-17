package mobile

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 这一层是 iOS 端唯一的入口，所以约定必须钉死：进出都是 JSON，
// 出什么岔子都不能 panic（在手机上崩一次就是整个 app 挂掉）。

const orgSample = `#+TITLE: 工作

* NEXT [#A] 写毕业论文  :thesis:
  SCHEDULED: <2026-09-16 三 14:00-16:00>
** TODO 补开题报告 [1/3]
   DEADLINE: <2026-09-18 五>
   - [X] 查文献
   - [ ] 写初稿
   - [ ] 改格式
* TODO 每周组会
  SCHEDULED: <2026-09-17 四 10:00 +1w>
`

const ledgerSample = `2026/09/01 房租
    Expenses:Housing:Rent          3000.00
    Assets:Bank:CMB

2026/09/10 发工资
    Assets:Bank:CMB               18000.00
    Income:Salary
`

func snapshot(t *testing.T, in SnapshotInput) SnapshotOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out SnapshotOutput
	if err := json.Unmarshal([]byte(Snapshot(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

func TestSnapshotReadsAgenda(t *testing.T) {
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"work.org": orgSample},
		Days:  7,
		Now:   "2026-09-16T09:00:00+08:00",
	})
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if len(out.Days) == 0 {
		t.Fatal("一天都没解析出来")
	}

	// 今天那条带时刻的计划该在。
	var found *Item
	for _, d := range out.Days {
		for i, it := range d.Items {
			if strings.Contains(it.Title, "写毕业论文") && d.Today {
				found = &d.Items[i]
			}
		}
	}
	if found == nil {
		t.Fatalf("今天的计划没找到：%+v", out.Days)
	}
	if found.Time != "14:00-16:00" {
		t.Fatalf("时刻不对：%q", found.Time)
	}
	if found.State != "NEXT" || found.Priority != "A" {
		t.Fatalf("状态或优先级不对：%+v", found)
	}
	if len(found.Tags) != 1 || found.Tags[0] != "thesis" {
		t.Fatalf("标签不对：%v", found.Tags)
	}
	// 引用要能拿回去改，格式跟桌面端一致。
	if !strings.HasPrefix(found.Ref, "work.org:") {
		t.Fatalf("引用格式不对：%q", found.Ref)
	}
}

// 子条目要带上父任务路径和勾选进度——手机屏幕小，这两样最省事。
func TestSnapshotCarriesSubtaskInfo(t *testing.T) {
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"work.org": orgSample},
		Now:   "2026-09-16T09:00:00+08:00",
	})

	var sub *Item
	for i, it := range out.Todos {
		if strings.Contains(it.Title, "补开题报告") {
			sub = &out.Todos[i]
		}
	}
	if sub == nil {
		t.Fatalf("待办里没有子条目：%+v", out.Todos)
	}
	if !strings.Contains(sub.Parents, "写毕业论文") {
		t.Fatalf("没带上父任务：%q", sub.Parents)
	}
	if sub.Done != 1 || sub.Total != 3 {
		t.Fatalf("勾选进度不对：%d/%d", sub.Done, sub.Total)
	}
}

func TestSnapshotReadsLedger(t *testing.T) {
	out := snapshot(t, SnapshotInput{
		Files:  map[string]string{"work.org": orgSample},
		Ledger: ledgerSample,
		Now:    "2026-09-16T09:00:00+08:00",
	})
	if out.Ledger == nil {
		t.Fatal("账本没解析出来")
	}
	if out.Ledger.Assets != 15000 {
		t.Fatalf("资产该是 15000（18000 工资 - 3000 房租），得到 %v", out.Ledger.Assets)
	}
	if len(out.Ledger.Recent) != 2 {
		t.Fatalf("流水该有两笔，得到 %d", len(out.Ledger.Recent))
	}
	// 最近的排前面。
	if out.Ledger.Recent[0].Date != "2026-09-10" {
		t.Fatalf("流水该按时间倒序：%+v", out.Ledger.Recent)
	}
	if len(out.Ledger.Accounts) == 0 {
		t.Fatal("账户列表是空的")
	}
}

// 没有账本就不该有账本那块，界面据此决定显不显示。
func TestSnapshotWithoutLedger(t *testing.T) {
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"work.org": orgSample},
		Now:   "2026-09-16T09:00:00+08:00",
	})
	if out.Ledger != nil {
		t.Fatal("没传账本却给了账本数据")
	}
}

// 多个文件时顺序要稳定，不然界面每次刷新都跳。
func TestSnapshotOrderIsStable(t *testing.T) {
	in := SnapshotInput{
		Files: map[string]string{
			"z.org": "* TODO 甲\n",
			"a.org": "* TODO 乙\n",
			"m.org": "* TODO 丙\n",
		},
		Now: "2026-09-16T09:00:00+08:00",
	}
	first := Snapshot(mustJSON(t, in))
	for i := 0; i < 5; i++ {
		if got := Snapshot(mustJSON(t, in)); got != first {
			t.Fatal("同样的输入给出了不同的结果")
		}
	}
}

// 在手机上崩一次就是整个 app 挂掉，所以什么烂输入都得稳稳接住。
func TestSnapshotNeverPanics(t *testing.T) {
	for _, in := range []string{
		"", "不是 JSON", "null", "[]", "{}",
		`{"files":null}`,
		`{"files":{"a.org":""}}`,
		`{"files":{"a.org":"* 没有状态的条目"}}`,
		`{"files":{"a.org":"*** 直接三级标题\n"}}`,
		`{"files":{"a.org":"* TODO 坏时间戳\n  SCHEDULED: <乱七八糟>\n"}}`,
		`{"ledger":"这不是账本"}`,
		`{"now":"不是时间"}`,
		`{"days":-5,"files":{"a.org":"* TODO 甲\n"}}`,
		`{"files":{"a.org":"* TODO 甲\n"},"days":99999}`,
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("输入 %q 让它崩了：%v", in, r)
				}
			}()
			got := Snapshot(in)
			var out SnapshotOutput
			if err := json.Unmarshal([]byte(got), &out); err != nil {
				t.Errorf("输入 %q 的出参不是合法 JSON：%s", in, got)
			}
		}()
	}
}

// 坏输入要说清楚哪儿不对，别默默返回空。
func TestSnapshotReportsBadInput(t *testing.T) {
	var out SnapshotOutput
	_ = json.Unmarshal([]byte(Snapshot("不是 JSON")), &out)
	if out.Error == "" {
		t.Fatal("入参不合法却没报错")
	}

	out = SnapshotOutput{}
	_ = json.Unmarshal([]byte(Snapshot(`{"now":"不是时间"}`)), &out)
	if !strings.Contains(out.Error, "RFC3339") {
		t.Fatalf("该说明时间格式：%q", out.Error)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Swift 的数组字段是非可选的，出参里冒出一个 null 就会让整个解码失败、
// 界面一片空白。这条守着所有出口——包括出错的那条路，
// 不然用户看到的是解码错误而不是真正的原因。
func TestNoNullArrays(t *testing.T) {
	cases := map[string]string{
		"正常":     Snapshot(mustJSON(t, SnapshotInput{Files: map[string]string{"a.org": "* TODO 没有标签\n"}, Days: 3})),
		"空输入":    Snapshot(`{}`),
		"入参不合法":  Snapshot("不是 JSON"),
		"时间不合法":  Snapshot(`{"now":"不是时间"}`),
		"Ask 出错": Ask("不是 JSON"),
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("出参不是合法 JSON: %v", err)
			}
			walkNull(t, "", v)
		})
	}
}

// 这几个字段在 Swift 那边是可选的，允许缺席；其余数组都不能是 null。
var nullableFields = map[string]bool{"ledger": true, "steps": true, "files": true, "error": true}

func walkNull(t *testing.T, path string, v any) {
	t.Helper()
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			if sub == nil && !nullableFields[k] {
				t.Errorf("%s/%s 是 null，Swift 解不了", path, k)
				continue
			}
			walkNull(t, path+"/"+k, sub)
		}
	case []any:
		for i, sub := range x {
			walkNull(t, fmt.Sprintf("%s[%d]", path, i), sub)
		}
	}
}

// gomobile 编出来的二进制在 iOS 沙箱里读不到系统的 zoneinfo，时区数据库是
// 靠 `_ "time/tzdata"` 打进来的。那行看着像个没用的空导入，很容易被顺手删掉——
// 删了 LoadLocation 一律失败，time.Local 退成 UTC，手机上「今天」整体差一天。
func TestApplyTimeZone(t *testing.T) {
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })

	applyTimeZone("Asia/Shanghai")
	if got := time.Local.String(); got != "Asia/Shanghai" {
		t.Fatalf("时区没设上：%s（tzdata 没打进来？）", got)
	}
	if _, off := time.Now().Zone(); off != 8*3600 {
		t.Fatalf("偏移不对：%d 秒", off)
	}

	// 空的和认不出来的都不该把已经设好的弄坏。
	for _, bad := range []string{"", "Not/AZone"} {
		applyTimeZone(bad)
		if got := time.Local.String(); got != "Asia/Shanghai" {
			t.Fatalf("applyTimeZone(%q) 把时区弄坏了：%s", bad, got)
		}
	}

	applyTimeZone("America/New_York")
	if got := time.Local.String(); got != "America/New_York" {
		t.Fatalf("换时区没生效：%s", got)
	}
}

// 列表里不该夹带详情。正文和勾选项挂在每一条上，单条会从约 180 字节涨到
// 900——五千条就是几 MB，每次刷新都要过一遍 gomobile 的桥。
func TestListPayloadStaysLight(t *testing.T) {
	org := "* TODO 有正文的任务\n  SCHEDULED: <2026-09-17 四>\n" +
		"  这是正文，不该出现在列表里。\n  - [ ] 一步\n  - [X] 两步\n"
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"a.org": org},
		Days:  3, Now: "2026-09-17T09:00:00+08:00",
	})
	raw := Snapshot(mustJSON(t, SnapshotInput{
		Files: map[string]string{"a.org": org},
		Days:  3, Now: "2026-09-17T09:00:00+08:00",
	}))
	if strings.Contains(raw, "这是正文") {
		t.Error("列表里夹带了正文")
	}
	if len(out.Todos) == 0 {
		t.Fatal("没有待办")
	}
	if !out.Todos[0].HasNotes {
		t.Error("有正文却没标 has_notes")
	}
	// 勾选项进度是轻量字段，该留着。
	if out.Todos[0].Done != 1 || out.Todos[0].Total != 2 {
		t.Errorf("进度不对：%d/%d", out.Todos[0].Done, out.Todos[0].Total)
	}
}

func TestDetailFetchesBody(t *testing.T) {
	org := "* TODO 有正文的任务\n  SCHEDULED: <2026-09-17 四 14:00-16:00>\n" +
		"  :PROPERTIES:\n  :EFFORT:   2:00\n  :END:\n" +
		"  这是正文。\n  - [ ] 一步\n  - [X] 两步\n"
	files := map[string]string{"a.org": org}

	var d DetailOutput
	if err := json.Unmarshal([]byte(Detail(mustJSON(t,
		DetailInput{Files: files, Ref: "a.org:0"}))), &d); err != nil {
		t.Fatal(err)
	}
	if d.Error != "" {
		t.Fatalf("不该出错：%s", d.Error)
	}
	if d.Notes != "这是正文。" {
		t.Errorf("正文不对：%q", d.Notes)
	}
	if d.Scheduled != "2026-09-17 14:00-16:00" {
		t.Errorf("计划时间不对：%q", d.Scheduled)
	}
	if d.Effort != "2:00" {
		t.Errorf("预估不对：%q", d.Effort)
	}
	if len(d.Boxes) != 2 || d.Boxes[0].Done || !d.Boxes[1].Done {
		t.Errorf("勾选项不对：%+v", d.Boxes)
	}

	// 烂 ref 要给话说，不能 panic 也不能返回空壳。
	for _, bad := range []string{"", "没有冒号", "a.org:999", "b.org:0", "a.org:abc"} {
		var e DetailOutput
		if err := json.Unmarshal([]byte(Detail(mustJSON(t,
			DetailInput{Files: files, Ref: bad}))), &e); err != nil {
			t.Fatalf("%q 的出参不是合法 JSON：%v", bad, err)
		}
		if e.Error == "" {
			t.Errorf("ref=%q 该报错却没报", bad)
		}
	}
}

// 待办有上限，截断了要说清楚总数——否则找不到某一条会以为是丢了。
func TestTodoLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "* TODO 任务 %d\n", i)
	}
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"a.org": b.String()}, TodoLimit: 10,
	})
	if len(out.Todos) != 10 {
		t.Errorf("该截到 10 条，实际 %d", len(out.Todos))
	}
	if out.TodosTotal != 50 {
		t.Errorf("总数该是 50，实际 %d", out.TodosTotal)
	}
}
