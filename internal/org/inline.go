package org

import (
	"regexp"
	"strings"
)

var (
	// [[链接][描述]] 与 [[链接]]
	linkRe = regexp.MustCompile(`\[\[([^\]\[]+)\](?:\[([^\]]*)\])?\]`)
	// org 的强调标记要求两侧有边界，避免把 a/b/c 这种误判成斜体。
	// Go 的正则没有反向引用，所以每种标记各编译一条。
	emphasisRes = compileEmphasis("*", "/", "_", "=", "~", "+")
)

// compileEmphasis 为每个强调符号生成一条「两侧有边界、内容不含该符号」的正则。
func compileEmphasis(marks ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(marks))
	for _, m := range marks {
		q := regexp.QuoteMeta(m)
		out = append(out, regexp.MustCompile(
			`(^|[\s\p{P}])`+q+`([^\s`+q+`](?:[^`+q+`]*[^\s`+q+`])?)`+q+`($|[\s\p{P}])`))
	}
	return out
}

// Link 是正文或标题里的一个链接。
type Link struct {
	// URL 是链接目标，可能是网址、文件路径或 id:/ 内部引用。
	URL string
	// Text 是展示文字，没写描述时与 URL 相同。
	Text string
}

// Links 提取文本里的全部链接。
func Links(s string) []Link {
	var out []Link
	for _, m := range linkRe.FindAllStringSubmatch(s, -1) {
		l := Link{URL: m[1], Text: m[2]}
		if l.Text == "" {
			l.Text = m[1]
		}
		out = append(out, l)
	}
	return out
}

// Plain 把 org 的行内标记转成适合终端显示的纯文本：
// 链接只留描述，强调标记去掉符号。
func Plain(s string) string {
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		g := linkRe.FindStringSubmatch(m)
		if g[2] != "" {
			return g[2]
		}
		return g[1]
	})
	// 相邻的强调会共用分隔符，跑两遍才能都去掉。
	for i := 0; i < 2; i++ {
		before := s
		for _, re := range emphasisRes {
			s = re.ReplaceAllString(s, "$1$2$3")
		}
		if s == before {
			break
		}
	}
	return s
}

// IsHeadline 报告一行是否是 org 标题行。
func IsHeadline(line string) bool {
	return strings.HasPrefix(line, "*") && headlineRe.MatchString(line)
}
