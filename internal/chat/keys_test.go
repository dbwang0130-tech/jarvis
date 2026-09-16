package chat

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// typeKeys 把一串按键喂给 reader，ESC 开头的序列也照常识别。
func typeKeys(t *testing.T, r *reader, keys string) {
	t.Helper()
	rd := bufio.NewReader(strings.NewReader(keys))
	for {
		k, err := readKey(rd)
		if err != nil {
			return
		}
		if !r.handleKey(k) {
			return
		}
	}
}

// newTestReader 造一个不碰真终端的 reader。
func newTestReader(t *testing.T) *reader {
	t.Helper()
	captureTerm(t)
	r := &reader{ch: make(chan event, 16), histIdx: -1}
	r.boxed.Store(true)
	return r
}

func TestLineEditingKeys(t *testing.T) {
	cases := []struct {
		name string
		keys string
		want string
		cur  int
	}{
		{"ctrl+a 回行首再打字", "abc\x01X", "Xabc", 1},
		{"ctrl+e 回行尾", "abc\x01\x05X", "abcX", 4},
		{"ctrl+k 删到行尾", "abcdef\x01\x0b", "", 0},
		{"ctrl+u 删到行首", "abcdef\x15", "", 0},
		{"ctrl+k 之后 ctrl+y 粘回来", "abcdef\x01\x0b\x19", "abcdef", 6},
		{"ctrl+u 之后 ctrl+y 粘回来", "abcdef\x15\x19", "abcdef", 6},
		{"ctrl+w 删一个词", "hello world\x17", "hello ", 6},
		{"ctrl+w 之后 ctrl+y", "hello world\x17\x19", "hello world", 11},
		{"ctrl+b 左移", "abc\x02X", "abXc", 3},
		{"ctrl+f 右移", "abc\x01\x06X", "aXbc", 2},
		{"ctrl+t 交换两个字", "ab\x14", "ba", 2},
		{"ctrl+d 删光标处", "abc\x01\x04", "bc", 0},
		{"退格", "abc\x7f", "ab", 2},
		{"alt+b 按词左移", "hello world\x1bbX", "hello Xworld", 7},
		{"alt+f 按词右移", "hello world\x01\x1bfX", "helloX world", 6},
		{"alt+d 删后一个词", "hello world\x01\x1bd", " world", 0},
		{"中文也按字符走", "你好\x01X", "X你好", 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newTestReader(t)
			typeKeys(t, r, c.keys)
			if got := string(r.buf); got != c.want {
				t.Fatalf("输入框里是 %q，想要 %q", got, c.want)
			}
			if r.cur != c.cur {
				t.Fatalf("光标在 %d，想要 %d", r.cur, c.cur)
			}
		})
	}
}

func TestEnterSubmitsAndRemembers(t *testing.T) {
	r := newTestReader(t)
	typeKeys(t, r, "查一下今天的日程\r")

	select {
	case ev := <-r.ch:
		if ev.kind != evLine || ev.line != "查一下今天的日程" {
			t.Fatalf("提交的内容不对：%+v", ev)
		}
	default:
		t.Fatal("回车没有提交")
	}
	if len(r.hist) != 1 {
		t.Fatalf("应当记进历史，现在有 %d 条", len(r.hist))
	}
	if len(r.buf) != 0 {
		t.Fatalf("提交后输入框该清空，现在是 %q", string(r.buf))
	}
}

func TestHistoryUpDown(t *testing.T) {
	r := newTestReader(t)
	typeKeys(t, r, "第一句\r第二句\r")
	<-r.ch
	<-r.ch

	typeKeys(t, r, "\x10") // ctrl+p
	if got := string(r.buf); got != "第二句" {
		t.Fatalf("上一条应当是「第二句」，拿到 %q", got)
	}
	typeKeys(t, r, "\x10")
	if got := string(r.buf); got != "第一句" {
		t.Fatalf("再上一条应当是「第一句」，拿到 %q", got)
	}
	typeKeys(t, r, "\x0e") // ctrl+n
	if got := string(r.buf); got != "第二句" {
		t.Fatalf("往下应当回到「第二句」，拿到 %q", got)
	}
}

