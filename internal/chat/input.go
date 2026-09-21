package chat

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

type eventKind int

const (
	evLine   eventKind = iota // 完整一行
	evCtrlC                   // 打断
	evEOF                     // Ctrl+D / 输入结束
	evRedraw                  // 重新读一遍 org 文件并整屏重画
	evPick                    // 确认框里的按键：up / down / enter / esc / 数字
)

type event struct {
	kind eventKind
	line string
}

// reader 只干两件事：把 stdin 解码成按键事件，交出输入框该占的行。
// 屏幕怎么摆归 screen 管——它在 alt screen 里整屏自绘，输入框钉在最底下。
//
// 按键先经 readKey 解成 key，再按模式分派：确认框挑选项、干活时的打断，
// 剩下的才是行编辑。
type reader struct {
	ch   chan event
	fd   int
	old  *term.State
	busy atomic.Bool
	// picking 为真时按键全部交给确认框，不参与行编辑。
	picking atomic.Bool
	mu      sync.Mutex
	buf     []rune
	cur     int  // 光标在 buf 中的位置
	masked  bool // 密码模式：回显成圆点

	hist    []string
	histIdx int
	// kill 是最近一次被 ctrl+k/u/w 删掉的内容，ctrl+y 可以粘回来。
	kill []rune
	// 候选命令每帧都要问一次，而它只随按键变。存下上次的结果，
	// 输入和宽度都没动就直接还回去。
	hintText string
	hintW    int
	hintOut  []string
}

func newReader() (*reader, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	r := &reader{ch: make(chan event, 16), fd: fd, old: old, histIdx: -1}
	go r.loop()
	go r.watchResize()
	return r, nil
}

// watchResize 在终端尺寸变化时整屏重画，免得框线错位、diff 对不上。
func (r *reader) watchResize() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	for range ch {
		scr.invalidate()
	}
}

func (r *reader) close() {
	_ = term.Restore(r.fd, r.old)
}

// setBusy 标记秘书是不是在干活。干活期间输入框照常留着，
// 可以接着打字；只有 esc / ctrl+c 会被当成打断。
func (r *reader) setBusy(b bool) {
	r.busy.Store(b)
	scr.refresh()
}

func (r *reader) loop() {
	rd := bufio.NewReader(os.Stdin)
	for {
		k, err := readKey(rd)
		if err != nil {
			r.ch <- event{kind: evEOF}
			return
		}
		if !r.handleKey(k) {
			return
		}
	}
}

// keyKind 是一次按键解出来的语义。把字节流解成它，是为了让
// 「ESC 后面跟没跟东西」这类判断只在一个地方做——之前打断、确认框、
// 行编辑三处各判一遍，已经开始各走各的了。
type keyKind int

const (
	kRune keyKind = iota // 普通字符
	kCtrl                // 控制字符，r 是原始字节
	kEsc                 // 光秃秃的一个 esc
	kUp
	kDown
	kLeft
	kRight
	kHome
	kEnd
	kDelete
	kShiftTab
	kPageUp
	kPageDown
	kWheelUp
	kWheelDown
	kAltB
	kAltF
	kAltD
	kAltBackspace
	kAltEnter
	kPaste
	kNone // 认不出来的，直接丢掉
)

type key struct {
	kind keyKind
	r    rune
	// text 只有 kPaste 用：整段粘进来的内容。
	text string
}

// readKey 从字节流里解出一次按键，转义序列一次吃干净。
func readKey(rd *bufio.Reader) (key, error) {
	rn, _, err := rd.ReadRune()
	if err != nil {
		return key{}, err
	}
	if rn != 0x1b {
		if rn < 0x20 || rn == 0x7f {
			return key{kind: kCtrl, r: rn}, nil
		}
		return key{kind: kRune, r: rn}, nil
	}

	// 光秃秃的 esc 后面没有字节，是用户按了 esc；带着后续字节的
	// 是方向键或鼠标上报，别把它当成打断。
	if rd.Buffered() == 0 {
		return key{kind: kEsc}, nil
	}
	b1, err := rd.ReadByte()
	if err != nil {
		return key{kind: kEsc}, nil
	}
	if b1 != '[' && b1 != 'O' {
		switch b1 {
		case 'b', 'B':
			return key{kind: kAltB}, nil
		case 'f', 'F':
			return key{kind: kAltF}, nil
		case 'd', 'D':
			return key{kind: kAltD}, nil
		case 0x7f, 0x08:
			return key{kind: kAltBackspace}, nil
		case '\r', '\n':
			return key{kind: kAltEnter}, nil
		}
		return key{kind: kNone}, nil
	}

	b2, err := rd.ReadByte()
	if err != nil {
		return key{kind: kNone}, nil
	}
	switch b2 {
	case 'A':
		return key{kind: kUp}, nil
	case 'B':
		return key{kind: kDown}, nil
	case 'C':
		return key{kind: kRight}, nil
	case 'D':
		return key{kind: kLeft}, nil
	case 'H':
		return key{kind: kHome}, nil
	case 'F':
		return key{kind: kEnd}, nil
	case 'Z':
		return key{kind: kShiftTab}, nil
	case '2': // 2~ 是 Insert，200~ 是一段粘贴的开头
		return readPaste(rd), nil
	case '3':
		eatTilde(rd)
		return key{kind: kDelete}, nil
	case '5':
		eatTilde(rd)
		return key{kind: kPageUp}, nil
	case '6':
		eatTilde(rd)
		return key{kind: kPageDown}, nil
	case '<':
		return readMouse(rd), nil
	}
	return key{kind: kNone}, nil
}

