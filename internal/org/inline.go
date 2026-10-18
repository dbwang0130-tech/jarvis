package org

import (
	"regexp"
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