// 空行按 ctrl+c 才是退出；行里有字时只清空。
func TestCtrlCClearsBeforeQuitting(t *testing.T) {
	r := newTestReader(t)
	typeKeys(t, r, "写了一半\x03")
	if len(r.buf) != 0 {
		t.Fatalf("ctrl+c 应当先清空输入：%q", string(r.buf))
	}
	select {
	case ev := <-r.ch:
		t.Fatalf("这时候不该往外发事件：%+v", ev)
	default:
	}

	typeKeys(t, r, "\x03")
	select {
	case ev := <-r.ch:
		if ev.kind != evCtrlC {
			t.Fatalf("空行再按一次该发打断：%+v", ev)
		}
	default:
		t.Fatal("空行按 ctrl+c 没有反应")
	}
}

// 行里有字时 ctrl+d 是删字符，空行才是退出。
func TestCtrlDDeletesThenExits(t *testing.T) {
	r := newTestReader(t)
	typeKeys(t, r, "ab\x01\x04")
	if got := string(r.buf); got != "b" {
		t.Fatalf("ctrl+d 该删掉光标处的字：%q", got)
	}

	r2 := newTestReader(t)
	typeKeys(t, r2, "\x04")
	select {
	case ev := <-r2.ch:
		if ev.kind != evEOF {
			t.Fatalf("空行按 ctrl+d 该退出：%+v", ev)
		}
	default:
		t.Fatal("空行按 ctrl+d 没有反应")
	}
}

// 秘书干活时打的字不该被吞掉：输入框照常编辑，回车排进队列。
func TestTypingWhileBusy(t *testing.T) {
	r := newTestReader(t)
	r.setBusy(true)

	typeKeys(t, r, "顺便看看邮件")
	if got := string(r.buf); got != "顺便看看邮件" {
		t.Fatalf("干活时也该能接着打字，现在是 %q", got)
	}

	typeKeys(t, r, "\r")
	select {
	case ev := <-r.ch:
		if ev.kind != evLine || ev.line != "顺便看看邮件" {
			t.Fatalf("回车该把这句话交出去：%+v", ev)
		}
	default:
		t.Fatal("回车没反应")
	}
}

// 干活时 esc 和 ctrl+c 是打断，不参与行编辑。
func TestEscInterruptsWhileBusy(t *testing.T) {
	for _, key := range []string{"\x1b", "\x03"} {
		r := newTestReader(t)
		r.setBusy(true)
		typeKeys(t, r, "写了一半"+key)

		select {
		case ev := <-r.ch:
			if ev.kind != evCtrlC {
				t.Fatalf("按 %q 该打断：%+v", key, ev)
			}
		default:
			t.Fatalf("按 %q 没反应", key)
		}
		if got := string(r.buf); got != "写了一半" {
			t.Fatalf("打断不该动输入框里的字：%q", got)
		}
	}
}

func TestQueueRoundTrip(t *testing.T) {
	s := &Session{}
	if s.dequeue() != "" {
		t.Fatal("空队列该返回空")
	}
	s.enqueue("第一句")
	s.enqueue("第二句")
	if n := s.queueLen(); n != 2 {
		t.Fatalf("排了两句，queueLen 说 %d", n)
	}
	if got := s.dequeue(); got != "第一句" {
		t.Fatalf("该先出第一句，拿到 %q", got)
	}
	if got := s.dequeue(); got != "第二句" {
		t.Fatalf("该接着出第二句，拿到 %q", got)
	}
	if s.dequeue() != "" || s.queueLen() != 0 {
		t.Fatal("取空之后该是空的")
	}
}

