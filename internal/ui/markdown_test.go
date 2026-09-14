package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// plain 去掉样式，只比较文字内容。
func plain(lines []string) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(stripANSI(l))
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestMarkdownBlocks(t *testing.T) {
	in := `## 本周安排

已经帮你排好了：

- **周二下午** 写季度汇报
- 周三 *体检*
1. 先做这个
2. 再做那个

> 周四没空档了

---

最后一行`
	got := plain(renderMarkdown(in))
	for _, want := range []string{
		"本周安排", "• 周二下午 写季度汇报", "• 周三 体检",
		"1. 先做这个", "2. 再做那个", "│ 周四没空档了", "最后一行",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"##", "**", "- ", "> "} {
		if strings.Contains(got, unwanted) {
			t.Errorf("标记 %q 不该留在输出里:\n%s", unwanted, got)
		}
	}
	if !strings.Contains(got, "────") {
		t.Errorf("分隔线未渲染:\n%s", got)
	}
}

func TestMarkdownInline(t *testing.T) {
	cases := map[string]string{
		"用 `orgtui -print` 看看":      "用 orgtui -print 看看",
		"这是 **重点** 内容":              "这是 重点 内容",
		"这是 *斜的*":                   "这是 斜的",
		"~~不要了~~":                   "不要了",
		"见 [org 手册](https://x.org)": "见 org 手册 https://x.org",
		"`a * b` 里的星号不该被当成强调":       "a * b 里的星号不该被当成强调",
		"路径 a/b_c_d 不该被拆":           "路径 a/b_c_d 不该被拆",
	}
	for in, want := range cases {
		if got := stripANSI(renderInlineMD(in)); got != want {
			t.Errorf("renderInlineMD(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestMarkdownCodeFence(t *testing.T) {
	in := "试试这个：\n```sh\norgtui -print -days 1\n```\n就这样"
	got := plain(renderMarkdown(in))
	if !strings.Contains(got, "orgtui -print -days 1") {
		t.Errorf("代码块内容丢失:\n%s", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("围栏标记不该保留:\n%s", got)
	}
	if !strings.Contains(got, "sh") {
		t.Errorf("语言标注应显示:\n%s", got)
	}

	// 流式过程中围栏还没闭合，也要能正常渲染
	partial := plain(renderMarkdown("试试：\n```sh\norgtui -pri"))
	if !strings.Contains(partial, "orgtui -pri") {
		t.Errorf("未闭合的代码块应照常显示:\n%s", partial)
	}
}

func TestMarkdownKeepsStyles(t *testing.T) {
	// 渲染后应带上样式，而不是只剩纯文本
	out := renderInlineMD("这是 **重点**")
	if lipgloss.Width(out) != lipgloss.Width("这是 重点") {
		t.Errorf("显示宽度不该因为样式改变: %q", out)
	}
}
