package chat

import "strings"

// live 是正在流的那段回复。整屏模型下它不用自己管光标，也不用固化：
// 每来一点增量就把攒到现在的整段文本重新渲染一遍，交给 screen 画在
// 内容区末尾。于是「要看到后面才知道怎么画」的结构可以先画成半成品、
// 下一帧再改对——表格先显示原始管道符，等分隔行到了自己变成对齐的表格。
type live struct {
	prefix string          // 第一行的前缀，比如那个绿圆点
	text   strings.Builder // 攒到现在的原始 markdown
	on     bool

	// 每来一个字就把整段重渲一遍是 O(n²)——一篇长回复能烧掉上百毫秒、
	// 扔出几十兆垃圾，而其中 99% 是早就定稿、一个字都不会再变的行。
	// 所以定期切一刀：切点之前的渲染结果存进 doneOut，往后只渲染
	// 还没定稿的那一小段。
	doneN   int      // 已定稿的原文长度
	doneOut []string // 它渲染出来的行
	doneW   int      // 定稿时用的宽度，变了就全部作废
	// buf 是交出去那份行的复用缓冲。调用方只在持锁的一帧里读它，
	// 或者 freeze 时拷进历史，没人长期留着。
	buf []string
}

// start 开一段新正文，prefix 会挂在第一行前面。
func (l *live) start(prefix string) {
	l.text.Reset()
	l.prefix = prefix
	l.reset()
	l.on = true
}

func (l *live) reset() { l.doneN, l.doneOut, l.doneW = 0, nil, 0 }

// feed 吃进一点增量，并立刻重画。
func (l *live) feed(s string) {
	if !l.on {
		l.start("")
	}
	l.text.WriteString(s)
	scr.refresh()
}

// activeLines 渲染出当前全部行，第一行带上前缀。
func (l *live) activeLines() []string {
	if !l.on || l.text.Len() == 0 {
		return nil
	}
	w := frameWidth() - 1 - visWidth(l.prefix)
	if l.doneW != w {
		l.reset() // 宽度变了，之前渲染的行全部作废
		l.doneW = w
	}

	text := l.text.String()
	l.advance(text, w)

	l.buf = append(l.buf[:0], l.doneOut...)
	if rest := text[l.doneN:]; rest != "" {
		l.buf = append(l.buf, renderMarkdown(rest, w)...)
	}
	if len(l.buf) > 0 && l.prefix != "" {
		l.buf[0] = l.prefix + l.buf[0]
	}
	return l.buf
}

// advance 把「最后一个空行」之前的部分定稿。空行是 markdown 的天然接缝：
// 标题、列表、引用、表格都不跨空行，唯一会跨的是代码块，所以要绕开还没
// 闭合的围栏。
func (l *live) advance(text string, w int) {
	cut := lineCut(text[l.doneN:])
	if cut <= 0 {
		return
	}
	// 切点落在分界那个换行符之后，渲染时要把它排掉——否则这一段末尾
	// 和下一段开头会各自多出一个空行。
	l.doneOut = append(l.doneOut, renderMarkdown(text[l.doneN:l.doneN+cut-1], w)...)
	l.doneN += cut
}

// lineCut 返回这段文本里可以安全切开的位置（某个整行之后）。
//
// renderMarkdown 是逐行驱动的，真正跨行的结构只有代码围栏和表格，
// 所以别的行边界都能切。早先只在空行处切，遇到一大段不带空行的正文
// 就完全退回每帧重渲全文——8k 这样的文本要 116ms、扔 49MB。
//
// 切点只设在「不在围栏里」的位置，所以下一段总是从干净状态开始，
// 不用把状态跨帧带着走。没有可切的就返回 0。
func lineCut(s string) int {
	var cut, at int
	code, inTable := false, false
	for _, line := range strings.SplitAfter(s, "\n") {
		if !strings.HasSuffix(line, "\n") {
			break // 最后一行还没写完，不算
		}
		at += len(line)
		switch t := strings.TrimSpace(line); {
		case isFence(t):
			code, inTable = !code, false
			if !code {
				cut = at // 围栏闭合了，这儿可以切
			}
		case code: // 代码块里头不能切
		case isTableRow(line) || inTable:
			inTable = isTableRow(line) // 表格没走完，等它
		default:
			cut = at
		}
	}
	return cut
}

// freeze 收工：把这段正文定格进历史。
func (l *live) freeze() {
	if !l.on {
		return
	}
	lines := l.activeLines()
	l.text.Reset()
	l.reset()
	l.on = false
	scr.commit(lines)
}
