package orgtools

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/org"
)

// 这些用例走的是秘书真正会走的那条路：拿工具名 + JSON 参数调 Run，
// 然后把文件读回来看写成了什么。改坏用户的 org 文件是这个包最大的风险。

// toolByName 从技能里取出一个工具。
func toolByName(t *testing.T, ws *Workspace, name string) agent.Tool {
	t.Helper()
	for _, tl := range NewSkill(ws).Tools() {
		if tl.Spec().Name == name {
			return tl
		}
	}
	t.Fatalf("没有名为 %s 的工具", name)
	return nil
}

// run 调用工具并返回它说了什么。
func run(t *testing.T, ws *Workspace, name string, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	out, changed, err := toolByName(t, ws, name).Run(string(raw))
	if err != nil {
		t.Fatalf("%s 执行失败：%v", name, err)
	}
	return out, changed
}

// refTo 找出某个标题的条目引用。
func refTo(t *testing.T, ws *Workspace, title string) string {
	t.Helper()
	for _, d := range ws.Coll.Docs {
		for _, n := range d.Nodes {
			if n.Title == title {
				return refOf(d, n)
			}
		}
	}
	t.Fatalf("找不到标题为 %q 的条目", title)
	return ""
}

func TestSetTodoStateWritesBack(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "补开题报告")

	if _, changed := run(t, ws, "set_todo_state", map[string]any{
		"ref": ref, "title": "补开题报告", "state": "DONE",
	}); !changed {
		t.Fatal("改了状态却说没动过文件")
	}

	got := readBack(t, path)
	if !strings.Contains(got, "** DONE 补开题报告") {
		t.Fatalf("状态没写回去：\n%s", got)
	}
	// 标记完成要留下 CLOSED 时间戳，这是 org 的惯例。
	if !strings.Contains(got, "CLOSED:") {
		t.Fatalf("标记完成该记下完成时间：\n%s", got)
	}
}

func TestScheduleItem(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "每周组会")

	run(t, ws, "schedule_item", map[string]any{
		"ref": ref, "title": "每周组会", "time": "2026-10-01 09:30-11:00",
	})

	got := readBack(t, path)
	if !strings.Contains(got, "<2026-10-01") || !strings.Contains(got, "09:30-11:00") {
		t.Fatalf("计划时间没写对：\n%s", got)
	}
}

// 留空表示取消，不能留下半截的 SCHEDULED 行。
func TestScheduleItemClearing(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "写毕业论文")

	run(t, ws, "schedule_item", map[string]any{
		"ref": ref, "title": "写毕业论文", "time": "",
	})

	got := readBack(t, path)
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "SCHEDULED:") && strings.Contains(line, "2026-09-16") {
			t.Fatalf("计划没被取消掉：%q", line)
		}
	}
}

func TestSetPriority(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "每周组会")

	run(t, ws, "set_priority", map[string]any{
		"ref": ref, "title": "每周组会", "priority": "B",
	})
	if got := readBack(t, path); !strings.Contains(got, "[#B] 每周组会") {
		t.Fatalf("优先级没写对：\n%s", got)
	}

	// 留空是去掉优先级。
	run(t, ws, "set_priority", map[string]any{
		"ref": refTo(t, ws, "每周组会"), "title": "每周组会", "priority": "",
	})
	if got := readBack(t, path); strings.Contains(got, "[#B] 每周组会") {
		t.Fatalf("优先级没去掉：\n%s", got)
	}
}

func TestCreateItem(t *testing.T) {
	ws, path := newWS(t)

	out, changed := run(t, ws, "create_item", map[string]any{
		"title": "买菜", "state": "TODO", "priority": "C",
		"scheduled": "2026-10-02 18:00", "tags": []string{"生活"},
		"file": "test.org",
	})
	if !changed {
		t.Fatal("新建条目却说没动过文件")
	}
	if !strings.Contains(out, "买菜") {
		t.Fatalf("回话里该提到新建的条目：%q", out)
	}

	got := readBack(t, path)
	if !strings.Contains(got, "TODO [#C] 买菜") {
		t.Fatalf("新条目没写对：\n%s", got)
	}
	if !strings.Contains(got, ":生活:") {
		t.Fatalf("标签没写上：\n%s", got)
	}
	if !strings.Contains(got, "<2026-10-02") {
		t.Fatalf("计划时间没写上：\n%s", got)
	}
}

