package chat

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

// captureTerm 把终端输出接到缓冲区上。
func captureTerm(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := termOut
	termOut = buf
	t.Cleanup(func() { termOut = old })
	return buf
}

// 只画变化的行，所以模拟屏得一直留着累积，不能每次新建。
type harness struct {
	buf *bytes.Buffer
	sim *fakeScreen
}

// fresh 把 screen 恢复成干净状态并挂上一块模拟屏，测完还原。
func fresh(t *testing.T) *harness {
	t.Helper()
	h := &harness{buf: captureTerm(t), sim: &fakeScreen{}}
	reset := func() {
		scr.history, scr.offset, scr.lastLen, scr.prev = nil, 0, 0, nil
		scr.liveLines, scr.confirmLines, scr.inputLines = nil, nil, nil
		scr.hintLines, scr.statusFn, scr.content = nil, nil, nil
		scr.spinning, scr.alt, scr.mouse = false, false, false
	}
	reset()
	t.Cleanup(reset)
	return h
}

// screen 把攒下的输出补画到模拟屏上，返回整屏的每一行。
func (h *harness) screen() []string {
	h.sim.write(h.buf.String())
	h.buf.Reset()
	return h.sim.screenText(height())
}

// text 是整屏拼成的一大段，方便做包含判断。
func (h *harness) text() string { return strings.Join(h.screen(), "\n") }

// testReader 造一个不碰真终端的输入框。
func testReader() *reader {
	r := &reader{ch: make(chan event, 8), histIdx: -1}
	return r
}

func TestInputBoxPinnedToBottom(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	r.insert("我打的字")

	scr.refresh()
	lines := h.screen()

	// 最后一行是状态栏，输入框紧挨其上——不管内容多少都钉在这儿。
	if got := lines[len(lines)-1]; !strings.Contains(got, "状态栏") {
		t.Fatalf("最后一行该是状态栏，实际是 %q", got)
	}
	if got := lines[len(lines)-3]; !strings.Contains(got, "我打的字") {
		t.Fatalf("倒数第三行该是输入行，实际是 %q", got)
	}
}

// 内容少的时候输入框也得在最底下，不能浮在中间。
func TestInputStaysAtBottomWithLittleContent(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }

	out("就一行内容\n")
	lines := h.screen()

	if got := lines[len(lines)-1]; !strings.Contains(got, "状态栏") {
		t.Fatalf("最后一行该是状态栏：%q", got)
	}
	// 那一行内容应当贴着输入框上方，而不是待在屏幕顶上。
	found := -1
	for i, l := range lines {
		if strings.Contains(l, "就一行内容") {
			found = i
		}
	}
	if found < 0 {
		t.Fatal("内容没画出来")
	}
	// 底部区是「空行 + 框三行 + 状态栏」，内容该紧挨在它上面。
	if gap := len(lines) - 1 - found; gap > 6 {
		t.Fatalf("内容在第 %d 行，离状态栏隔了 %d 行，太远了（共 %d 行）",
			found, gap, len(lines))
	}
}

// 内容超过一屏时，看到的是最后那一屏。
func TestViewportShowsTail(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }

	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	lines := h.screen()

	if !strings.Contains(strings.Join(lines, "\n"), "第 100 行") {
		t.Fatalf("最新的一行没显示出来：\n%s", strings.Join(lines, "\n"))
	}
	if strings.Contains(strings.Join(lines, "\n"), "第 1 行\n") {
		t.Fatal("第 1 行早该滚出去了")
	}
}

// 往上滚能看到旧内容，滚回底部又回到最新。
func TestScrollUpAndBack(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	h.screen()

	scr.scroll(30)
	lines := h.screen()
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "第 100 行") {
		t.Fatalf("往上滚了 30 行还能看到最新的一行：\n%s", joined)
	}
	if !strings.Contains(joined, "第 70 行") {
		t.Fatalf("往上滚 30 行该看到第 70 行附近：\n%s", joined)
	}
	// 输入框还在底下。
	if got := lines[len(lines)-1]; !strings.Contains(got, "状态栏") {
		t.Fatalf("滚动时输入框不该跟着跑：%q", got)
	}

	scr.toBottom()
	lines = h.screen()
	if !strings.Contains(strings.Join(lines, "\n"), "第 100 行") {
		t.Fatal("回到底部却看不到最新内容")
	}
}

