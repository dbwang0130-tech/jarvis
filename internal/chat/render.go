package chat

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// screen 是整屏渲染器。进 alt screen 之后整块屏幕归我们自己画：
//
//	[内容区]        历史 + 正在流的正文，可以往上滚
//	[确认框]
//	[转圈提示]
//	[输入框]        钉在底下，不随内容走
//	[状态栏]
//
// 这跟 Claude Code 是同一个路子：终端原生的 scrollback 不再参与，
// 历史攒在 history 里自己管，滚动就是挪 offset 再整屏重画。
// 每帧包在「同步输出」里，终端攒够一帧再刷，不会看到撕裂。
//
// 退出时会把 history 回放到主屏幕上，这一趟的内容不会白跑（这点比
// Claude Code 强：它退出后屏幕就恢复原样了）。
type screen struct {
	mu sync.Mutex

	spinning bool
	frame    int
	start    time.Time
	verb     string
	tokens   int
	stop     chan struct{}

	// history 是已经定稿的内容行，往上滚看的就是它。
	history []string
	// offset 是从底往上滚了多少行，0 表示贴着底。
	offset int

	// 底部那块由这几段拼起来，从上到下：确认框、转圈、候选命令、
	// 输入框、状态栏。每段各报各的行，谁在第几行由这里算，
	// 调用方不用互相猜位置。
	liveLines    func() []string
	confirmLines func() []string
	hintLines    func() []string
	inputLines   func() (lines []string, curRow, curCol int)
	statusFn     func() string
	// content 非空时，内容区显示的是它而不是聊天历史——
	// 日程、待办这些视图是整屏换掉，不该往对话里灌东西。
	content []string

	alt bool
	// curW/curH 是这一帧的尺寸，render 开头定下来供各段共用：
	// 少几次 ioctl 是其次，主要是窗口正被拖动时各段别拿到不同的宽度。
	curW, curH int
	// onResize 在终端尺寸变化时被叫一次，让内容按新宽度重新生成。
	onResize func()
	// mouse 为真表示正在收鼠标上报（滚轮能用，代价是拖选被拦）。
	mouse bool
	// lastLen 是上一帧内容有多少行，用来在翻历史时抵消新内容的推挤。
	lastLen int
	// prev 是上一帧每行画的是什么。只把变了的行重画一遍——
	// 不然流式正文每来一个字就整屏重刷，几 KB 的转义序列砸下去，
	// 终端跟不上不说，管道写满了还会把自己卡死。
	prev []string
}

var scr = &screen{}

// spinFrames 是转圈动画，取 Claude Code 那种由细到粗的星芒。
var spinFrames = []string{"·", "✢", "✳", "∗", "✻", "✽"}

// verbs 是等待时显示的动词，管家腔调，每轮随机换一个。
var verbs = []string{"思忖", "盘算", "琢磨", "推演", "斟酌", "打点", "筹划", "端详"}

// 框线、箭头、中点这些 Ambiguous 字符，绝大多数终端都按一列画。
// runewidth 默认会跟着中文 locale 把它们算成两列，那样框就会散架。
func init() { runewidth.DefaultCondition.EastAsianWidth = false }

// termOut 是所有输出的去处，测试时可以换成缓冲区。
var termOut io.Writer = os.Stdout

// raw 直接写终端，不做任何换行转换。
func raw(s string) { io.WriteString(termOut, s) }

// level 是一条消息在界面上的层级。缩进和前缀由这张表定死，
// 调用方只挑层级、不自己拼前缀——之前各拼各的，结果 note("  ⎿ …")
// 和 note 自带的 "· " 叠成了「· ⎿」。
type level int

const (
	lvUser   level = iota // > 你说的话
	lvAgent               // ⏺ 秘书说的话
	lvTool                // ⏺ 秘书调的工具
	lvResult              // ⎿ 工具的结果
	lvNote                // · 程序的回执
	lvError               // ✗ 出了岔子
	lvSystem              // ✻ 压缩上下文这类自己的动静
)

