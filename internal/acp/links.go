package acp

import (
	"regexp"
	"strings"
)

// Jarvis 的提示词让模型把提到的条目写成 [标题](jarvis://item/<ref>) 这类内部链接，
// 那是给自家界面展开详情卡片用的。ACP 客户端没有这个 scheme，留着就是一个
// 点了没反应的链接，所以只留标题——跟终端界面的处理一个意思（chat/markdown.go）。

var internalLink = regexp.MustCompile(`\[([^\[\]\n]*)\]\(jarvis://[^()\s]*\)`)

func stripLinks(s string) string { return internalLink.ReplaceAllString(s, "$1") }

// linkFilter 是流式版的 stripLinks：增量会把一个链接拆在好几段里，
// 从 `[` 起先攒着，确定不是内部链接（或者攒太长、遇到换行）就原样放行。
type linkFilter struct {
	buf strings.Builder
}

// maxHeld 是最多攒多少字节。标题加地址超过这个长度的不太会是内部链接，
// 攒着只会让正文卡住不动。
const maxHeld = 400

// feed 交进一段增量，返回现在可以推出去的部分。
func (f *linkFilter) feed(s string) string {
	var out strings.Builder
	for _, r := range s {
		if f.buf.Len() == 0 {
			if r == '[' {
				f.buf.WriteRune(r)
			} else {
				out.WriteRune(r)
			}
			continue
		}
		f.buf.WriteRune(r)
		held := f.buf.String()
		switch {
		case internalLink.MatchString(held):
			out.WriteString(stripLinks(held))
			f.buf.Reset()
		case !couldBeLink(held) || len(held) > maxHeld:
			// 攒着的那段不是链接：从第二个字起可能又是一个 `[`，重新过一遍。
			f.buf.Reset()
			out.WriteByte('[')
			out.WriteString(f.feed(held[1:]))
		}
	}
	return out.String()
}

// flush 一轮（或一步）结束，攒着的原样放出去。
func (f *linkFilter) flush() string {
	s := f.buf.String()
	f.buf.Reset()
	return s
}

// couldBeLink 判断攒着的前缀还有没有可能长成一个内部链接。
func couldBeLink(held string) bool {
	if strings.Contains(held, "\n") {
		return false
	}
	close := strings.Index(held, "]")
	if close < 0 {
		return !strings.Contains(held[1:], "[")
	}
	rest := held[close+1:]
	const prefix = "(jarvis://"
	if len(rest) <= len(prefix) {
		return strings.HasPrefix(prefix, rest)
	}
	return strings.HasPrefix(rest, prefix) && !strings.ContainsAny(rest[len(prefix):], " \t(")
}