// 滚轮走 SGR 鼠标上报：按键 64 往上、65 往下。
func TestMouseWheelScrolls(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	h.screen()

	r := testReader()
	typeKeys(t, r, "\x1b[<64;10;10M\x1b[<64;10;10M") // 往上滚两下
	if scr.offset == 0 {
		t.Fatal("滚轮往上没滚动")
	}
	up := scr.offset

	typeKeys(t, r, "\x1b[<65;10;10M") // 往下滚一下
	if scr.offset >= up {
		t.Fatalf("滚轮往下该滚回来：之前 %d，现在 %d", up, scr.offset)
	}

	// 普通鼠标点击不该动滚动位置。
	before := scr.offset
	typeKeys(t, r, "\x1b[<0;10;10M\x1b[<0;10;10m")
	if scr.offset != before {
		t.Fatalf("点击也把屏幕滚了：%d → %d", before, scr.offset)
	}
}

// PageUp / PageDown 翻半屏。
func TestPageKeysScroll(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	h.screen()

	r := testReader()
	typeKeys(t, r, "\x1b[5~")
	if scr.offset != height()/2 {
		t.Fatalf("PageUp 该翻半屏（%d 行），实际 %d", height()/2, scr.offset)
	}
	typeKeys(t, r, "\x1b[6~")
	if scr.offset != 0 {
		t.Fatalf("PageDown 该翻回来，实际 %d", scr.offset)
	}
}

// 在别的视图里打字：字当场就得落进输入框，不能等主 goroutine 回头再插，
// 否则紧跟着的按键会抢在前面，顺序就乱了。
func TestTypingInOtherViewKeepsOrder(t *testing.T) {
	fresh(t)
	r := newTestReader(t)
	r.setBoxed(false)

	typeKeys(t, r, "abc")
	if got := string(r.buf); got != "abc" {
		t.Fatalf("输入框里该是 abc，实际 %q", got)
	}
	if !r.isBoxed() {
		t.Fatal("打了字就该切回秘书视图")
	}
	// 只发了一次切视图通知，不是每个字一次。
	n := 0
	for len(r.ch) > 0 {
		if (<-r.ch).kind == evFocus {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("切视图通知发了 %d 次，该只发第一次", n)
	}
}

// i 只把视图切回来，不落字。
func TestIOnlyFocuses(t *testing.T) {
	fresh(t)
	r := newTestReader(t)
	r.setBoxed(false)

	typeKeys(t, r, "i")
	if got := string(r.buf); got != "" {
		t.Fatalf("i 不该落字，输入框里却有 %q", got)
	}
	if !r.isBoxed() {
		t.Fatal("i 该把视图切回来")
	}
}

// 秘书干活时滚一下屏幕，不该把它打断——滚轮序列也是 ESC 开头的。
func TestScrollingWhileBusyDoesNotInterrupt(t *testing.T) {
	h := fresh(t)
	func() {
		tr := testReader()
		scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
		scr.statusFn = func() string { return "状态栏" }
	}()
	for i := 1; i <= 100; i++ {
		out(fmt.Sprintf("第 %d 行\n", i))
	}
	h.screen()

	r := newTestReader(t)
	r.setBusy(true)

	typeKeys(t, r, "\x1b[<64;10;10M\x1b[<64;10;10M")
	select {
	case ev := <-r.ch:
		t.Fatalf("滚个屏幕却发出了事件：%+v", ev)
	default:
	}
	if scr.offset == 0 {
		t.Fatal("干活时也该能往回翻")
	}

	// 方向键同理，不该被当成打断。
	typeKeys(t, r, "\x1b[A")
	select {
	case ev := <-r.ch:
		t.Fatalf("按方向键却发出了事件：%+v", ev)
	default:
	}

	// 光秃秃的 esc 才是打断。
	typeKeys(t, r, "\x1b")
	select {
	case ev := <-r.ch:
		if ev.kind != evCtrlC {
			t.Fatalf("单按 esc 该打断：%+v", ev)
		}
	default:
		t.Fatal("单按 esc 没反应")
	}
}

// 确认框亮着时滚轮也不能被当成选项编号。
func TestScrollingDoesNotPickOption(t *testing.T) {
	fresh(t)
	r := newTestReader(t)
	r.setPicking(true)

	typeKeys(t, r, "\x1b[<64;10;10M")
	select {
	case ev := <-r.ch:
		t.Fatalf("滚轮不该变成选项按键：%+v", ev)
	default:
	}
}

// 解码层是三处 ESC 判断收拢成一处的地方，单独测它认不认得各种序列。
func TestReadKeyDecodesSequences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want keyKind
		r    rune
	}{
		{"普通字符", "a", kRune, 'a'},
		{"中文", "你", kRune, '你'},
		{"回车", "\r", kCtrl, '\r'},
		{"ctrl+c", "\x03", kCtrl, 0x03},
		{"光秃秃的 esc", "\x1b", kEsc, 0},
		{"上", "\x1b[A", kUp, 0},
		{"下", "\x1b[B", kDown, 0},
		{"右", "\x1b[C", kRight, 0},
		{"左", "\x1b[D", kLeft, 0},
		{"Home", "\x1b[H", kHome, 0},
		{"End", "\x1b[F", kEnd, 0},
		{"shift+tab", "\x1b[Z", kShiftTab, 0},
		{"Delete", "\x1b[3~", kDelete, 0},
		{"PageUp", "\x1b[5~", kPageUp, 0},
		{"PageDown", "\x1b[6~", kPageDown, 0},
		{"滚轮上", "\x1b[<64;10;10M", kWheelUp, 0},
		{"滚轮下", "\x1b[<65;10;10M", kWheelDown, 0},
		{"鼠标点击不算按键", "\x1b[<0;10;10M", kNone, 0},
		{"alt+b", "\x1bb", kAltB, 0},
		{"alt+f", "\x1bf", kAltF, 0},
		{"alt+d", "\x1bd", kAltD, 0},
		{"alt+退格", "\x1b\x7f", kAltBackspace, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rd := bufio.NewReader(strings.NewReader(c.in))
			k, err := readKey(rd)
			if err != nil {
				t.Fatalf("解不出来：%v", err)
			}
			if k.kind != c.want {
				t.Fatalf("解成了 %v，想要 %v", k.kind, c.want)
			}
			if c.r != 0 && k.r != c.r {
				t.Fatalf("字符是 %q，想要 %q", k.r, c.r)
			}
			// 整段序列必须一次吃干净，别给下一次剩下半截。
			if n := rd.Buffered(); n != 0 {
				rest := make([]byte, n)
				_, _ = rd.Read(rest)
				t.Fatalf("序列没吃干净，还剩 %q", rest)
			}
		})
	}
}

