package chat

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

// fakeScreen 是个只认几种序列的迷你终端：光标上移、回车、清到屏幕底。
// live 重画时用的就是这几样，拿它把输出跑一遍，就能看出光标数学对不对。
type fakeScreen struct {
	lines []string
	row   int
	col   int
}

var (
	simCSI = regexp.MustCompile(`^\x1b\[([?0-9;]*)([A-Za-z])`)
	simOSC = regexp.MustCompile(`^\x1b\][^\x1b\a]*(\x1b\\|\a)`)
)

func (s *fakeScreen) write(data string) {
	for i := 0; i < len(data); {
		if data[i] == 0x1b {
			if m := simCSI.FindStringSubmatch(data[i:]); m != nil {
				if strings.HasPrefix(m[1], "?") {
					i += len(m[0]) // 藏/显光标这类私有序列，不动内容
					continue
				}
				n := 1
				if first, _, _ := strings.Cut(m[1], ";"); first != "" {
					n = 0
					for _, c := range first {
						n = n*10 + int(c-'0')
					}
				}
				switch m[2] {
				case "H", "f": // 绝对定位 ESC[行;列H
					r, c := 1, 1
					if parts := strings.Split(m[1], ";"); m[1] != "" {
						r = atoiOr(parts[0], 1)
						if len(parts) > 1 {
							c = atoiOr(parts[1], 1)
						}
					}
					s.row, s.col = r-1, c-1
					s.grow()
				case "A":
					if s.row -= n; s.row < 0 {
						s.row = 0
					}
				case "J":
					s.clearToEnd()
				case "K":
					s.clearLine()
				}
				// 其余（颜色之类）不动光标。
				i += len(m[0])
				continue
			}
			if m := simOSC.FindString(data[i:]); m != "" {
				i += len(m)
				continue
			}
			i += 2
			continue
		}
		switch data[i] {
		case '\r':
			s.col = 0
		case '\n':
			s.row++
			s.grow()
		default:
			r, size := decodeRune(data[i:])
			s.grow()
			s.putRune(r)
			i += size - 1
		}
		i++
	}
}

func atoiOr(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return def
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return 0, 1
}

func (s *fakeScreen) grow() {
	for len(s.lines) <= s.row {
		s.lines = append(s.lines, "")
	}
}

func (s *fakeScreen) putRune(r rune) {
	line := []rune(s.lines[s.row])
	for len(line) < s.col {
		line = append(line, ' ')
	}
	if s.col < len(line) {
		line[s.col] = r
	} else {
		line = append(line, r)
	}
	s.lines[s.row] = string(line)
	s.col++
}

func (s *fakeScreen) clearToEnd() {
	s.grow()
	line := []rune(s.lines[s.row])
	if s.col < len(line) {
		s.lines[s.row] = string(line[:s.col])
	}
	s.lines = s.lines[:s.row+1]
}

func (s *fakeScreen) clearLine() {
	s.grow()
	line := []rune(s.lines[s.row])
	if s.col < len(line) {
		s.lines[s.row] = string(line[:s.col])
	}
}

// screenText 把整屏按行返回（去掉颜色），不足的行补空串。
func (s *fakeScreen) screenText(h int) []string {
	outs := make([]string, h)
	for i := 0; i < h; i++ {
		if i < len(s.lines) {
			outs[i] = strings.TrimRight(ansiRe.ReplaceAllString(s.lines[i], ""), " ")
		}
	}
	return outs
}

// streamOnto 把一段文本一个字一个字喂给 live，每帧都画到模拟屏上。
func streamOnto(t *testing.T, text string, chunk int) *harness {
	t.Helper()
	h := fresh(t)
	l := &live{}
	scr.liveLines = l.activeLines
	l.start("")

	rs := []rune(text)
	for i := 0; i < len(rs); i += chunk {
		j := i + chunk
		if j > len(rs) {
			j = len(rs)
		}
		l.feed(string(rs[i:j]))
		h.screen() // 每帧都补画到模拟屏上
	}
	return h
}

