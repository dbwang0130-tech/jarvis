package orgtools

import (
	"strings"
	"testing"
)

// 计时和 LOGBOOK 这几样改的是用户真实文件里的抽屉，写坏了不容易看出来
// （抽屉是折叠的），所以每条都读回磁盘核对。

func TestClockInOut(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "写毕业论文")

	run(t, ws, "clock_in", map[string]any{"ref": ref})
	got := readBack(t, path)
	if !strings.Contains(got, ":LOGBOOK:") || !strings.Contains(got, "CLOCK:") {
		t.Fatalf("计时没写进 LOGBOOK：\n%s", got)
	}
	// 还没结束的那条不该有时长。
	if strings.Contains(got, "=>") {
		t.Fatalf("刚开始就写上了时长：\n%s", got)
	}

	ref = refTo(t, ws, "写毕业论文") // 改动会让行号变
	run(t, ws, "clock_out", map[string]any{"ref": ref})
	got = readBack(t, path)
	if !strings.Contains(got, "=>") {
		t.Fatalf("结束计时没补上时长：\n%s", got)
	}
}

// 同一时间只该有一个条目在计时。两个一起走，统计出来的工时就是假的，
// 而且先开的那条 CLOCK 会一直挂着没有收尾。
//
// 这条规则在提示词里也写了一遍，但提示词是建议、代码才是保证——
// 模型忘了先 clock_out 的代价是一份查不出来的错账。
func TestClockInClosesOthers(t *testing.T) {
	ws, path := readyClock(t)

	second := refTo(t, ws, "每周组会")
	msg := run(t, ws, "clock_in", map[string]any{"ref": second})

	got := readBack(t, path)
	if n := strings.Count(got, "CLOCK:"); n != 2 {
		t.Fatalf("该有两条计时记录，实际 %d：\n%s", n, got)
	}
	// 先开的那条要被收尾（有时长），新开的那条还没有。
	if n := strings.Count(got, "=>"); n != 1 {
		t.Errorf("该正好有一条收了尾，实际 %d：\n%s", n, got)
	}
	// 回话里要说清楚顺手停了谁，不然用户以为两个都在走。
	if !strings.Contains(msg, "写毕业论文") {
		t.Errorf("没说停掉了哪个：%q", msg)
	}
}

// 没在计时的条目上喊停要报错，不能默默当成做过了。
func TestClockOutWithoutClockIn(t *testing.T) {
	ws, _ := newWS(t)
	ref := refTo(t, ws, "每周组会")
	if _, err := toolByName(t, ws, "clock_out").
		Run(`{"ref":"` + ref + `"}`); err == nil {
		t.Error("没在计时却说停掉了")
	}
}

func TestAddNote(t *testing.T) {
	ws, path := newWS(t)
	ref := refTo(t, ws, "补开题报告")

	run(t, ws, "add_note", map[string]any{
		"ref": ref, "text": "卡在数据那一段，问了老张才通",
	})

	got := readBack(t, path)
	if !strings.Contains(got, "Note taken on") {
		t.Fatalf("笔记没按 org 的格式写：\n%s", got)
	}
	if !strings.Contains(got, "问了老张") {
		t.Fatalf("笔记正文丢了：\n%s", got)
	}
	// 笔记归笔记，别把标题行弄坏。
	if !strings.Contains(got, "** TODO 补开题报告") {
		t.Fatalf("标题行被动过了：\n%s", got)
	}
}

// readyClock 起一个已经在计时的工作区。
func readyClock(t *testing.T) (*Workspace, string) {
	t.Helper()
	ws, path := newWS(t)
	run(t, ws, "clock_in", map[string]any{"ref": refTo(t, ws, "写毕业论文")})
	return ws, path
}