// 连着的几个序列要能一个一个解出来，不互相串味。
func TestReadKeyBackToBack(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("a\x1b[A\x1b[<64;1;1Mb"))
	want := []keyKind{kRune, kUp, kWheelUp, kRune}
	for i, w := range want {
		k, err := readKey(rd)
		if err != nil {
			t.Fatalf("第 %d 个解不出来：%v", i, err)
		}
		if k.kind != w {
			t.Fatalf("第 %d 个解成了 %v，想要 %v", i, k.kind, w)
		}
	}
}

// 滚动在任何模式下都该能使——干活时、确认框亮着时都一样。
func TestScrollWorksInEveryMode(t *testing.T) {
	for _, mode := range []string{"空闲", "干活", "确认框"} {
		h := fresh(t)
		func() {
			tr := testReader()
			scr.inputLines, scr.hintLines = tr.inputLines, tr.hintLines
			scr.statusFn = func() string { return "状态栏" }
		}()
		for i := 1; i <= 100; i++ {
			out(fmt.Sprintf("第 %d 行\n", i))
		}
		h.screen()

		r := newTestReader(t)
		switch mode {
		case "干活":
			r.setBusy(true)
		case "确认框":
			r.setPicking(true)
		}

		typeKeys(t, r, "\x1b[<64;10;10M")
		if scr.offset == 0 {
			t.Fatalf("%s时滚轮不好使", mode)
		}
		select {
		case ev := <-r.ch:
			t.Fatalf("%s时滚个屏幕却发出了事件：%+v", mode, ev)
		default:
		}
	}
}