// 一个字一个字流完之后，屏幕上该是渲染好的最终样子——
// 中间画出去的半成品都得被后面的帧盖掉。
func TestStreamedOutputMatchesFinalRender(t *testing.T) {
	for _, chunk := range []int{1, 2, 5, 17} {
		got := strings.Join(streamOnto(t, mdDoc, chunk).screen(), "\n")
		got = strings.TrimRight(strings.TrimLeft(got, "\n"), "\n ")

		want := strings.Join(renderMarkdown(mdDoc, width()-1), "\n")
		want = strings.TrimRight(ansiRe.ReplaceAllString(want, ""), "\n ")

		if got != want {
			t.Fatalf("按 %d 个字符流完之后屏幕对不上\n屏幕：\n%s\n应为：\n%s", chunk, got, want)
		}
	}
}

// 表格更能说明问题：中途是原始管道符，流完必须是对齐的表格。
func TestStreamedTableEndsAligned(t *testing.T) {
	plain := strings.Join(streamOnto(t, tableDoc, 1).screen(), "\n")

	if strings.Contains(plain, "| 项目 | 金额 |") {
		t.Fatalf("流完了还留着半成品：\n%s", plain)
	}
	if !strings.Contains(plain, "─┼─") {
		t.Fatalf("表格没成形：\n%s", plain)
	}
	if !strings.Contains(plain, "就这些。") {
		t.Fatalf("表格后面的正文丢了：\n%s", plain)
	}
	// 表格前面的正文也得还在。
	if !strings.Contains(plain, "这是本月开销：") {
		t.Fatalf("表格前面的正文被盖掉了：\n%s", plain)
	}
}

// 走一遍完整的一轮：转圈 → 调工具 → 回结果 → 正文（带表格）→ 收尾，
// 看屏幕最后留下的是不是该有的样子。
func TestOneTurnOnScreen(t *testing.T) {
	h := fresh(t)
	s := &Session{ag: agentForTest()}
	r := testReader()
	scr.liveLines = s.live.activeLines
	scr.inputLines, scr.hintLines = r.inputLines, r.hintLines
	scr.statusFn = func() string { return "状态栏" }
	flush := func() { h.screen() }

	scr.startSpin()
	flush()
	s.emit(agentEvent("tool_call", "list_todos", "查看待办"))
	flush()
	s.emit(agentEvent("tool_result", "list_todos", "3 条未完成"))
	flush()

	body := "先生，三件事：\n\n| 事项 | 截止 |\n| --- | --- |\n| 开题报告 | 周五 |\n\n就这些。"
	for _, r := range body {
		s.emit(agentEvent("delta", "", string(r)))
		flush()
	}
	s.live.freeze()
	scr.stopSpin()
	flush()

	got := h.text()
	for _, want := range []string{"查看待办", "3 条未完成", "先生，三件事：", "─┼─", "开题报告", "就这些。", "状态栏"} {
		if !strings.Contains(got, want) {
			t.Fatalf("屏幕上少了 %q：\n%s", want, got)
		}
	}
	for _, gone := range []string{"| 事项 | 截止 |", "esc 打断"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q 该被盖掉却还在：\n%s", gone, got)
		}
	}
	if strings.Index(got, "查看待办") > strings.Index(got, "先生，三件事") {
		t.Fatalf("工具行跑到正文后面去了：\n%s", got)
	}
	// 输入框始终钉在最底下。
	lines := h.screen()
	if !strings.Contains(lines[len(lines)-1], "状态栏") {
		t.Fatalf("最后一行该是状态栏：%q", lines[len(lines)-1])
	}
}

func agentForTest() *agent.Agent { return &agent.Agent{} }

// agentEvent 造一个进度事件。
func agentEvent(kind, tool, text string) agent.Event {
	e := agent.Event{Tool: tool, Text: text}
	switch kind {
	case "delta":
		e.Kind = agent.EventDelta
	case "tool_call":
		e.Kind = agent.EventToolCall
	case "tool_result":
		e.Kind = agent.EventToolResult
	}
	return e
}