// maxPaste 是一次粘贴最多往输入框里攒多少字节。超出的照读不误、
// 只是不再攒——收尾的 ESC[201~ 必须消费掉，否则剩下的字节会被
// 当成按键解析，里头的回车会直接把输入提交出去。
const maxPaste = 1 << 20

// readPaste 认 ESC[200~ 并一口气读到 ESC[201~，整段原样交出去。
// 收在这一层，上面就不用为粘贴单开一个模式：制表符、换行都留着，
// 也不会因为结束标记丢失而永久卡在粘贴状态。前面已经读掉了 '2'。
func readPaste(rd *bufio.Reader) key {
	var head strings.Builder
	for head.Len() < 4 {
		c, err := rd.ReadByte()
		if err != nil {
			return key{kind: kNone}
		}
		if c == '~' {
			break
		}
		head.WriteByte(c)
	}
	if head.String() != "00" { // 201~ 是收尾，单独出现就忽略；其余不认
		return key{kind: kNone}
	}

	var body strings.Builder
	for {
		c, err := rd.ReadByte()
		if err != nil {
			break
		}
		if c == 0x1b {
			if b, _ := rd.Peek(5); strings.HasSuffix(string(b), "201~") {
				_, _ = rd.Discard(5)
				break
			}
		}
		if body.Len() < maxPaste {
			body.WriteByte(c)
		}
	}
	return key{kind: kPaste, text: body.String()}
}

// eatTilde 吃掉// eatTilde 吃掉 ESC[N~ 这类序列结尾的波浪号。
func eatTilde(rd *bufio.Reader) {
	if b, err := rd.ReadByte(); err == nil && b != '~' {
		_ = rd.UnreadByte()
	}
}

// readMouse 解析 SGR 鼠标上报 ESC[<按键;列;行M。只认滚轮，
// 别的按键事件整段吃掉就算了。
func readMouse(rd *bufio.Reader) key {
	var sb strings.Builder
	for sb.Len() < 32 {
		c, err := rd.ReadByte()
		if err != nil {
			return key{kind: kNone}
		}
		if c == 'M' || c == 'm' {
			break
		}
		sb.WriteByte(c)
	}
	btn, err := strconv.Atoi(strings.SplitN(sb.String(), ";", 2)[0])
	if err != nil {
		return key{kind: kNone}
	}
	switch btn {
	case 64:
		return key{kind: kWheelUp}
	case 65:
		return key{kind: kWheelDown}
	}
	return key{kind: kNone}
}