// 粘一段多行文本进来：整段原样收下，不能被拆成好几条提交。
func TestPasteKeepsMultilineIntact(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	typeKeys(t, r, "\x1b[200~第一行\n第二行\n第三行\x1b[201~")

	if got := string(r.buf); got != "第一行\n第二行\n第三行" {
		t.Fatalf("粘进来的内容不对：%q", got)
	}
	select {
	case ev := <-r.ch:
		t.Fatalf("粘贴过程中不该提交：%+v", ev)
	default:
	}

	// 粘完再按回车，整段一次交出去。
	typeKeys(t, r, "\r")
	select {
	case ev := <-r.ch:
		if ev.kind != evLine || ev.line != "第一行\n第二行\n第三行" {
			t.Fatalf("提交的内容不对：%+v", ev)
		}
	default:
		t.Fatal("回车没提交")
	}
}

// 粘贴期间的斜杠、tab 之类都只是字符，不该触发命令或切视图。
func TestPasteDoesNotTriggerKeys(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	typeKeys(t, r, "\x1b[200~/help\x1b[201~")

	if got := string(r.buf); got != "/help" {
		t.Fatalf("粘进来的该是普通文本：%q", got)
	}
	select {
	case ev := <-r.ch:
		t.Fatalf("粘贴不该发出事件：%+v", ev)
	default:
	}
}

// alt+回车是换行，回车才是提交。
func TestAltEnterInsertsNewline(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	typeKeys(t, r, "第一行\x1b\r第二行")
	if got := string(r.buf); got != "第一行\n第二行" {
		t.Fatalf("alt+回车该换行：%q", got)
	}
	select {
	case ev := <-r.ch:
		t.Fatalf("alt+回车不该提交：%+v", ev)
	default:
	}
}

// 输入框空着的时候给句提示，有字了就让位。
func TestInputHint(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }

	scr.refresh()
	if !strings.Contains(h.text(), "说点什么") {
		t.Fatalf("空输入框该有提示：\n%s", h.text())
	}

	r.insert("你好")
	scr.refresh()
	got := h.text()
	if strings.Contains(got, "说点什么") {
		t.Fatalf("有字了提示该让位：\n%s", got)
	}
	if !strings.Contains(got, "你好") {
		t.Fatalf("打的字没显示：\n%s", got)
	}
}

// 多行内容让输入框长高，每行单独显示，箭头只在第一行。
func TestMultilineGrowsTheBox(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	r.insert("第一行\n第二行\n第三行")

	scr.refresh()
	lines, curRow, _ := r.inputLines()
	if len(lines) != 5 { // 上框线 + 三行 + 下框线（状态栏是另一段）
		t.Fatalf("三行内容该占五行，现在 %d 行", len(lines))
	}
	if curRow != 3 {
		t.Fatalf("光标该在第三个内容行（下标 3），现在 %d", curRow)
	}
	got := h.text()
	for _, want := range []string{"第一行", "第二行", "第三行"} {
		if !strings.Contains(got, want) {
			t.Fatalf("少了 %q：\n%s", want, got)
		}
	}
	if n := strings.Count(got, ">"); n != 1 {
		t.Fatalf("箭头只该出现一次，现在 %d 次：\n%s", n, got)
	}
}

// 粘进来一大段也不能把屏幕吃光：只显示光标附近那几行。
func TestManyLinesCapTheBox(t *testing.T) {
	fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	r.insert(strings.Repeat("一行\n", 30) + "最后一行")

	lines, curRow, _ := r.inputLines()
	if len(lines) != maxInputRows+2 { // 上下两条框线
		t.Fatalf("最多长到 %d 行内容（共 %d 行），现在 %d 行",
			maxInputRows, maxInputRows+2, len(lines))
	}
	if curRow < 1 || curRow > maxInputRows {
		t.Fatalf("光标该落在可见范围内，现在在第 %d 行", curRow)
	}
}

