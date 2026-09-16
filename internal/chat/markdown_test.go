package chat

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

const mdDoc = "# 今天的安排\n\n" +
	"先生，我把**要紧的**三件事挑出来了：\n\n" +
	"- 上午和王老师开会，材料在 `~/org/thesis.org`\n" +
	"- 下午补开题报告\n\n" +
	"> 房租今天到期。\n\n" +
	"参考 [org 手册](https://orgmode.org)。\n\n" +
	"```sh\ngit push origin main\n```\n\n" +
	"---\n"

func renderPlain(text string) string {
	lines := renderMarkdown(text, 76)
	return ansiRe.ReplaceAllString(strings.Join(lines, "\n"), "")
}

func TestMarkdownBlocks(t *testing.T) {
	got := renderPlain(mdDoc)

	for _, gone := range []string{"# 今天", "**要紧的**", "`~/org", "> 房租", "```", "[org 手册]"} {
		if strings.Contains(got, gone) {
			t.Fatalf("%q 没被渲染掉：\n%s", gone, got)
		}
	}
	for _, want := range []string{"今天的安排", "要紧的", "• 上午和王老师开会", "│ 房租今天到期。",
		"sh", "git push origin main", "org 手册"} {
		if !strings.Contains(got, want) {
			t.Fatalf("少了 %q：\n%s", want, got)
		}
	}
	// 链接要挂上 OSC 8，点得开。
	if !strings.Contains(strings.Join(renderMarkdown(mdDoc, 76), ""), "\x1b]8;;https://orgmode.org") {
		t.Fatal("链接没做成超链接")
	}
}

const tableDoc = "这是本月开销：\n" +
	"| 项目 | 金额 | 备注 |\n" +
	"| --- | --- | --- |\n" +
	"| 房租 | 3000 | 已付 |\n" +
	"| 吃饭 | 1200 |  |\n" +
	"就这些。\n"