// handleKey 分派一次按键，返回是否继续读下去。
func (r *reader) handleKey(k key) bool {
	// 翻历史在哪个模式下都一样使。
	switch k.kind {
	case kWheelUp:
		scr.scroll(3)
		return true
	case kWheelDown:
		scr.scroll(-3)
		return true
	case kPageUp:
		scr.scroll(height() / 2)
		return true
	case kPageDown:
		scr.scroll(-height() / 2)
		return true
	case kNone:
		return true
	}

	// 确认框亮着时，按键只用来挑选项。
	if r.picking.Load() {
		r.pick(k)
		return true
	}

	// 秘书干活时 esc / ctrl+c 是打断，别的键照常打字，
	// 回车会把这句话排到队里。
	if r.busy.Load() && (k.kind == kEsc || (k.kind == kCtrl && k.r == 0x03)) {
		r.ch <- event{kind: evCtrlC}
		return true
	}

	switch k.kind {
	case kRune:
		r.edit(func() { r.insertRunes([]rune{k.r}) })
	case kUp:
		r.histUp()
	case kDown:
		r.histDown()
	case kLeft:
		r.edit(func() {
			if r.cur > 0 {
				r.cur--
			}
		})
	case kRight:
		r.edit(func() {
			if r.cur < len(r.buf) {
				r.cur++
			}
		})
	case kHome:
		r.edit(func() { r.cur = 0 })
	case kEnd:
		r.edit(func() { r.cur = len(r.buf) })
	case kDelete:
		r.edit(r.deleteForward)
	case kAltB:
		r.edit(func() { r.cur = r.wordLeft() })
	case kAltF:
		r.edit(func() { r.cur = r.wordRight() })
	case kAltD:
		r.edit(func() { r.killTo(r.wordRight()) })
	case kAltBackspace:
		r.edit(r.deleteWord)
	case kAltEnter: // alt+回车：换行而不是提交
		r.edit(func() { r.insertRunes([]rune{'\n'}) })
	case kPaste: // 粘进来的一整段，原样收下
		r.edit(func() { r.insertRunes([]rune(k.text)) })
	case kCtrl:
		return r.handleCtrl(k.r)
	}
	return true
}

// handleCtrl 处理控制字符，返回是否继续读下去。
func (r *reader) handleCtrl(rn rune) bool {
	switch rn {
	case '\r', '\n':
		r.mu.Lock()
		line := string(r.buf)
		r.buf, r.cur, r.histIdx = nil, 0, -1
		if line != "" && (len(r.hist) == 0 || r.hist[len(r.hist)-1] != line) {
			r.hist = append(r.hist, line)
		}
		r.mu.Unlock()
		scr.refresh()
		r.ch <- event{kind: evLine, line: line}
	case 0x03: // ctrl+c：行里有字先清空，空行才是退出
		r.mu.Lock()
		had := len(r.buf) > 0
		r.buf, r.cur, r.histIdx = nil, 0, -1
		r.mu.Unlock()
		if had {
			r.draw()
			return true
		}
		scr.refresh()
		r.ch <- event{kind: evCtrlC}
	case 0x04: // ctrl+d：行里有字就删光标处那个，空行才是退出
		r.mu.Lock()
		empty := len(r.buf) == 0
		r.mu.Unlock()
		if !empty {
			r.edit(r.deleteForward)
			return true
		}
		r.ch <- event{kind: evEOF}
		return false
	case 0x09: // tab：补全正在打的命令
		r.completeCommand()
	case 0x12: // ctrl+r：重读 org 文件并重画
		r.ch <- event{kind: evRedraw}
	case 0x0b: // ctrl+k：删到行尾
		r.edit(func() { r.killTo(len(r.buf)) })
	case 0x15: // ctrl+u：删到行首
		r.edit(func() { r.killTo(0) })
	case 0x17: // ctrl+w：删掉光标前一个词
		r.edit(r.deleteWord)
	case 0x19: // ctrl+y：把刚删掉的粘回来
		r.edit(func() { r.insertRunes(r.kill) })
	case 0x01: // ctrl+a：行首
		r.edit(func() { r.cur = 0 })
	case 0x05: // ctrl+e：行尾
		r.edit(func() { r.cur = len(r.buf) })
	case 0x02: // ctrl+b：左移一个字
		r.edit(func() {
			if r.cur > 0 {
				r.cur--
			}
		})
	case 0x06: // ctrl+f：右移一个字
		r.edit(func() {
			if r.cur < len(r.buf) {
				r.cur++
			}
		})
	case 0x10: // ctrl+p：上一条历史
		r.histUp()
	case 0x0e: // ctrl+n：下一条历史
		r.histDown()
	case 0x14: // ctrl+t：把光标前两个字掉个个儿
		r.edit(func() {
			if r.cur >= 2 {
				r.buf[r.cur-2], r.buf[r.cur-1] = r.buf[r.cur-1], r.buf[r.cur-2]
			}
		})
	case 0x0c: // ctrl+l：把翻得到的历史清掉
		scr.clearHistory()
	case 0x7f, 0x08: // 退格
		r.edit(func() {
			if r.cur > 0 {
				r.buf = append(r.buf[:r.cur-1], r.buf[r.cur:]...)
				r.cur--
			}
		})
	}
	return true
}

// setPicking 切换确认框模式。
func (r *reader) setPicking(on bool) { r.picking.Store(on) }