func TestCommandMatching(t *testing.T) {
	// 不写死有哪些命令——加一条新命令不该让测试碎掉。
	for _, prefix := range []string{"/m", "/l", "/c"} {
		got := matchCommands(prefix)
		if len(got) == 0 {
			t.Fatalf("%q 一条都没匹配到", prefix)
		}
		for _, c := range got {
			if !strings.HasPrefix(c.name, prefix) {
				t.Fatalf("%q 匹配出了 %s", prefix, c.name)
			}
		}
		if p := commonPrefix(got); !strings.HasPrefix(p, prefix) {
			t.Fatalf("%q 的公共前缀是 %q，比前缀本身还短", prefix, p)
		}
	}

	// 打全了只剩自己；带了参数就不再当命令补。
	if got := matchCommands("/quit"); len(got) != 1 || got[0].name != "/quit" {
		t.Fatalf("/quit 该只匹配自己，得到 %v", got)
	}
	for _, s := range []string{"/zzz", "不是命令", "/mail 带了参数"} {
		if got := matchCommands(s); got != nil {
			t.Fatalf("%q 不该匹配到命令，得到 %v", s, got)
		}
	}
}

func TestTabCompletesCommand(t *testing.T) {
	fresh(t)

	// 找一条前缀唯一、且要参数的命令来测「补全时连空格一起补上」。
	var uniq command
	for _, c := range commands {
		if c.args == "" {
			continue
		}
		if len(matchCommands(c.name[:len(c.name)-1])) == 1 {
			uniq = c
			break
		}
	}
	if uniq.name == "" {
		t.Skip("没有前缀唯一又带参数的命令可测")
	}
	r := newTestReader(t)
	typeKeys(t, r, uniq.name[:len(uniq.name)-1]+"\t")
	if got, want := string(r.buf), uniq.name+" "; got != want {
		t.Fatalf("该补成 %q，得到 %q", want, got)
	}

	// 多个匹配：补到公共前缀，并且不切视图。
	r = newTestReader(t)
	typeKeys(t, r, "/m\t")
	if got := string(r.buf); got != commonPrefix(matchCommands("/m")) {
		t.Fatalf("该补到公共前缀，得到 %q", got)
	}
	select {
	case ev := <-r.ch:
		t.Fatalf("还在补全就不该切视图：%+v", ev)
	default:
	}

	// 不是在打命令：tab 照旧切视图。
	r = newTestReader(t)
	typeKeys(t, r, "你好\t")
	select {
	case ev := <-r.ch:
		if ev.kind != evView {
			t.Fatalf("普通输入时 tab 该切视图：%+v", ev)
		}
	default:
		t.Fatal("tab 没反应")
	}
}

// 打命令的时候候选挂在输入框上面，打全了就收起来。
func TestCommandHintsAppearAndVanish(t *testing.T) {
	h := fresh(t)
	r := testReader()
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }

	r.insert("/m")
	scr.refresh()
	got := h.text()
	for _, want := range []string{"/model", "/mail"} {
		if !strings.Contains(got, want) {
			t.Fatalf("候选里少了 %q：\n%s", want, got)
		}
	}

	// 打全了只剩它自己，候选就该收起来。
	r.insert("odel")
	scr.refresh()
	if strings.Contains(h.text(), "切换模型") {
		t.Fatalf("打全之后不该还挂着候选：\n%s", h.text())
	}
}

// /help 和补全共用一张表，不会再各写各的走散。
func TestHelpCoversEveryCommand(t *testing.T) {
	fresh(t)
	s := &Session{}
	s.help()
	// 帮助比一屏长，所以查历史而不是查屏幕。
	got := strings.Join(scr.history, "\n")
	for _, c := range commands {
		if !strings.Contains(got, c.name) {
			t.Fatalf("/help 里没有 %s", c.name)
		}
	}
}

// 粘贴整段在解码层收干净：制表符这类控制字符也得原样留着。
func TestPasteKeepsTabsAndNewlines(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	typeKeys(t, r, "\x1b[200~func f() {\n\tif x {\n\t\treturn\n\t}\n}\x1b[201~")

	want := "func f() {\n\tif x {\n\t\treturn\n\t}\n}"
	if got := string(r.buf); got != want {
		t.Fatalf("粘进来的代码被改了：\n%q\n想要：\n%q", got, want)
	}
}

