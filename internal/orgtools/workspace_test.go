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
	_, err := toolByName(t, ws, "update_item").
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
	if _, err := toolByName(t, ws, "update_item").
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