// pick 把按键翻成确认框里的动作。
func (r *reader) pick(k key) {
	send := func(s string) { r.ch <- event{kind: evPick, line: s} }
	switch k.kind {
	case kUp:
		send("up")
	case kDown:
		send("down")
	case kEsc:
		send("esc")
	case kRune:
		switch {
		case k.r == 'k':
			send("up")
		case k.r == 'j':
			send("down")
		case k.r >= '1' && k.r <= '9':
			send(string(k.r))
		}
	case kCtrl:
		switch k.r {
		case '\r', '\n':
			send("enter")
		case 0x03, 0x04: // ctrl+c / ctrl+d 视作取消
			send("esc")
		}
	}
}

// deleteForward 删掉光标处那个字。调用方需持锁。
func (r *reader) deleteForward() {
	if r.cur < len(r.buf) {
		r.buf = append(r.buf[:r.cur], r.buf[r.cur+1:]...)
	}
}

// edit 在锁内改缓冲区，然后重画输入框。
func (r *reader) edit(f func()) {
	r.mu.Lock()
	f()
	r.mu.Unlock()
	r.draw()
}

// completeCommand 把命令补到最长的公共前缀。补不动就什么都不做——
// 候选还列在输入框上面，接着打就是了。
func (r *reader) completeCommand() {
	r.mu.Lock()
	text := string(r.buf)
	cs := matchCommands(text)
	full := commonPrefix(cs)
	// 只剩一条就连空格一起补上，接着打参数。
	if len(cs) == 1 && cs[0].args != "" {
		full += " "
	}
	if len(cs) == 0 || full == text {
		r.mu.Unlock()
		return
	}
	r.buf = []rune(full)
	r.cur = len(r.buf)
	r.mu.Unlock()
	scr.refresh()
}

// killTo 把光标与 pos 之间的内容剪下来存进 kill，供 ctrl+y 粘回。
func (r *reader) killTo(pos int) {
	lo, hi := r.cur, pos
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == hi {
		return
	}
	r.kill = append([]rune(nil), r.buf[lo:hi]...)
	r.buf = append(r.buf[:lo], r.buf[hi:]...)
	r.cur = lo
}

// insertRunes 在光标处插入一段字符。调用方需持锁。
func (r *reader) insertRunes(rs []rune) {
	if len(rs) == 0 {
		return
	}
	r.buf = append(r.buf[:r.cur], append(append([]rune(nil), rs...), r.buf[r.cur:]...)...)
	r.cur += len(rs)
}

// deleteWord 删掉光标前的一个词，删掉的存进 kill 供 ctrl+y 粘回。
func (r *reader) deleteWord() { r.killTo(r.wordLeft()) }

// wordLeft 和 wordRight 找按词移动的落点。
func (r *reader) wordLeft() int {
	i := r.cur
	for i > 0 && r.buf[i-1] == ' ' {
		i--
	}
	for i > 0 && r.buf[i-1] != ' ' {
		i--
	}
	return i
}

func (r *reader) wordRight() int {
	i := r.cur
	for i < len(r.buf) && r.buf[i] == ' ' {
		i++
	}
	for i < len(r.buf) && r.buf[i] != ' ' {
		i++
	}
	return i
}

func (r *reader) histUp() {
	r.mu.Lock()
	if len(r.hist) == 0 || r.histIdx >= len(r.hist)-1 {
		r.mu.Unlock()
		return
	}
	r.histIdx++
	r.buf = []rune(r.hist[len(r.hist)-1-r.histIdx])
	r.cur = len(r.buf)
	r.mu.Unlock()
	r.draw()
}

func (r *reader) histDown() {
	r.mu.Lock()
	if r.histIdx <= 0 {
		r.histIdx, r.buf = -1, nil
	} else {
		r.histIdx--
		r.buf = []rune(r.hist[len(r.hist)-1-r.histIdx])
	}
	r.cur = len(r.buf)
	r.mu.Unlock()
	r.draw()
}

// setMasked 切换密码回显。
func (r *reader) setMasked(on bool) {
	r.mu.Lock()
	r.masked = on
	r.mu.Unlock()
}

// draw 请底部区重画一遍。输入框的位置由 screen 统一安排，
// 这里只管内容。
func (r *reader) draw() { scr.refresh() }

// insert 把一段文字塞进输入缓冲（打断时把没答复的那句退回来）。
func (r *reader) insert(text string) {
	r.mu.Lock()
	rs := []rune(text)
	r.buf = append(r.buf[:r.cur], append(rs, r.buf[r.cur:]...)...)
	r.cur += len(rs)
	r.mu.Unlock()
}