// 正翻着历史的时候来了新内容，眼前这段不该被推走。
func TestScrollPositionSurvivesNewOutput(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	scr.scroll(30)
	if !strings.Contains(h.text(), "第 70 行") {
		t.Fatal("没找到锚点")
	}

	out("新来的一行\n")
	out("又来一行\n")
	lines := h.screen()

	if !strings.Contains(strings.Join(lines, "\n"), "第 70 行") {
		t.Fatalf("新内容把正在看的那段推走了：\n%s", strings.Join(lines, "\n"))
	}
}

// 转圈那行排在输入框上面，收起来之后不留痕。
func TestSpinnerSitsAboveInput(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }

	scr.startSpin()
	lines := h.screen()
	spin, input := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "esc 打断") {
			spin = i
		}
		if strings.Contains(l, "状态栏") {
			input = i
		}
	}
	if spin < 0 {
		t.Fatalf("转圈没画出来：\n%s", strings.Join(lines, "\n"))
	}
	if spin > input {
		t.Fatalf("转圈那行该在输入框上面：转圈第 %d 行，状态栏第 %d 行", spin, input)
	}

	scr.stopSpin()
	if joined := h.text(); strings.Contains(joined, "esc 打断") {
		t.Fatalf("收起来之后还留着：\n%s", joined)
	}
}

// 确认框顶掉输入框，选完之后定格进历史、输入框回来。
func TestConfirmBoxReplacesInputBox(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	r.insert("写了一半的话")

	c := newConfirm(&agent.Pending{Title: "清理 12 封广告", Risk: agent.RiskReversible})
	c.show()
	joined := h.text()

	if strings.Contains(joined, "写了一半的话") {
		t.Fatalf("确认框亮着时不该还显示输入框：\n%s", joined)
	}
	if !strings.Contains(joined, "清理 12 封广告") || !strings.Contains(joined, "状态栏") {
		t.Fatalf("确认框或状态栏没画出来：\n%s", joined)
	}

	c.done()
	joined = h.text()
	if !strings.Contains(joined, "清理 12 封广告") {
		t.Fatalf("确认框该定格进历史：\n%s", joined)
	}
	if !strings.Contains(joined, "写了一半的话") {
		t.Fatalf("选完之后输入框该回来，字也还在：\n%s", joined)
	}
	if n := strings.Count(joined, "清理 12 封广告"); n != 1 {
		t.Fatalf("确认框出现了 %d 次：\n%s", n, joined)
	}
}

// 进出 alt screen 的序列要成对，退出时把历史回放出来。
func TestEnterLeaveAltScreen(t *testing.T) {
	h := fresh(t)
	scr.enter(true)
	got := h.buf.String()
	for _, want := range []string{"\033[?1049h", "\033[2J", "\033[?1000h", "\033[?1006h"} {
		if !strings.Contains(got, want) {
			t.Fatalf("进 alt screen 少了 %q：%q", want, got)
		}
	}

	h.buf.Reset()
	scr.history = []string{"这趟说过的话"}
	scr.leave()
	got = h.buf.String()
	for _, want := range []string{"\033[?1000l", "\033[?1049l", "这趟说过的话"} {
		if !strings.Contains(got, want) {
			t.Fatalf("退出时少了 %q：%q", want, got)
		}
	}
	if strings.Index(got, "\033[?1049l") > strings.Index(got, "这趟说过的话") {
		t.Fatal("该先退回主屏幕再回放历史")
	}
}

// 每一帧都得包在同步输出里，不然会看到画到一半的样子。
func TestFrameIsSynchronized(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	scr.refresh()

	got := h.buf.String()
	if !strings.HasPrefix(got, "\033[?2026h") {
		t.Fatalf("一帧该以同步开始：%q", got[:12])
	}
	if !strings.HasSuffix(got, "\033[?2026l") {
		t.Fatalf("一帧该以同步结束：%q", got[len(got)-12:])
	}
}