// 删除是破坏性动作：必须连子条目一起删干净，且不能误伤旁边的条目。
func TestDeleteItemTakesSubtree(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "写毕业论文")

	run(t, ws, "delete_item", map[string]any{"ref": ref, "title": "写毕业论文"})

	got := readBack(t, path)
	if strings.Contains(got, "写毕业论文") {
		t.Fatalf("条目没删掉：\n%s", got)
	}
	if strings.Contains(got, "补开题报告") {
		t.Fatalf("子条目该跟着一起删：\n%s", got)
	}
	if !strings.Contains(got, "每周组会") {
		t.Fatalf("旁边的条目被误删了：\n%s", got)
	}
}

// 标题对不上就不能动手——防止行号漂移之后删错条目。
func TestDeleteItemChecksTitle(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)
	ref := refTo(t, ws, "每周组会")

	raw, _ := json.Marshal(map[string]any{"ref": ref, "title": "对不上的标题"})
	_, _, err := toolByName(t, ws, "delete_item").Run(string(raw))
	if err == nil {
		t.Fatal("标题对不上还是删了")
	}
	if readBack(t, path) != before {
		t.Fatal("出错了却把文件改了")
	}
}

// 改完之后撤销要能整个退回去——这是秘书改坏东西时的兜底。
func TestToolChangesAreUndoable(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)

	run(t, ws, "set_todo_state", map[string]any{
		"ref": refTo(t, ws, "每周组会"), "title": "每周组会", "state": "DONE",
	})
	run(t, ws, "create_item", map[string]any{"title": "临时加的", "file": "test.org"})

	if readBack(t, path) == before {
		t.Fatal("改动没落盘")
	}
	if err := ws.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := readBack(t, path); got != before {
		t.Fatalf("撤销之后没回到原样：\n%s", got)
	}
}

// 只读的查询工具不该声明自己改过文件，否则界面会白刷一遍。
func TestReadOnlyToolsReportNoChange(t *testing.T) {
	ws, _ := newWS(t)
	for _, name := range []string{"list_agenda", "list_todos", "get_item", "find_free_slots"} {
		tl := toolByName(t, ws, name)
		ro, ok := tl.(interface{ ReadOnly() bool })
		if !ok || !ro.ReadOnly() {
			t.Errorf("%s 该标成只读", name)
		}
		if tl.Risk() != agent.RiskNone {
			t.Errorf("%s 是查询，不该有风险等级", name)
		}
	}
}

// 引用失效时要给出能让模型自己纠正的提示，而不是默默改错条目。
func TestStaleRefIsRejected(t *testing.T) {
	ws, _ := newWS(t)
	raw, _ := json.Marshal(map[string]any{
		"ref": "test.org:999", "title": "每周组会", "state": "DONE",
	})
	out, _, err := toolByName(t, ws, "set_todo_state").Run(string(raw))
	if err == nil && !strings.Contains(out, "每周组会") {
		t.Fatalf("行号不存在时该报错或按标题纠正，得到：%q", out)
	}
	if err != nil && !strings.Contains(err.Error(), "test.org") {
		t.Fatalf("提示该带上是哪个引用出了问题：%v", err)
	}
}

// 文档里的条目行号在改动后会漂移，refOf 必须跟着当前状态走。
func TestRefFollowsTheNode(t *testing.T) {
	ws, _ := newWS(t)
	var doc *org.Document
	for _, d := range ws.Coll.Docs {
		doc = d
	}
	n := doc.NodeAt(doc.Nodes[0].Line)
	if n == nil {
		t.Fatal("取不到第一个条目")
	}
	if got, want := refOf(doc, n), fmt.Sprintf("test.org:%d", n.Line); got != want {
		t.Fatalf("引用是 %q，该是 %q", got, want)
	}
}