// inputLines 交出输入框那几行，以及光标落在框里第几行第几列。
// 候选命令和状态栏是底部区里另外的段，不归这儿管。
func (r *reader) inputLines() ([]string, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	w := frameWidth() - 1
	if w < 24 {
		w = 24
	}
	inner := w - 4 // 去掉左右框线和各一个空格
	textW := inner - 2
	text := string(r.buf)
	if r.masked {
		text = strings.Repeat("•", len(r.buf))
	}

	rows, curRow, curCol := inputRows(text, r.cur, textW)

	// 空着的时候给句提示，别让人对着一个光标发愣。
	hint := text == "" && !r.masked
	if hint {
		rows[0] = "说点什么，或者 / 看命令"
	}

	body := make([]string, 0, len(rows))
	for i, row := range rows {
		mark := "  " // 续行不再重复那个箭头
		if i == 0 {
			mark = coral + "> " + reset
		}
		cell := pad(row, textW)
		if hint && i == 0 {
			cell = dim + cell + reset // 先补宽再上色，pad 不数转义序列
		}
		body = append(body, mark+cell)
	}
	return roundBox(body, w), 1 + curRow, 4 + curCol
}

// hintLines 列出跟当前输入匹配的命令，由 screen 摆在输入框上面。
func (r *reader) hintLines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	text, w := string(r.buf), frameWidth()-1
	if r.hintOut != nil && r.hintText == text && r.hintW == w {
		return r.hintOut
	}
	r.hintOut = commandHints(text, w)
	if r.hintOut == nil {
		r.hintOut = []string{} // 空结果也要记住，别每帧再算一遍
	}
	r.hintText, r.hintW = text, w
	return r.hintOut
}

// maxHints 是最多列几条候选命令。
const maxHints = 6

// commandHints 列出跟当前输入匹配的命令，挂在输入框上面。
func commandHints(text string, w int) []string {
	cs := matchCommands(text)
	// 只剩一条而且已经打全了，就不用再提示了。
	if len(cs) == 0 || (len(cs) == 1 && cs[0].name == text) {
		return nil
	}
	var outs []string
	for i, c := range cs {
		if i == maxHints {
			outs = append(outs, dim+fmt.Sprintf("  …… 还有 %d 条", len(cs)-maxHints)+reset)
			break
		}
		name := c.name
		if c.args != "" {
			name += " " + c.args
		}
		outs = append(outs, truncVis("  "+coral+pad(name, 26)+reset+dim+c.desc+reset, w))
	}
	return outs
}

// maxInputRows 是输入框最多长到几行。再多就只显示光标附近那一段，
// 免得粘进来一大段把屏幕吃光。
const maxInputRows = 6

// inputRows 把（可能多行的）输入切成要显示的若干行，
// 并算出光标落在第几行第几列。
func inputRows(text string, cur, w int) (rows []string, curRow, curCol int) {
	lines := strings.Split(text, "\n")

	// 光标在第几个逻辑行、行内第几个字符。
	curRow, inLine := len(lines)-1, cur
	for i, l := range lines {
		n := len([]rune(l))
		if inLine <= n {
			curRow = i
			break
		}
		inLine -= n + 1 // 加上那个换行符
	}

	// 行数超了就只留光标附近那一段。
	start, end := 0, len(lines)
	if end > maxInputRows {
		start = min(max(0, curRow-maxInputRows+1), end-maxInputRows)
		end = start + maxInputRows
	}

	rows = make([]string, 0, end-start)
	for _, l := range lines[start:end] {
		shown, _ := visibleTail(l, len([]rune(l)), w)
		rows = append(rows, shown)
	}
	_, curCol = visibleTail(lines[curRow], inLine, w)
	return rows, curRow - start, curCol
}

// visibleTail 返回输入行里实际显示的片段，以及光标前的显示宽度。
func visibleTail(text string, cur, w int) (string, int) {
	runes := []rune(text)
	if cur > len(runes) {
		cur = len(runes)
	}
	if runewidth.StringWidth(text) <= w {
		return text, runewidth.StringWidth(string(runes[:cur]))
	}
	// 从光标往左取，凑满一屏。
	start := cur
	width := 0
	for start > 0 && width+runewidth.RuneWidth(runes[start-1]) <= w-1 {
		start--
		width += runewidth.RuneWidth(runes[start])
	}
	end := cur
	for end < len(runes) && width+runewidth.RuneWidth(runes[end]) <= w-1 {
		width += runewidth.RuneWidth(runes[end])
		end++
	}
	return string(runes[start:end]), runewidth.StringWidth(string(runes[start:cur]))
}