// levels 定义每一层长什么样：缩进、带色的前缀、正文颜色（空表示不上色）。
var levels = map[level]struct{ indent, glyph, body string }{
	lvUser:   {"", coral + "> " + reset, ""},
	lvAgent:  {"", green + "⏺ " + reset, ""},
	lvTool:   {"", coral + "⏺ " + reset, ""},
	lvResult: {"  ", dim + "⎿ " + reset, dim},
	lvNote:   {"  ", dim + "· " + reset, dim},
	lvError:  {"  ", red + "✗ " + reset, red},
	lvSystem: {"", dim + "✻ " + reset, dim},
}

// roundBox 把几行内容套进一个圆角框。框宽 w（含边框），
// 每行内容已经补齐到 w-4 宽。输入框和确认框共用这一份画法。
func roundBox(body []string, w int) []string {
	out := make([]string, 0, len(body)+2)
	out = append(out, gray+"╭"+strings.Repeat("─", w-2)+"╮"+reset)
	for _, l := range body {
		out = append(out, gray+"│ "+reset+l+gray+" │"+reset)
	}
	return append(out, gray+"╰"+strings.Repeat("─", w-2)+"╯"+reset)
}

// glyphOf 返回某一层的前缀，供流式正文那路取用——它不走 msg
// （内容是一帧帧重画的），但前缀得跟这张表保持一致。
func glyphOf(lv level) string { return levels[lv].glyph }

// msg 按层级打一条消息。太长会自己折行，续行缩进对齐首行正文——
// 交给 screen 那层去折的话，折出来的行不知道该缩进多少。
func msg(lv level, format string, args ...any) {
	st := levels[lv]
	hang := st.indent + strings.Repeat(" ", visWidth(st.glyph))
	avail := width() - visWidth(hang) - 1
	if avail < 16 {
		avail = 16
	}

	var b strings.Builder
	first := true
	for _, para := range strings.Split(fmt.Sprintf(format, args...), "\n") {
		for _, line := range wrapANSI(para, avail) {
			if st.body != "" {
				line = st.body + line + reset
			}
			if first {
				b.WriteString(st.indent + st.glyph + line + "\n")
				first = false
				continue
			}
			b.WriteString(hang + line + "\n")
		}
	}
	out(b.String())
}

// prompt 打一行提问。跟回执同一层缩进，但不带那个点——
// 问句前面顶个圆点读着别扭。
func promptLine(hint string) {
	out("  " + gray + hint + reset + "\n")
}

// note 打一条回执：命令做完了、档位换了，这类程序自己说的话。
func note(format string, args ...any) { msg(lvNote, format, args...) }

// fail 打一条出错提示。
func fail(format string, args ...any) { msg(lvError, format, args...) }

// width 返回终端宽度// frameWidth 是这一帧说定的宽度。render 之外（测试、直接调各段）
// 拿不到就现问终端。
func frameWidth() int {
	if scr.curW > 0 {
		return scr.curW
	}
	return width()
}

// width 返回终端宽度，取不到就按 80 算。
func width() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 20 {
		return 80
	}
	return w
}

// height 返回终端高度，取不到就按 24 算。
func height() int {
	_, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || h < 4 {
		return 24
	}
	return h
}

// enter 进 alt screen：独立的一块屏幕，原来的终端内容原封不动留在下面。
// 顺便开鼠标按键上报，滚轮才能送到我们手里。
func (s *screen) enter(mouse bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw("\033[?1049h" + // 换到 alt screen
		"\033[2J\033[H" + // 清屏、光标归位
		"\033[?2004h" + // 粘贴的内容会被包起来，好跟手打的区分开
		"\033[?25l") // 先把光标藏起来，画完再摆位置
	s.alt = true
	s.prev = nil
	s.setMouseLocked(mouse)
}