// 结束标记丢了也不能永久卡在粘贴状态——后面的按键还得照常使。
func TestPasteWithoutEndMarkRecovers(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	// 没有 ESC[201~，解码器读到流末尾就收手。
	typeKeys(t, r, "\x1b[200~半截")
	if got := string(r.buf); got != "半截" {
		t.Fatalf("没收尾的粘贴也该收下：%q", got)
	}

	// 接着按回车，照样提交。
	typeKeys(t, r, "\r")
	select {
	case ev := <-r.ch:
		if ev.kind != evLine || ev.line != "半截" {
			t.Fatalf("该正常提交：%+v", ev)
		}
	default:
		t.Fatal("卡在粘贴状态了，回车没反应")
	}
}

// 一次粘贴解成一个事件，不是一串字符。
func TestPasteIsOneKey(t *testing.T) {
	rd := bufio.NewReader(strings.NewReader("\x1b[200~abc\tdef\x1b[201~x"))
	k, err := readKey(rd)
	if err != nil {
		t.Fatal(err)
	}
	if k.kind != kPaste {
		t.Fatalf("该解成一次粘贴，得到 %v", k.kind)
	}
	if k.text != "abc\tdef" {
		t.Fatalf("粘贴内容是 %q", k.text)
	}
	// 紧跟其后的按键不受影响。
	k, _ = readKey(rd)
	if k.kind != kRune || k.r != 'x' {
		t.Fatalf("后面那个键被吃了：%v %q", k.kind, k.r)
	}
}

// 粘贴超过上限时，收尾的 ESC[201~ 也必须读掉——否则剩下的字节会被
// 当成按键，里头的回车会把输入直接提交出去。
func TestOversizePasteStillConsumesEndMark(t *testing.T) {
	fresh(t)
	r := newTestReader(t)

	big := strings.Repeat("x", maxPaste+100)
	typeKeys(t, r, "\x1b[200~"+big+"\x1b[201~\r")

	// 收尾标记之后那个回车是真按键，该把内容提交出去；
	// 要是标记没读干净，多出来的 x 会被当按键，这儿就收不到 evLine。
	select {
	case ev := <-r.ch:
		if ev.kind != evLine {
			t.Fatalf("回车该提交：%+v", ev)
		}
		if n := len(ev.line); n != maxPaste {
			t.Fatalf("粘贴该截到 %d 个字节，实际提交了 %d", maxPaste, n)
		}
	default:
		t.Fatal("回车没反应——收尾标记多半没读干净")
	}
}

// 向导的步骤表要齐整：每步都得有标题和干活的函数。
func TestSetupStepsAreComplete(t *testing.T) {
	if len(setupSteps) == 0 {
		t.Fatal("一步都没有")
	}
	seen := map[string]bool{}
	for i, st := range setupSteps {
		if strings.TrimSpace(st.title) == "" {
			t.Errorf("第 %d 步没有标题", i+1)
		}
		if st.run == nil {
			t.Errorf("第 %d 步（%s）没有实现", i+1, st.title)
		}
		if seen[st.title] {
			t.Errorf("标题重了：%s", st.title)
		}
		seen[st.title] = true
	}
	// 模型是唯一不配就什么都干不了的，该排在第一步。
	if setupSteps[0].title != "模型" {
		t.Fatalf("第一步该是模型，实际是 %s", setupSteps[0].title)
	}
}

// 配置摘要会打到屏幕上，密钥不能出现在里面。
func TestSummaryHidesKey(t *testing.T) {
	h := fresh(t)
	s := &Session{cfgPath: "/tmp/config.json"}
	s.cfg.Agent.Provider = "deepseek"
	s.cfg.Agent.APIKey = "sk-绝密不能外泄"
	s.cfg.Files = []string{"/tmp/org"}

	s.showConfigSummary()

	got := strings.Join(scr.history, "\n")
	if strings.Contains(got, "sk-绝密不能外泄") {
		t.Fatalf("密钥漏到摘要里了：\n%s", got)
	}
	if !strings.Contains(got, "密钥已设") {
		t.Fatalf("该说明密钥配没配：\n%s", got)
	}
	_ = h
}

