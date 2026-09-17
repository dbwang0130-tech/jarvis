package orgtools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
	"time"
)

const sample = `#+TITLE: 测试

* NEXT [#A] 写毕业论文  :thesis:
  SCHEDULED: <2026-09-16 三 14:00-16:00>
** TODO 补开题报告
   DEADLINE: <2026-09-20 日>
* TODO 每周组会
  SCHEDULED: <2026-09-17 四 10:00 +1w>
`

// newWS 在临时目录里摆一个 org 文件，返回它的工作区和文件路径。
func newWS(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.org")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	coll, err := org.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewWorkspace(coll, config.Config{WorkHours: config.WorkHours{Start: 9, End: 22}}), path
}

// readBack 把磁盘上的文件读回来，用于确认改动真的落了盘。
func readBack(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 撤销是秘书改坏东西之后的最后一道防线：改过的文件要整个回到动手之前。
func TestUndoRestoresEverything(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)

	doc := ws.Coll.Doc(path)
	if doc == nil {
		t.Fatal("找不到文档")
	}
	ws.Snapshot(doc)
	doc.Lines = append(doc.Lines, "* 秘书加的一行")
	doc.Reparse()
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	ws.Record(doc, "加了一行")

	if readBack(t, path) == before {
		t.Fatal("改动没落盘，后面的撤销就没意义了")
	}
	if !ws.HasChanges() {
		t.Fatal("改过之后该记着有改动")
	}

	if err := ws.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != before {
		t.Fatalf("撤销之后文件没回到原样：\n%s", got)
	}
	if ws.HasChanges() {
		t.Fatal("撤销之后改动记录该清掉")
	}
}

// 同一个文件改很多次，撤销要回到最早那个版本，而不是上一步。
func TestUndoGoesBackToTheStart(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)
	doc := ws.Coll.Doc(path)

	for i := 0; i < 3; i++ {
		ws.Snapshot(doc) // 只有头一次留底，后面几次不该覆盖它
		doc.Lines = append(doc.Lines, "* 第", "* 二", "* 三")
		doc.Reparse()
		if err := doc.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != before {
		t.Fatalf("撤销该回到动手之前，而不是上一步：\n%s", got)
	}
}

// 没改过东西时撤销是空操作，不该报错也不该动文件。
func TestUndoWithoutChanges(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)
	if err := ws.Undo(); err != nil {
		t.Fatalf("没改过也不该报错：%v", err)
	}
	if readBack(t, path) != before {
		t.Fatal("空撤销把文件动了")
	}
}

// 条目引用是「文件名:行号」，错的格式要给出看得懂的提示。
func TestResolveByRef(t *testing.T) {
	ws, path := newWS(t)
	doc := ws.Coll.Doc(path)
	var want *org.Node
	for _, n := range doc.Nodes {
		if n.Title == "写毕业论文" {
			want = n
		}
	}
	if want == nil {
		t.Fatal("样例里没有这个条目")
	}

	gotDoc, gotNode, err := ws.resolve(refOf(doc, want), "")
	if err != nil {
		t.Fatalf("按引用找不到：%v", err)
	}
	if gotDoc != doc || gotNode != want {
		t.Fatalf("找到的不是同一个条目：%v", gotNode.Title)
	}

	if _, _, err := ws.resolve("这不是引用", ""); err == nil {
		t.Fatal("格式不对该报错")
	} else if !strings.Contains(err.Error(), "行号") {
		t.Fatalf("提示该说清楚格式：%v", err)
	}
	if _, _, err := ws.resolve("没有这个文件.org:1", ""); err == nil {
		t.Fatal("文件不存在该报错")
	}
}

// 引用找不到时可以退回按标题找。
func TestResolveByTitle(t *testing.T) {
	ws, _ := newWS(t)

	_, node, err := ws.resolve("", "每周组会")
	if err != nil {
		t.Fatalf("按标题找不到：%v", err)
	}
	if node.Title != "每周组会" {
		t.Fatalf("找到的是 %q", node.Title)
	}
	if _, _, err := ws.resolve("", "根本没有这条"); err == nil {
		t.Fatal("标题不存在该报错")
	}
}

// 改动记录要带上摘要和文件名，界面靠它告诉用户秘书动了什么。
func TestRecordKeepsSummary(t *testing.T) {
	ws, path := newWS(t)
	doc := ws.Coll.Doc(path)

	ws.Record(doc, "把 %s 排到了 %s", "写论文", "周三")
	if len(ws.Changes) != 1 {
		t.Fatalf("该记一笔，实际 %d 笔", len(ws.Changes))
	}
	c := ws.Changes[0]
	if !strings.Contains(c.Summary, "写论文") || !strings.Contains(c.Summary, "周三") {
		t.Fatalf("摘要没带上内容：%q", c.Summary)
	}
	if c.File == "" {
		t.Fatal("该记下改的是哪个文件")
	}
	if c.Time.IsZero() {
		t.Fatal("该记下什么时候改的")
	}

	ws.ResetChanges()
	if ws.HasChanges() {
		t.Fatal("清过之后不该还有改动")
	}
}

// 撞上「文件被别人改过」之后要能接着用：重读一遍，让模型重新查引用再改。
// 不重读的话手里这份永远是旧的，之后每一次保存都被拒，直到重启。
func TestSaveReloadsAfterConflict(t *testing.T) {
	ws, path := newWS(t)
	// 挑一个不循环的条目——循环任务标完成会退回 TODO 并把日期推到下一轮，
	// 断言 DONE 会白白扑空。
	ref := refTo(t, ws, "补开题报告")

	// 别人在这期间往文件末尾加了一条。
	raw := readBack(t, path)
	if err := os.WriteFile(path, []byte(raw+"* TODO 电脑那头加的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	// 这一次该被拒。
	_, _, err := toolByName(t, ws, "set_todo_state").
		Run(`{"ref":"` + ref + `","state":"DONE"}`)
	if err == nil {
		t.Fatal("文件被改过了，不该直接覆盖")
	}
	if !strings.Contains(err.Error(), "重新查") {
		t.Errorf("没说清楚接下来该怎么办：%v", err)
	}
	// 最要紧的：别人加的那条还在。
	if !strings.Contains(readBack(t, path), "电脑那头加的") {
		t.Fatal("把别人的改动冲掉了")
	}

	// 重读之后重新查引用，这一次该成。
	ref = refTo(t, ws, "补开题报告")
	if _, _, err := toolByName(t, ws, "set_todo_state").
		Run(`{"ref":"` + ref + `","state":"DONE"}`); err != nil {
		t.Fatalf("重读之后该能改：%v", err)
	}
	got := readBack(t, path)
	if !strings.Contains(got, "DONE 补开题报告") {
		t.Errorf("改动没落盘：\n%s", got)
	}
	if !strings.Contains(got, "电脑那头加的") {
		t.Errorf("重试时把别人的改动冲掉了：\n%s", got)
	}
}
