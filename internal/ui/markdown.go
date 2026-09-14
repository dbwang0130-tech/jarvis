package ui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 模型的回复里常带一点 markdown，这里做最小够用的渲染：
// 标题、列表、引用、代码块，以及粗体、斜体、行内代码、链接和删除线。
var (
	mdHeading  = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdBullet   = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdOrdered  = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
	mdQuote    = regexp.MustCompile(`^\s*>\s?(.*)$`)
	mdRule     = regexp.MustCompile(`^\s*(?:-{3,}|\*{3,}|_{3,})\s*$`)
	mdFence    = regexp.MustCompile("^\\s*```\\s*(\\w*)\\s*$")
	mdCode     = regexp.MustCompile("`([^`]+)`")
	mdBoldStar = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdBoldUnd  = regexp.MustCompile(`__([^_]+)__`)
	mdItalStar = regexp.MustCompile(`\*([^*\s][^*]*)\*`)
	mdItalUnd  = regexp.MustCompile(`(^|[\s\p{P}])_([^_\s][^_]*)_($|[\s\p{P}])`)
	mdStrike   = regexp.MustCompile(`~~([^~]+)~~`)
	mdLink     = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
)

var (
	styMDHeading = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styMDCode    = lipgloss.NewStyle().Foreground(colToday)
	styMDBold    = lipgloss.NewStyle().Bold(true)
	styMDItalic  = lipgloss.NewStyle().Italic(true)
	styMDStrike  = lipgloss.NewStyle().Strikethrough(true).Foreground(colDim)
	styMDQuote   = lipgloss.NewStyle().Foreground(colDim).Italic(true)
	styMDBullet  = lipgloss.NewStyle().Foreground(colAccent)
)

// renderMarkdown 把一段 markdown 渲染成若干终端行。
// 未闭合的代码块按已打开处理，方便流式输出时逐字显示。
func renderMarkdown(text string) []string {
	var out []string
	inFence := false

	for _, line := range strings.Split(text, "\n") {
		if m := mdFence.FindStringSubmatch(line); m != nil {
			inFence = !inFence
			if inFence && m[1] != "" {
				out = append(out, styDim.Render("  "+m[1]))
			}
			continue
		}
		if inFence {
			out = append(out, styMDCode.Render("  "+line))
			continue
		}

		switch {
		case strings.TrimSpace(line) == "":
			out = append(out, "")
		case mdRule.MatchString(line):
			out = append(out, styDim.Render(strings.Repeat("─", 20)))
		case mdHeading.MatchString(line):
			m := mdHeading.FindStringSubmatch(line)
			out = append(out, styMDHeading.Render(renderInlineMD(m[2])))
		case mdQuote.MatchString(line):
			m := mdQuote.FindStringSubmatch(line)
			out = append(out, styMDQuote.Render("│ "+renderInlineMD(m[1])))
		case mdBullet.MatchString(line):
			m := mdBullet.FindStringSubmatch(line)
			out = append(out, m[1]+styMDBullet.Render("• ")+renderInlineMD(m[2]))
		case mdOrdered.MatchString(line):
			m := mdOrdered.FindStringSubmatch(line)
			out = append(out, m[1]+styMDBullet.Render(m[2]+" ")+renderInlineMD(m[3]))
		default:
			out = append(out, renderInlineMD(line))
		}
	}
	return out
}

// renderInlineMD 渲染行内标记。先把行内代码换成占位符，
// 免得代码里的星号被当成强调。
func renderInlineMD(s string) string {
	var codes []string
	s = mdCode.ReplaceAllStringFunc(s, func(m string) string {
		codes = append(codes, mdCode.FindStringSubmatch(m)[1])
		return fmt.Sprintf("\x00%d\x00", len(codes)-1)
	})

	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		g := mdLink.FindStringSubmatch(m)
		return styAccent.Render(g[1]) + styDim.Render(" "+g[2])
	})
	s = mdBoldStar.ReplaceAllStringFunc(s, func(m string) string {
		return styMDBold.Render(mdBoldStar.FindStringSubmatch(m)[1])
	})
	s = mdBoldUnd.ReplaceAllStringFunc(s, func(m string) string {
		return styMDBold.Render(mdBoldUnd.FindStringSubmatch(m)[1])
	})
	s = mdStrike.ReplaceAllStringFunc(s, func(m string) string {
		return styMDStrike.Render(mdStrike.FindStringSubmatch(m)[1])
	})
	s = mdItalStar.ReplaceAllStringFunc(s, func(m string) string {
		return styMDItalic.Render(mdItalStar.FindStringSubmatch(m)[1])
	})
	s = mdItalUnd.ReplaceAllStringFunc(s, func(m string) string {
		g := mdItalUnd.FindStringSubmatch(m)
		return g[1] + styMDItalic.Render(g[2]) + g[3]
	})

	for i, c := range codes {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00%d\x00", i), styMDCode.Render(c))
	}
	return s
}
