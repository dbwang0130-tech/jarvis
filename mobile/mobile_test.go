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
func TestSetTimeZone(t *testing.T) {
	orig, origSet := time.Local, tzSet
	t.Cleanup(func() { time.Local, tzSet = orig, origSet })

	tzSet = false
	SetTimeZone("Asia/Shanghai")
	if got := time.Local.String(); got != "Asia/Shanghai" {
		t.Fatalf("时区没设上：%s（tzdata 没打进来？）", got)
	}
	if _, off := time.Now().Zone(); off != 8*3600 {
		t.Fatalf("偏移不对：%d 秒", off)
	}

	// 只认第一次。后面再怎么喊都不该改动 time.Local——改它的同时别的
	// goroutine 正在读，那是数据竞争，不是「顶多算错一次」。
	for _, later := range []string{"America/New_York", "UTC", "", "Not/AZone"} {
		SetTimeZone(later)
		if got := time.Local.String(); got != "Asia/Shanghai" {
			t.Fatalf("SetTimeZone(%q) 不该生效，时区变成了 %s", later, got)
		}
	}
}

// 认不出来的时区名不该把 tzSet 标记掉，否则后面真正合法的那个就设不上了。
func TestSetTimeZoneBadNameDoesNotLock(t *testing.T) {
	orig, origSet := time.Local, tzSet
	t.Cleanup(func() { time.Local, tzSet = orig, origSet })

	tzSet = false
	SetTimeZone("Not/AZone")
	if tzSet {
		t.Fatal("认不出来的名字把开关锁死了")
	}
	SetTimeZone("Asia/Tokyo")
	if got := time.Local.String(); got != "Asia/Tokyo" {
		t.Fatalf("合法的时区没设上：%s", got)
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

// 详情只给「已计时 3 小时」这个总数的话，「什么时候做的、做了几回、
// 中间状态怎么变的」全看不出来。这条守着 LOGBOOK 真的被读出来了。
func TestDetailReadsLogbook(t *testing.T) {
	doc := `* DONE 写周报
  CLOSED: [2026-09-16 三 18:20]
  :LOGBOOK:
  CLOCK: [2026-09-16 三 14:00]--[2026-09-16 三 15:30] =>  1:30
  CLOCK: [2026-09-16 三 16:00]--[2026-09-16 三 16:45] =>  0:45
  - State "DONE" from "NEXT" [2026-09-16 三 18:20]
  - Note taken on [2026-09-16 三 17:00] \\
    卡在数据那一段，问了老张才通
  :END:
  正文留着。
`
	var d DetailOutput
	if err := json.Unmarshal([]byte(Detail(mustJSON(t,
		DetailInput{Files: map[string]string{"a.org": doc}, Ref: "a.org:0"}))), &d); err != nil {
		t.Fatal(err)
	}
	if d.Error != "" {
		t.Fatalf("不该出错：%s", d.Error)
	}
	if d.Clocked != 135 {
		t.Errorf("总时长该是 135 分钟，实际 %d", d.Clocked)
	}
	if len(d.Log) != 4 {
		t.Fatalf("该有 4 条记录（两段计时 + 一次状态变更 + 一条笔记），实际 %d：%+v",
			len(d.Log), d.Log)
	}

	kinds := map[string]int{}
	for _, l := range d.Log {
		kinds[l.Kind]++
	}
	if kinds["clock"] != 2 || kinds["state"] != 1 || kinds["note"] != 1 {
		t.Errorf("种类不对：%v", kinds)
	}

	// 笔记正文在续行里，解析器只留了头一行——不捞回来就是一条空笔记。
	var note LogItem
	for _, l := range d.Log {
		if l.Kind == "note" {
			note = l
		}
	}
	if !strings.Contains(note.Text, "问了老张") {
		t.Errorf("笔记正文没捞回来：%q", note.Text)
	}
	if strings.Contains(note.Text, "Note taken on") {
		t.Errorf("笔记里混进了 org 的样板字：%q", note.Text)
	}

	// 新的排前面，否则一眼看过去最上面是最早的事。
	for i := 1; i < len(d.Log); i++ {
		if d.Log[i-1].When != "" && d.Log[i].When != "" && d.Log[i-1].When < d.Log[i].When {
			t.Errorf("顺序不对，%q 排在了 %q 前面", d.Log[i-1].When, d.Log[i].When)
		}
	}

	// 状态变更要说清楚从哪变到哪。
	var state LogItem
	for _, l := range d.Log {
		if l.Kind == "state" {
			state = l
		}
	}
	if !strings.Contains(state.Text, "NEXT") || !strings.Contains(state.Text, "DONE") {
		t.Errorf("状态变更没说清楚：%q", state.Text)
	}
}

// 待办要能按时间分组，就得知道每条归到哪一天、还剩几天。
// 不给的话界面只能摊成一长条，「今天该干的」和「下个月才到期的」混在一起。
func TestTodosCarryDue(t *testing.T) {
	org := `* TODO 明天截止
  DEADLINE: <2026-09-18 五>
* TODO 早就过期了
  DEADLINE: <2026-09-01 二>
* TODO 只是计划
  SCHEDULED: <2026-09-20 日>
* TODO 没排期
`
	out := snapshot(t, SnapshotInput{
		Files: map[string]string{"a.org": org},
		Now:   "2026-09-17T09:00:00+08:00", TZ: "Asia/Shanghai",
	})

	byTitle := map[string]Item{}
	for _, it := range out.Todos {
		byTitle[it.Title] = it
	}

	if got := byTitle["明天截止"]; got.Due != "2026-09-18" || got.DueKind != "deadline" {
		t.Errorf("截止那条不对：due=%q kind=%q", got.Due, got.DueKind)
	}
	if got := byTitle["只是计划"]; got.Due != "2026-09-20" || got.DueKind != "scheduled" {
		t.Errorf("计划那条不对：due=%q kind=%q", got.Due, got.DueKind)
	}
	// 没排期的不能编一个日期出来——界面靠空串把它们归到「没排期」。
	if got := byTitle["没排期"]; got.Due != "" || got.DueKind != "" {
		t.Errorf("没排期的却带了日期：due=%q kind=%q", got.Due, got.DueKind)
	}
	// 过期的天数要是负的，界面靠这个把它挑进「逾期」。
	if got := byTitle["早就过期了"]; got.DueIn >= 0 {
		t.Errorf("过期的剩余天数该是负的，实际 %d", got.DueIn)
	}
}

// 流水原来只报「最大的那笔分录」，收入支出都是正数，一眼分不出方向；
// 一笔账拆成好几项（午饭 35 + 饮料 8）时只报 35 也是错的。
func TestRecentTransactions(t *testing.T) {
	book := `2026/09/10 发工资
    Assets:Bank:招行            18000.00
    Income:Salary

2026/09/12 午饭
    Expenses:Food:餐饮             35.00
    Expenses:Food:饮料              8.00
    Assets:Bank:招行

2026/09/13 转去余额宝
    Assets:Fund:余额宝           5000.00
    Assets:Bank:招行
`
	out := snapshot(t, SnapshotInput{Ledger: book, Now: "2026-09-17T09:00:00+08:00"})
	if out.Ledger == nil {
		t.Fatal("账本没解析出来")
	}

	byPayee := map[string]TxnBrief{}
	for _, tx := range out.Ledger.Recent {
		byPayee[tx.Payee] = tx
	}

	pay := byPayee["发工资"]
	if pay.Kind != "income" || pay.Amount != 18000 {
		t.Errorf("进账那笔不对：kind=%q amount=%.2f", pay.Kind, pay.Amount)
	}
	if pay.Category != "Salary" {
		t.Errorf("没说清楚哪来的：%q", pay.Category)
	}

	// 拆成两项的要合计，而且是负的。
	lunch := byPayee["午饭"]
	if lunch.Kind != "expense" || lunch.Amount != -43 {
		t.Errorf("花钱那笔不对：kind=%q amount=%.2f（该是 -43）", lunch.Kind, lunch.Amount)
	}

	// 钱在自己账户间挪，既不是花也不是挣——混进支出会把当天的账撑得很难看。
	move := byPayee["转去余额宝"]
	if move.Kind != "transfer" {
		t.Errorf("转账被当成了 %q", move.Kind)
	}
}
