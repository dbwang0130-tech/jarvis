package mobile

import "testing"

func TestResolveMailLinks(t *testing.T) {
	key := func(no int) (string, bool) {
		switch no {
		case 3:
			return "工作:42", true
		case 7:
			return "私人:9", true
		}
		return "", false
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		// 中文账号名要转义：原样塞进 URL 的话 Swift 那边解不出来。
		{"翻成 key 并转义", "有一封 [发票](jarvis://mail/3) 要看。",
			"有一封 [发票](jarvis://mail/%E5%B7%A5%E4%BD%9C:42) 要看。"},
		{"一句里两条", "[发票](jarvis://mail/3) 和 [面试](jarvis://mail/7)",
			"[发票](jarvis://mail/%E5%B7%A5%E4%BD%9C:42) 和 [面试](jarvis://mail/%E7%A7%81%E4%BA%BA:9)"},
		// 翻不出来的拆回纯文本：坏链接比不可点更糟。
		{"编号对不上就拆成纯文本", "还有 [过期的](jarvis://mail/99) 那封。",
			"还有 过期的 那封。"},
		{"没有链接就原样", "今天没什么要紧的信。", "今天没什么要紧的信。"},
		// 日程那种链接不是它管的，一个字都不能动。
		{"别碰日程链接", "[买牛奶](jarvis://item/abc) 该做了。",
			"[买牛奶](jarvis://item/abc) 该做了。"},
		{"普通网址也别碰", "见 [文档](https://example.com/a)。",
			"见 [文档](https://example.com/a)。"},
	}
	for _, c := range cases {
		if got := resolveMailLinks(c.in, key); got != c.want {
			t.Errorf("%s：\n得到 %q\n应是 %q", c.name, got, c.want)
		}
	}
}

// 没有邮件会话时（手机上没配邮箱）原样返回，别把链接拆了——
// 那一轮根本不会有邮件链接，真有也不该由这儿处理。
func TestResolveMailLinksNoSession(t *testing.T) {
	in := "[发票](jarvis://mail/3)"
	if got := resolveMailLinks(in, nil); got != in {
		t.Errorf("该原样返回，得到 %q", got)
	}
	if got := resolveMailLinks(in, sessionMailKey(nil)); got != in {
		t.Errorf("sessionMailKey(nil) 该等于没有查法，得到 %q", got)
	}
}
