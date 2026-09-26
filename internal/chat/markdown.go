package chat

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// 正文放在一块可以反复重画的区域里（见 live），所以这里不用猜、
// 不用扣：每来一点增量就把攒到现在的整段文本重新渲染一遍，
// 画错了下一帧改回来。表格因此可以先显示成半成品，等分隔行到了
// 再变成对齐的样子。
func renderMarkdown(text string, w int) []string {
	var outs []string
	lines := strings.Split(text, "\n")
	inCode := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)

		// 代码块：围栏本身不打印，开栏时把语言名标出来。
		if isFence(t) {
			if !inCode {
				if lang := strings.TrimSpace(strings.TrimPrefix(t, "```")); lang != "" {
					outs = append(outs, dim+"  "+lang+reset)
				}
			}
			inCode = !inCode
			continue
		}
		if inCode {
			outs = append(outs, cyan+"  "+line+reset)
			continue
		}

		// 表格：表头 + 分隔行 + 若干数据行，攒齐了一起画。
		if isTableRow(line) && i+1 < len(lines) && isTableSep(lines[i+1]) {
			rows := []string{line, lines[i+1]}
			i++
			for i+1 < len(lines) && isTableRow(lines[i+1]) {
				i++
				rows = append(rows, lines[i])
			}
			outs = append(outs, renderTable(rows, w)...)
			continue
		}

		outs = append(outs, renderLine(line, w)...)
	}
	return outs
}

// renderLine 画一个普通行：标题、引用、分隔线、列表或段落。
func renderLine(line string, w int) []string {
	t := strings.TrimSpace(line)
	if t == "" {
		return []string{""}
	}

	if m := mdHeading.FindStringSubmatch(t); m != nil {
		text := bold + inlineMD(m[2]) + reset
		if len(m[1]) <= 2 {
			text = bold + cyan + inlineMD(m[2]) + reset
		}
		return wrapANSI(text, w)
	}
	if m := mdQuote.FindStringSubmatch(t); m != nil {
		var outs []string
		for _, l := range wrapANSI(dim+inlineMD(m[1])+reset, w-2) {
			outs = append(outs, gray+"│ "+reset+l)
		}
		return outs
	}
	if mdRule.MatchString(t) {
		n := w - 4
		if n > 60 {
			n = 60
		}
		if n < 4 {
			n = 4
		}
		return []string{gray + "  " + strings.Repeat("─", n) + reset}
	}
	if m := mdBullet.FindStringSubmatch(line); m != nil {
		return hangingIndent(m[1]+coral+"• "+reset, m[2], w)
	}
	if m := mdOrdered.FindStringSubmatch(line); m != nil {
		return hangingIndent(m[1]+coral+m[2]+" "+reset, m[3], w)
	}
	return wrapANSI(inlineMD(line), w)
}

// hangingIndent 画列表项：首行带标记，折下来的行缩进对齐正文。
func hangingIndent(prefix, body string, w int) []string {
	pw := visWidth(prefix)
	wrapped := wrapANSI(inlineMD(body), w-pw)
	outs := make([]string, 0, len(wrapped))
	for i, l := range wrapped {
		if i == 0 {
			outs = append(outs, prefix+l)
			continue
		}
		outs = append(outs, strings.Repeat(" ", pw)+l)
	}
	return outs
}

var mdTableSep = regexp.MustCompile(`^:?-{1,}:?$`)

// isFence 判断一行是不是代码围栏。live 切增量渲染的刀口时也要认它，
// 所以定义放这儿共用——哪天这里认了别的围栏写法，那边跟着就对。
func isFence(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "```")
}

// isTableRow 判断一行是不是表格行（以 | 开头、以 | 结尾）。
func isTableRow(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) > 1 && strings.HasPrefix(t, "|") && strings.HasSuffix(t, "|")
}

// isTableSep 判断一行是不是 |---|---| 这样的分隔行。
func isTableSep(line string) bool {
	cells := splitRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if !mdTableSep.MatchString(strings.TrimSpace(c)) {
			return false
		}
	}
	return true
}