// 窗口尺寸一变，上一帧就不作数了，必须整屏重画。
func TestInvalidateRedrawsEverything(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	out("一些内容\n")
	h.screen()

	// 再画一帧：什么都没变，diff 之后应该几乎没有输出。
	scr.refresh()
	quiet := len(h.buf.String())
	h.screen()

	scr.invalidate()
	loud := len(h.buf.String())

	if loud <= quiet {
		t.Fatalf("作废之后该整屏重画：没变时输出 %d 字节，作废后 %d 字节", quiet, loud)
	}
	if !strings.Contains(h.text(), "一些内容") {
		t.Fatal("整屏重画之后内容丢了")
	}
}

// 静止不动时不该有输出——这是 diff 渲染的意义所在。
func TestIdleFrameIsCheap(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	out("一些内容\n")
	h.screen()

	scr.refresh()
	got := h.buf.String()
	// 只剩同步输出的开关和光标定位，不该再重画任何一行。
	if strings.Contains(got, "\033[K") {
		t.Fatalf("什么都没变却还在擦行重画：%q", got)
	}
}

// 别的视图是整屏换掉内容区，不往对话历史里灌东西——
// 切来切去之后聊天记录得跟没切过一样。
func TestViewReplacesContentWithoutTouchingHistory(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	out("> /accounts\n")
	out("  · 还没有邮箱账号\n")
	before := append([]string(nil), scr.history...)

	scr.setContent([]string{"── 日程 ──", "", "09-15 周二 今天"})
	got := h.text()
	if !strings.Contains(got, "09-15 周二 今天") {
		t.Fatalf("没换成日程：\n%s", got)
	}
	if strings.Contains(got, "还没有邮箱账号") {
		t.Fatalf("切过去之后不该还看到对话：\n%s", got)
	}
	if len(scr.history) != len(before) {
		t.Fatalf("切视图不该动历史：之前 %d 行，现在 %d 行", len(before), len(scr.history))
	}

	scr.setContent(nil)
	got = h.text()
	if !strings.Contains(got, "还没有邮箱账号") {
		t.Fatalf("切回来该看到原来的对话：\n%s", got)
	}
	if strings.Contains(got, "09-15 周二 今天") {
		t.Fatalf("切回来之后日程不该还留在屏幕上：\n%s", got)
	}
}

// 视图是从上往下读的，内容不满一屏时贴着顶；聊天相反，贴着底。
func TestViewSticksToTopChatSticksToBottom(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()

	scr.setContent([]string{"第一行", "第二行"})
	lines := h.screen()
	if !strings.Contains(lines[0], "第一行") {
		t.Fatalf("视图该从第一行开始：\n%s", strings.Join(lines, "\n"))
	}

	scr.setContent(nil)
	out("聊天第一句\n")
	lines = h.screen()
	last := -1
	for i, l := range lines {
		if strings.Contains(l, "聊天第一句") {
			last = i
		}
	}
	if last < 0 {
		t.Fatal("聊天内容没画出来")
	}
	if gap := len(lines) - 1 - last; gap > 6 {
		t.Fatalf("聊天该贴着底，现在离状态栏还隔 %d 行", gap)
	}
}

// 鼠标上报和拖选复制只能要一个，/mouse 就是在这两者之间切。
func TestMouseToggle(t *testing.T) {
	h := fresh(t)

	scr.enter(true)
	if !strings.Contains(h.buf.String(), "\033[?1000h") {
		t.Fatal("开着鼠标进去时该开上报")
	}
	if !scr.MouseOn() {
		t.Fatal("状态该是开着")
	}

	h.buf.Reset()
	scr.SetMouse(false)
	if !strings.Contains(h.buf.String(), "\033[?1000l") {
		t.Fatalf("关的时候该撤掉上报：%q", h.buf.String())
	}
	if scr.MouseOn() {
		t.Fatal("状态该是关着")
	}

	// 重复设成同一个值不该再发一遍序列。
	h.buf.Reset()
	scr.SetMouse(false)
	if got := h.buf.String(); got != "" {
		t.Fatalf("没变化就不该发东西：%q", got)
	}

	// 退出时无论如何都要撤干净，不然留给终端一个开着上报的烂摊子。
	scr.SetMouse(true)
	h.buf.Reset()
	scr.leave()
	if !strings.Contains(h.buf.String(), "\033[?1000l") {
		t.Fatal("退出时该撤掉鼠标上报")
	}
}
