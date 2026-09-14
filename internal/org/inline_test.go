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

func TestLinks(t *testing.T) {
	got := Links("见 [[https://example.com][官网]] 和 [[file:a.org]]")
	if len(got) != 2 {
		t.Fatalf("链接数 = %d: %+v", len(got), got)
	}
	if got[0].URL != "https://example.com" || got[0].Text != "官网" {
		t.Errorf("第一个链接 = %+v", got[0])
	}
	if got[1].URL != "file:a.org" || got[1].Text != "file:a.org" {
		t.Errorf("无描述链接应回退到 URL: %+v", got[1])
	}
	if len(Links("没有链接")) != 0 {
		t.Error("不该凭空造出链接")
	}
}