// setMouseLocked 开关鼠标上报。开着滚轮才能翻历史，但终端原生的
// 拖选会被拦掉；关掉就换回拖选。调用方需持锁。
func (s *screen) setMouseLocked(on bool) {
	if on == s.mouse {
		return
	}
	s.mouse = on
	if on {
		raw("\033[?1000h\033[?1006h") // 鼠标按键上报 + SGR 坐标（滚轮走这个）
		return
	}
	raw("\033[?1000l\033[?1006l")
}

// SetMouse 运行时开关鼠标上报。
func (s *screen) SetMouse(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setMouseLocked(on)
}

// MouseOn 报告这会儿收不收鼠标上报。
func (s *screen) MouseOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mouse
}

// leave 收摊：关鼠标、退回原来的屏幕，然后把这一趟的内容回放出来，
// 留在终端的 scrollback 里，想翻想复制都行。
func (s *screen) leave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.alt {
		return
	}
	s.setMouseLocked(false)
	raw("\033[?2004l\033[?25h\033[?1049l")
	s.alt = false
	if len(s.history) > 0 {
		raw(strings.Join(s.history, "\r\n") + "\r\n")
	}
}

// write 把内容记进历史再重画。传进来的东西按行拆开，太长的先折好。
func (s *screen) write(text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLocked(text)
	s.render()
}

// commit 把正文定格进历史（live 收工时用）。
func (s *screen) commit(lines []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, lines...)
	s.trimHistoryLocked()
	s.render()
}

// maxHistory 是往回翻得到的最多行数。一场会话很难到这个量级，
// 纯粹是给长跑的会话留个兜底，免得内存一直涨。
const maxHistory = 20000

// trimHistoryLocked 砍掉最老的行，顺便把滚动位置跟着挪，
// 免得手里的 offset 指到已经没了的地方。调用方需持锁。
func (s *screen) trimHistoryLocked() {
	if n := len(s.history) - maxHistory; n > 0 {
		s.history = s.history[n:]
		if s.offset -= n; s.offset < 0 {
			s.offset = 0
		}
	}
}

// appendLocked 把一段文本按行折好塞进历史。调用方需持锁。
func (s *screen) appendLocked(text string) {
	text = strings.TrimSuffix(text, "\n")
	w := width()
	for _, line := range strings.Split(text, "\n") {
		s.history = append(s.history, wrapANSI(line, w)...)
	}
	s.trimHistoryLocked()
}

// invalidate 作废上一帧的记录：窗口尺寸一变，屏幕上是什么就不作数了，
// 得整屏重画一遍，不然 diff 会以为还是老样子。
func (s *screen) invalidate() {
	s.mu.Lock()
	s.prev = nil
	raw("\033[2J")
	s.mu.Unlock()

	// 内容是按当时的宽度折好行存下来的，窗口一变就得重新生成，
	// 否则视图里的内容要么溢出要么留一大片白。回调自己会重画。
	if s.onResize != nil {
		s.onResize()
		return
	}
	s.refresh()
}

// refresh 重画一帧。
func (s *screen) refresh() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render()
}

// scroll 往上（n 为正）或往下（n 为负）滚几行。
func (s *screen) scroll(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset += n
	s.render()
}

// toBottom 跳回最新内容。
func (s *screen) toBottom() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.offset = 0
	s.render()
}

// setContent 换掉内容区的内容。传 nil 表示回到聊天历史。
// 换了来源才重新定位：视图是从上往下读的，滚到顶；聊天回到最新。
// 同一个视图刷新内容时，滚动位置留着不动。
func (s *screen) setContent(lines []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (s.content == nil) != (lines == nil) {
		s.offset, s.lastLen = 0, 0
		if lines != nil {
			s.offset = 1 << 30 // 夹回合法范围后就是顶
		}
	}
	s.content = lines
	s.render()
}