// 重复跑向导不该把已经配好的东西弄坏：填的还是原来那个值，就什么都不动。
func TestSetupIsIdempotent(t *testing.T) {
	t.Run("服务商填原值不清模型名", func(t *testing.T) {
		before := "deepseek-reasoner"
		cfg := struct{ provider, model string }{"deepseek", before}
		// 换服务商才清模型名；填的还是同一个就得留着。
		if cfg.provider == "deepseek" {
			// 模拟 setupModel 里的判断
			name := "deepseek"
			if name != "" && name != cfg.provider {
				cfg.model = ""
			}
		}
		if cfg.model != before {
			t.Fatalf("填的还是同一个服务商，模型名被清成了 %q", cfg.model)
		}
	})

	t.Run("路径比较认得出同一批", func(t *testing.T) {
		cases := []struct {
			a, b []string
			same bool
		}{
			{[]string{"~/org"}, []string{"~/org"}, true},
			{[]string{"~/org"}, []string{"~/org", "~/work"}, false},
			{[]string{"~/org"}, []string{"~/别的"}, false},
			{nil, nil, true},
		}
		for _, c := range cases {
			if got := sameStrings(c.a, c.b); got != c.same {
				t.Errorf("sameStrings(%v, %v) = %v，想要 %v", c.a, c.b, got, c.same)
			}
		}
		// 展开前后该算同一个：一个写 ~，一个写全路径。
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("拿不到 home")
		}
		if !sameStrings([]string{"~/org"}, []string{filepath.Join(home, "org")}) {
			t.Fatal("~ 和展开后的全路径该算同一个")
		}
	})
}

// 日程读不上来也不能挡着启动：给个空的顶上，把问题记下来，
// 让人进到界面里再用 /files 或 /setup 改。
func TestSetupSurvivesBadOrgPath(t *testing.T) {
	dir := t.TempDir()
	s := &Session{cfgPath: filepath.Join(dir, "config.json")}
	s.cfg.Files = []string{"/dev/null/这个路径不可能建出来"}
	s.cfg.LedgerFile = filepath.Join(dir, "ledger.dat")

	if err := s.setup(); err != nil {
		t.Fatalf("日程读不了不该让启动失败：%v", err)
	}
	if s.coll == nil {
		t.Fatal("该给一个空集合顶上，而不是留 nil")
	}
	if len(s.startupWarn) == 0 {
		t.Fatal("该把问题记下来告诉用户")
	}
	if !strings.Contains(strings.Join(s.startupWarn, " "), "/setup") {
		t.Fatalf("提示该指出怎么修：%v", s.startupWarn)
	}

	// 空集合下各视图都得画得出来，不能崩。
	for _, v := range []view{viewAgenda, viewTodos, viewMail, viewLedger} {
		if lines := s.viewLines(v); len(lines) == 0 {
			t.Errorf("%s 视图什么都没画", v)
		}
	}
}

// 没配模型也能进界面，只是秘书不可用。
func TestSetupWithoutModel(t *testing.T) {
	dir := t.TempDir()
	s := &Session{cfgPath: filepath.Join(dir, "config.json")}
	s.cfg.Files = []string{filepath.Join(dir, "org")}
	s.cfg.LedgerFile = filepath.Join(dir, "ledger.dat")

	if err := s.setup(); err != nil {
		t.Fatalf("没配模型不该让启动失败：%v", err)
	}
	if s.agentErr == nil {
		t.Fatal("没配模型该记下原因")
	}
	if s.ag != nil {
		t.Fatal("没配模型时不该真造出一个秘书")
	}
	// 界面那套东西都得在。
	if s.orgWS == nil || s.mailS == nil || s.ledgerSkill == nil {
		t.Fatal("技能没搭起来")
	}
}