func TestTableIsAligned(t *testing.T) {
	lines := renderMarkdown(tableDoc, 76)
	var rows []string
	for _, l := range lines {
		if strings.Contains(l, "│") {
			rows = append(rows, l)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("表格行数不对：%d\n%s", len(rows), renderPlain(tableDoc))
	}
	w := lastBarCol(rows[0])
	for i, r := range rows {
		if got := lastBarCol(r); got != w {
			t.Fatalf("第 %d 行的竖线在第 %d 列，首行在第 %d 列：%q",
				i, got, w, ansiRe.ReplaceAllString(r, ""))
		}
	}
	for _, r := range rows {
		if plain := ansiRe.ReplaceAllString(r, ""); strings.HasSuffix(plain, " ") {
			t.Fatalf("行尾有多余空格：%q", plain)
		}
	}
	if !strings.Contains(renderPlain(tableDoc), "─┼─") {
		t.Fatal("表头下面应该有一条分隔线")
	}
}

// lastBarCol 返回一行里最后一根竖线所在的显示列。
func lastBarCol(line string) int {
	plain := ansiRe.ReplaceAllString(line, "")
	i := strings.LastIndex(plain, "│")
	if i < 0 {
		return -1
	}
	return visWidth(plain[:i])
}

// 这是重画方案的关键：分隔行还没到时，表格就先按原样显示，
// 等它到了，下一帧自己变成对齐的表格。
func TestTableTakesShapeWhenSeparatorArrives(t *testing.T) {
	half := renderPlain("| 项目 | 金额 |\n")
	if !strings.Contains(half, "| 项目 | 金额 |") {
		t.Fatalf("还没看到分隔行时应当原样显示：%q", half)
	}
	whole := renderPlain("| 项目 | 金额 |\n| --- | --- |\n| 房租 | 3000 |\n")
	if strings.Contains(whole, "| 项目") {
		t.Fatalf("分隔行到了就该变成表格：%q", whole)
	}
	if !strings.Contains(whole, "─┼─") {
		t.Fatalf("没画出表头分隔线：%q", whole)
	}
}

// 标记没闭合时先原样显示，闭合了下一帧就变粗体。
func TestUnclosedMarkShowsRawThenRenders(t *testing.T) {
	if got := renderPlain("看这里 **重点"); !strings.Contains(got, "**重点") {
		t.Fatalf("没闭合的标记该原样留着：%q", got)
	}
	got := renderMarkdown("看这里 **重点**", 76)
	if strings.Contains(ansiRe.ReplaceAllString(got[0], ""), "**") {
		t.Fatalf("闭合之后该渲染成粗体：%q", got[0])
	}
	if !strings.Contains(got[0], bold) {
		t.Fatalf("没有加粗：%q", got[0])
	}
}

// 孤零零的星号不是强调，别乱改。
func TestLoneAsteriskUntouched(t *testing.T) {
	if got := renderPlain("另外 3 * 5 我核对过了。"); !strings.Contains(got, "3 * 5") {
		t.Fatalf("孤星号被动了：%q", got)
	}
}

func TestLongLineWraps(t *testing.T) {
	long := strings.Repeat("中文", 60) // 120 列宽
	lines := renderMarkdown(long, 40)
	if len(lines) < 3 {
		t.Fatalf("长行没折：%d 行", len(lines))
	}
	for i, l := range lines {
		if w := visWidth(l); w > 40 {
			t.Fatalf("第 %d 行宽 %d，超出 40：%q", i, w, l)
		}
	}
}

// 折行不能把颜色漏到后面的内容上。
func TestWrapKeepsColorsBalanced(t *testing.T) {
	lines := wrapANSI(bold+strings.Repeat("ab", 40)+reset, 30)
	if len(lines) < 2 {
		t.Fatalf("没折行：%v", lines)
	}
	for i, l := range lines[:len(lines)-1] {
		if !strings.HasSuffix(l, reset) {
			t.Fatalf("第 %d 行结尾没收住颜色：%q", i, l)
		}
	}
	for i, l := range lines[1:] {
		if !strings.HasPrefix(l, bold) {
			t.Fatalf("第 %d 行开头没把颜色接上：%q", i+1, l)
		}
	}
}

// 列表折行后要缩进对齐正文，不能顶到行首。
func TestListItemHangingIndent(t *testing.T) {
	lines := renderMarkdown("- "+strings.Repeat("很长的内容", 12), 40)
	if len(lines) < 2 {
		t.Fatalf("没折行：%v", lines)
	}
	plain := ansiRe.ReplaceAllString(lines[1], "")
	if !strings.HasPrefix(plain, "  ") {
		t.Fatalf("折下来的行没缩进：%q", plain)
	}
}

// visWidth 手写扫描要跟"剥掉转义再量"的结果一致，OSC 8 超链接也不能漏。
func TestVisWidthMatchesStrippedWidth(t *testing.T) {
	cases := []string{
		"普通文字",
		"abc",
		"",
		bold + "加粗" + reset,
		dim + "a" + coral + "b" + reset + "c",
		osc8("https://example.com/m?x=1", "带网址的链接"),
		gray + "│ " + reset + coral + "> " + reset + "混着框线",
		"\x1b[38;2;1;2;3m真彩色\x1b[0m",
	}
	for _, c := range cases {
		want := runewidth.StringWidth(ansiRe.ReplaceAllString(c, ""))
		if got := visWidth(c); got != want {
			t.Errorf("visWidth(%q) = %d，剥掉转义量出来是 %d", c, got, want)
		}
	}
}

// 增量渲染的切点：不管在哪儿切，拼出来的结果都要和整段渲染逐字节一致。
func TestLineCutMatchesWholeRender(t *testing.T) {
	docs := map[string]string{
		"没有空行":  "- 甲\n- 乙\n- 丙\n普通一行\n# 标题\n> 引用\n",
		"带表格":   "开头\n\n| 项目 | 金额 |\n| --- | --- |\n| 房租 | 3000 |\n\n结尾\n",
		"带代码块":  "说明\n\n```sh\ngit push\n\n还有空行\n```\n\n后面\n",
		"未闭合围栏": "说明\n\n```go\nfunc f() {\n",
		"混合":    "# 标\n\n- 甲\n\n```py\nx=1\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n尾\n",
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			cut := lineCut(doc)
			if cut == 0 {
				return // 没有可切的，本身就没问题
			}
			got := append(renderMarkdown(doc[:cut-1], 76), renderMarkdown(doc[cut:], 76)...)
			want := renderMarkdown(doc, 76)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("在第 %d 字节切开后结果不一致\n切开：\n%s\n整段：\n%s",
					cut, strings.Join(got, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// 代码围栏和表格中间不能切。
func TestLineCutAvoidsBlocks(t *testing.T) {
	// 围栏还没闭合，切点只能停在围栏之前。
	doc := "开头\n```go\nx := 1\ny := 2\n"
	if cut := lineCut(doc); cut > len("开头\n") {
		t.Fatalf("切进代码块里了：cut=%d", cut)
	}
	// 表格还在写，切点不能落在表格中间。
	doc = "开头\n| a | b |\n| - | - |\n"
	if cut := lineCut(doc); cut > len("开头\n") {
		t.Fatalf("切进表格里了：cut=%d", cut)
	}
}