// render 整屏画一帧。调用方需持锁。
func (s *screen) render() {
	s.curW, s.curH = width(), height()
	h := s.curH
	bottom, curRow, curCol := s.bottomLocked()
	avail := h - len(bottom)
	if avail < 1 {
		avail = 1
	}

	// 内容区要么是别的视图的内容，要么是聊天（历史 + 正在流的正文）。
	// 聊天那路不拼成新切片——那是每帧两次全量复制，而真正要画的只有
	// 一屏那几十行，按下标取就够了。
	var head, ls []string
	if s.content != nil {
		head = s.content
	} else {
		head = s.history
		if s.liveLines != nil {
			ls = s.liveLines()
		}
	}
	total := len(head) + len(ls)
	at := func(i int) string {
		if i < len(head) {
			return head[i]
		}
		return ls[i-len(head)]
	}

	// 正翻着历史的时候，新内容不该把眼前这段推走——涨了多少就补多少。
	if s.offset > 0 && total > s.lastLen {
		s.offset += total - s.lastLen
	}
	s.lastLen = total

	// 把滚动位置夹回合法范围。
	if max := total - avail; s.offset > max {
		s.offset = max
	}
	if s.offset < 0 {
		s.offset = 0
	}
	end := total - s.offset
	start := end - avail
	if start < 0 {
		start = 0
	}

	// 滚动位置这会儿才定下来，补在状态栏后面。状态栏是具名的一段，
	// 不用靠下标去猜它在哪。
	if s.statusFn != nil && len(bottom) > 0 {
		st := s.statusFn()
		if s.offset > 0 {
			st += fmt.Sprintf("%s · ↑ %d/%d 行 · PageDown 回到最新%s",
				warn, end, total, reset+dim)
		}
		bottom[len(bottom)-1] = truncVis(st, s.curW-1)
	}

	// 先把这一帧整屏该长什么样拼出来，再跟上一帧比，只画不一样的行。
	//
	// 内容不够一屏时补在哪头有讲究：聊天贴着底（最新的在眼前），
	// 日程这些视图贴着顶（从头往下读）。
	frame := make([]string, 0, h)
	if s.content == nil {
		for i := end - start; i < avail; i++ {
			frame = append(frame, "")
		}
	}
	for i := start; i < end; i++ {
		frame = append(frame, at(i))
	}
	for len(frame) < avail {
		frame = append(frame, "")
	}
	frame = append(frame, bottom...)
	for len(frame) < h {
		frame = append(frame, "")
	}
	frame = frame[:h]

	var b strings.Builder
	b.WriteString("\033[?2026h") // 这一帧攒齐了再刷，别让人看见画到一半
	for i, line := range frame {
		if i < len(s.prev) && s.prev[i] == line {
			continue // 这行没变，不用管
		}
		fmt.Fprintf(&b, "\033[%d;1H\033[K%s", i+1, line)
	}
	s.prev = frame

	if curCol >= 0 {
		fmt.Fprintf(&b, "\033[%d;%dH\033[?25h", avail+curRow+1, curCol+1)
	} else {
		b.WriteString("\033[?25l")
	}
	b.WriteString("\033[?2026l")
	raw(b.String())
}

// bottomLocked 凑出钉在底下的那几行，并算出光标落点。
// 光标列返回 -1 表示这会儿没有输入框，把光标藏起来。
func (s *screen) bottomLocked() (lines []string, curRow, curCol int) {
	curCol = -1 // 一路没摆上输入框的话就是它：光标藏起来
	if s.confirmLines != nil {
		lines = append(lines, s.confirmLines()...)
	}
	if s.spinning {
		lines = append(lines, s.statusLine())
	}
	// 确认框亮着的时候按键是用来挑选项的，候选和输入框都收起来。
	if s.confirmLines == nil {
		if s.hintLines != nil {
			lines = append(lines, s.hintLines()...)
		}
		if s.inputLines != nil {
			il, r, c := s.inputLines()
			if len(il) > 0 {
				lines = append(lines, "") // 跟上面的东西空一行，别贴着
				curRow, curCol = len(lines)+r, c
				lines = append(lines, il...)
			}
		}
	}
	// 状态栏永远是最后一行，内容等滚动位置定下来再填。
	if s.statusFn != nil {
		lines = append(lines, "")
	}
	return lines, curRow, curCol
}

