package chat

import (
	"strings"
	"testing"
)

// liveOnScreen 把一块 live 挂到屏幕上。
func liveOnScreen(t *testing.T) (*live, *reader) {
	t.Helper()
	l := &live{}
	r := testReader()
	scr.liveLines = l.activeLines
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	return l, r
}

// 正在流的正文画在内容区末尾，输入框仍旧钉在下面。
func TestLiveRendersAboveInput(t *testing.T) {
	h := fresh(t)
	l, _ := liveOnScreen(t)

	l.start(green + "⏺ " + reset)
	l.feed("先生，三件事：")
	lines := h.screen()

	body, status := -1, -1
	for i, s := range lines {
		if strings.Contains(s, "先生，三件事：") {
			body = i
		}
		if strings.Contains(s, "状态栏") {
			status = i
		}
	}
	if body < 0 {
		t.Fatalf("正文没画出来：\n%s", strings.Join(lines, "\n"))
	}
	if body >= status {
		t.Fatalf("正文该在输入框上面：正文第 %d 行，状态栏第 %d 行", body, status)
	}
	if !strings.Contains(lines[body], "⏺") {
		t.Fatalf("第一行该带圆点：%q", lines[body])
	}
}

// 每来一点增量就整段重渲染：表格先是半成品，分隔行到了自己变对齐。
func TestLiveTableTakesShape(t *testing.T) {
	h := fresh(t)
	l, _ := liveOnScreen(t)
	l.start("")

	l.feed("| 项目 | 金额 |\n")
	if joined := h.text(); !strings.Contains(joined, "| 项目 | 金额 |") {
		t.Fatalf("分隔行还没来，该原样显示：\n%s", joined)
	}

	l.feed("| --- | --- |\n| 房租 | 3000 |\n")
	joined := h.text()
	if strings.Contains(joined, "| 项目") {
		t.Fatalf("分隔行到了该变成表格：\n%s", joined)
	}
	if !strings.Contains(joined, "─┼─") {
		t.Fatalf("没画出表头分隔线：\n%s", joined)
	}
}

// 定格之后这段就归历史了，新一段不会再把它拉回来重画。
func TestLiveFreezeMovesToHistory(t *testing.T) {
	h := fresh(t)
	l, _ := liveOnScreen(t)

	l.start("")
	l.feed("第一段")
	l.freeze()

	if l.text.Len() != 0 || l.on {
		t.Fatal("定格之后该是干净状态")
	}
	if len(scr.history) == 0 {
		t.Fatal("定格的内容该进历史")
	}

	l.feed("第二段")
	joined := h.text()
	if !strings.Contains(joined, "第一段") || !strings.Contains(joined, "第二段") {
		t.Fatalf("两段都该在屏幕上：\n%s", joined)
	}
	if n := strings.Count(joined, "第一段"); n != 1 {
		t.Fatalf("第一段出现了 %d 次：\n%s", n, joined)
	}
}

// 正文很长时视口只显示最后一屏，输入框照样在底下。
func TestLongLiveKeepsInputVisible(t *testing.T) {
	h := fresh(t)
	l, _ := liveOnScreen(t)
	l.start("")

	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("这是第 " + strings.Repeat("x", 3) + " 行\n")
	}
	l.feed(b.String())
	lines := h.screen()

	if got := lines[len(lines)-1]; !strings.Contains(got, "状态栏") {
		t.Fatalf("正文再长，输入框也得在最底下：%q", got)
	}
}
