package acp

import "testing"

func TestLinkFilterAcrossChunks(t *testing.T) {
	cases := []struct {
		chunks []string
		want   string
	}{
		{[]string{"看 [房贷", "还款](jar", "vis://item/life.org:35) 吧"}, "看 房贷还款 吧"},
		{[]string{"[普通](https://a.com) 链接"}, "[普通](https://a.com) 链接"},
		{[]string{"数组 a[0] 和 [x"}, "数组 a[0] 和 [x"},
		{[]string{"[[嵌套](jarvis://mail/3)"}, "[嵌套"},
		{[]string{"[换行\n]"}, "[换行\n]"},
		{[]string{"[一](jarvis://item/a)[二](jarvis://item/b)"}, "一二"},
	}
	for _, c := range cases {
		var f linkFilter
		got := ""
		for _, ch := range c.chunks {
			got += f.feed(ch)
		}
		got += f.flush()
		if got != c.want {
			t.Errorf("%q → %q，想要 %q", c.chunks, got, c.want)
		}
	}
}