// statusLine 是转圈那一行：动画 + 动词 + 用时 + token + 打断提示。
func (s *screen) statusLine() string {
	sec := int(time.Since(s.start).Seconds())
	line := fmt.Sprintf("%s%s%s %s%s… %s(%ds", coral, spinFrames[s.frame%len(spinFrames)], reset,
		bold, s.verb, dim, sec)
	if s.tokens > 0 {
		line += fmt.Sprintf(" · ↑ %s tokens", human(s.tokens))
	}
	return line + " · esc 打断)" + reset
}

// startSpin 开始转圈。
func (s *screen) startSpin() {
	s.mu.Lock()
	if s.spinning {
		s.mu.Unlock()
		return
	}
	s.spinning = true
	s.frame = 0
	s.tokens = 0
	s.start = time.Now()
	s.verb = verbs[rand.Intn(len(verbs))]
	s.stop = make(chan struct{})
	stop := s.stop
	s.render()
	s.mu.Unlock()

	go func() {
		t := time.NewTicker(120 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.mu.Lock()
				if s.spinning {
					s.frame++
					s.render()
				}
				s.mu.Unlock()
			}
		}
	}()
}

// stopSpin 收起转圈。
func (s *screen) stopSpin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.spinning {
		return
	}
	s.spinning = false
	close(s.stop)
	s.render()
}

// setTokens 更新转圈那行上的 token 数。
func (s *screen) setTokens(n int) {
	s.mu.Lock()
	s.tokens = n
	s.mu.Unlock()
}

// clearHistory 清空翻得到的历史（/clear 用）。
func (s *screen) clearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
	s.offset = 0
	s.prev = nil
	raw("\033[2J")
	s.render()
}

// human 把 token 数压成 1.2k 这种写法。
func human(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// wrapANSI 按显示宽度折行，转义序列不计宽度也不被切断。
// 折行处会先收一个 reset，新行开头再把还生效的颜色补回去，
// 免得颜色在行尾漏出去染了后面的内容。
func wrapANSI(s string, w int) []string {
	if w < 8 {
		w = 8
	}
	if visWidth(s) <= w {
		return []string{s}
	}

	var (
		outs   []string
		cur    strings.Builder
		active []string // 还生效的颜色序列
		used   int
	)
	flush := func() {
		line := cur.String()
		if len(active) > 0 {
			line += reset
		}
		outs = append(outs, line)
		cur.Reset()
		used = 0
		for _, code := range active {
			cur.WriteString(code)
		}
	}

	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == 0x1b {
			// 整段转义序列原样搬过去，顺便记下颜色状态。
			j := i
			for j < len(rs) && rs[j] != 'm' && rs[j] != '\\' && rs[j] != 0x07 {
				j++
			}
			if j < len(rs) {
				j++
			}
			code := string(rs[i:j])
			cur.WriteString(code)
			if code == reset {
				active = active[:0]
			} else if strings.HasSuffix(code, "m") {
				active = append(active, code)
			}
			i = j - 1
			continue
		}
		cw := runewidth.RuneWidth(rs[i])
		if used+cw > w {
			flush()
		}
		cur.WriteRune(rs[i])
		used += cw
	}
	if cur.Len() > 0 {
		outs = append(outs, cur.String())
	}
	return outs
}

// padLeft 按显示宽度左侧补空格，用来把金额右对齐。
func padLeft(s string, w int) string { return runewidth.FillLeft(s, w) }

// pad 按显示宽度右侧补空格，超出就截断。
func pad(s string, w int) string {
	if runewidth.StringWidth(s) > w {
		return runewidth.Truncate(s, w, "…")
	}
	return runewidth.FillRight(s, w)
}
