package org

import "testing"

func TestPlain(t *testing.T) {
	cases := map[string]string{
		"[[https://example.com][示例站点]]":       "示例站点",
		"[[https://example.com]]":             "https://example.com",
		"看 [[file:notes.org][笔记]] 里的说明":       "看 笔记 里的说明",
		"*很重要* 的事":                            "很重要 的事",
		"用 =org-agenda= 看日程":                  "用 org-agenda 看日程",
		"~code~ 和 /斜体/":                       "code 和 斜体",
		"路径 a/b/c 不该被当成斜体":                    "路径 a/b/c 不该被当成斜体",
		"2026/09/14 也不该":                      "2026/09/14 也不该",
		"没有标记的普通文本":                           "没有标记的普通文本",
		"[[id:abc-123][跳转]]和[[https://x][x]]": "跳转和x",
	}
	for in, want := range cases {
		if got := Plain(in); got != want {
			t.Errorf("Plain(%q) = %q，期望 %q", in, got, want)
		}
	}
}