// splitRow 把一行按 | 拆成单元格。
func splitRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimSuffix(strings.TrimPrefix(t, "|"), "|")
	parts := strings.Split(t, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// renderTable 把攒下来的原始行渲染成对齐的表格。
// 没有分隔行的就不算表格，原样还回去。
func renderTable(lines []string, w int) []string {
	if len(lines) < 2 || !isTableSep(lines[1]) {
		return lines
	}
	var rows [][]string
	for i, l := range lines {
		if i == 1 {
			continue // 分隔行不参与
		}
		cells := splitRow(l)
		for j := range cells {
			cells[j] = inlineMD(cells[j])
		}
		rows = append(rows, cells)
	}

	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	widths := make([]int, cols)
	for _, r := range rows {
		for i, c := range r {
			if w := visWidth(c); w > widths[i] {
				widths[i] = w
			}
		}
	}
	fitWidths(widths, w)

	var outs []string
	for ri, r := range rows {
		cells := make([]string, cols)
		for i := range cells {
			c := ""
			if i < len(r) {
				c = truncVis(r[i], widths[i])
			}
			// 最后一列不补空格，免得每行拖一串尾巴，复制出去还得自己删。
			if i < cols-1 {
				c = padVis(c, widths[i])
			}
			if ri == 0 {
				c = bold + c + reset
			}
			cells[i] = c
		}
		// 末列为空时分隔符会剩个尾巴，一并剪掉。
		outs = append(outs, trimVisRight("  "+strings.Join(cells, gray+" │ "+reset)))
		if ri == 0 {
			seps := make([]string, cols)
			for i := range seps {
				seps[i] = strings.Repeat("─", widths[i])
			}
			outs = append(outs, gray+"  "+strings.Join(seps, "─┼─")+reset)
		}
	}
	return outs
}

// fitWidths 在表格超出终端宽度时，从最宽的列开始削，直到装得下。
func fitWidths(widths []int, w int) {
	avail := w - 4 - 3*(len(widths)-1) // 缩进 + 列间的 " │ "
	if avail < len(widths)*4 {
		avail = len(widths) * 4
	}
	for {
		total := 0
		widest := 0
		for i, w := range widths {
			total += w
			if w > widths[widest] {
				widest = i
			}
		}
		if total <= avail || widths[widest] <= 4 {
			return
		}
		widths[widest]--
	}
}

// ansiRe 匹配颜色序列和 OSC 8 超链接，算宽度时它们都不占位置。
var ansiRe = regexp.MustCompile("\\x1b\\[[0-9;]*m|\\x1b\\]8;;[^\\x1b\\x07]*(\\x1b\\\\|\\x07)")

// visWidth 是去掉转义序列之后的显示宽度。
//
// 手写扫描而不是走正则：它在每条历史行、每次折行、每帧状态栏上都要跑，
// 正则版每次还要建一个新串——实测占了单帧渲染的三分之一。
func visWidth(s string) int {
	w := 0
	for i := 0; i < len(s); {
		switch {
		case s[i] == 0x1b:
			i = skipEsc(s, i)
		case s[i] < utf8.RuneSelf:
			w++
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			w += runewidth.RuneWidth(r)
			i += n
		}
	}
	return w
}

// skipEsc 跳过一段转义序列，返回它之后的下标。
// CSI（ESC[…字母）和 OSC（ESC]…BEL 或 ESC\）要分开认——OSC 8 超链接的
// 网址里常有字母，按 CSI 那样找第一个字母当结尾会截错地方。
func skipEsc(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[':
		for j := i + 2; j < len(s); j++ {
			if c := s[j]; (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
				return j + 1
			}
		}
	case ']':
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
	default:
		return i + 2 // 别的两字节序列
	}
	return len(s)
}

// padVis 按显示宽度右侧补空格，忽略颜色序列。
func padVis(s string, w int) string {
	if d := w - visWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// tailBlanksRe 匹配行尾那串「空格和颜色序列的混合」。
var tailBlanksRe = regexp.MustCompile(`(?: |\x1b\[[0-9;]*m)+$`)

// trimVisRight 剪掉行尾看得见的空格，夹在中间的颜色序列原样留着
// （直接 TrimRight 会被末尾的 reset 挡住）。
func trimVisRight(s string) string {
	m := tailBlanksRe.FindStringIndex(s)
	if m == nil {
		return s
	}
	return s[:m[0]] + strings.ReplaceAll(s[m[0]:], " ", "")
}

// truncVis 按显示宽度截断，忽略颜色序列；截断时补一个省略号。
func truncVis(s string, w int) string {
	if visWidth(s) <= w {
		return s
	}
	plain := ansiRe.ReplaceAllString(s, "")
	return runewidth.Truncate(plain, w, "…")
}

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdQuote   = regexp.MustCompile(`^>\s?(.*)$`)
	mdRule    = regexp.MustCompile(`^(?:-{3,}|\*{3,}|_{3,})$`)
	mdBullet  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdOrdered = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	mdBold    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdCode    = regexp.MustCompile("`([^`]+)`")
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	mdStrike  = regexp.MustCompile(`~~([^~]+)~~`)
)

// osc8 用 OSC 8 把文字变成可点的超链接。不认这个序列的终端
// 会把它忽略掉，只留下文字，不会漏出乱码。
func osc8(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + underline + text + reset + "\x1b]8;;\x1b\\"
}

// inlineMD 渲染单元格里的行内标记。表格是攒齐了才画的，
// 所以这里可以放心地把 **粗体** 这类标记换成转义序列。
func inlineMD(s string) string {
	s = mdCode.ReplaceAllString(s, cyan+"$1"+reset)
	s = mdBold.ReplaceAllString(s, bold+"$1"+reset)
	s = mdStrike.ReplaceAllString(s, dim+"$1"+reset)
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		p := mdLink.FindStringSubmatch(m)
		// jarvis:// 是给手机界面用的内部链接（jarvis://item/<ref>、
		// jarvis://mail/<编号>）：那边点一下会展开详情卡片。桌面上没有这个
		// scheme 的处理程序，发成 OSC 8 就是一个点了没反应的链接，有些终端
		// 还会把这串地址显示在状态栏上。只留下划线——它在这儿的作用就剩
		// 「这几个字指的是一条具体的东西」。
		if strings.HasPrefix(p[2], "jarvis://") {
			return underline + p[1] + reset
		}
		return osc8(p[2], p[1])
	})
	return s
}
