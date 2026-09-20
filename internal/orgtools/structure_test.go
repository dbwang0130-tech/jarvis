package orgtools

import (
	"strings"
	"testing"
)

// 升降级和移动改的是整棵子树的位置，写错会静默丢内容——子任务跟着父节点
// 一起没了，而文件本身还是合法的 org，不会有任何报错。

func TestDemoteAndPromote(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "move_item", map[string]any{"ref": refTo(t, ws, "每周组会"), "level_delta": 1})
	got := readBack(t, path)
	if !strings.Contains(got, "** TODO 每周组会") {
		t.Fatalf("没降一级：\n%s", got)
	}

	run(t, ws, "move_item", map[string]any{"ref": refTo(t, ws, "每周组会"), "level_delta": -1})
	got = readBack(t, path)
	if !strings.Contains(got, "* TODO 每周组会") || strings.Contains(got, "** TODO 每周组会") {
		t.Fatalf("没升回来：\n%s", got)
	}
	// 一降一升之后计划时间不该丢。
	if !strings.Contains(got, "2026-09-17") {
		t.Fatalf("计划时间在升降级里丢了：\n%s", got)
	}
}

// 带子任务的条目升降级时，子任务要跟着一起动，层级关系不能乱。
func TestDemoteKeepsChildren(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "move_item", map[string]any{"ref": refTo(t, ws, "写毕业论文"), "level_delta": 1})
	got := readBack(t, path)

	if !strings.Contains(got, "** NEXT [#A] 写毕业论文") {
		t.Fatalf("父节点没降级：\n%s", got)
	}
	if !strings.Contains(got, "*** TODO 补开题报告") {
		t.Fatalf("子任务没跟着降，层级乱了：\n%s", got)
	}
	if !strings.Contains(got, "DEADLINE: <2026-09-20") {
		t.Fatalf("子任务的截止时间丢了：\n%s", got)
	}
}

// 查询类的工具是秘书动手前必看的一步，返回的内容不对，后面全跟着错。

func TestListTodosShowsWhatMatters(t *testing.T) {
	ws, _ := newWS(t)
	out, changed := run(t, ws, "list_todos", map[string]any{})
	if changed {
		t.Error("只是看一眼，不该说动过文件")
	}
	for _, want := range []string{"写毕业论文", "补开题报告", "每周组会"} {
		if !strings.Contains(out, want) {
			t.Errorf("待办列表里少了 %q：\n%s", want, out)
		}
	}
	// 引用必须给出来——没有它，后面的修改无从下手。
	if !strings.Contains(out, "test.org:") {
		t.Errorf("没给出条目引用：\n%s", out)
	}
}

func TestGetItemShowsDetail(t *testing.T) {
	ws, _ := newWS(t)
	out, _ := run(t, ws, "get_item", map[string]any{"ref": refTo(t, ws, "写毕业论文")})

	for _, want := range []string{"写毕业论文", "NEXT", "thesis", "2026-09-16"} {
		if !strings.Contains(out, want) {
			t.Errorf("详情里少了 %q：\n%s", want, out)
		}
	}
}

func TestFindFreeSlots(t *testing.T) {
	ws, _ := newWS(t)
	out, changed := run(t, ws, "find_free_slots", map[string]any{
		"date": "2026-09-16", "minutes": 60,
	})
	if changed {
		t.Error("只是找空档，不该说动过文件")
	}
	// 那天 14:00-16:00 有事，空档要在它前后断开，而不是横跨过去。
	for _, want := range []string{"09:00-14:00", "16:00-22:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("空档没在已占用时段前后断开，少了 %q：\n%s", want, out)
		}
	}
	// 已占用的那段要说明是被什么占了，不然用户不知道能不能挪。
	if !strings.Contains(out, "写毕业论文") {
		t.Errorf("没说清楚那段时间被什么占了：\n%s", out)
	}
}

// 循环规则设上之后，下一次的日期要能算对——算错的话循环任务会一直停在
// 过去某一天，或者直接跳过一整轮。
func TestSetRepeat(t *testing.T) {
	ws, path := newWS(t)
	run(t, ws, "update_item", map[string]any{
		"ref": refTo(t, ws, "补开题报告"), "repeat": "+1m",
	})
	got := readBack(t, path)
	if !strings.Contains(got, "+1m") {
		t.Fatalf("循环规则没写上：\n%s", got)
	}
}
