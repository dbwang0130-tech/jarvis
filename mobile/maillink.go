package mobile

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/mailstore"
)

// mailLink 认回话里的邮件链接。模型写的是这一轮清单里的编号：
// [主题](jarvis://mail/3)。
var mailLink = regexp.MustCompile(`\[([^\]]+)\]\(jarvis://mail/(\d+)\)`)

// resolveMailLinks 把回话里的邮件编号换成界面认得的 key。
//
// **编号只在这一轮的会话里有意义**，而界面拿它没用：它手上那份清单来自本地
// 索引（mailstore），标识是「账号:UID」。两边各有一套标识，中间必须有人翻译。
//
// 翻不出来的那条**退化成纯文本**，不是留一个坏链接：界面上一个点了没反应的
// 蓝字比不可点更糟，人会以为 app 卡了。翻不出来是正常会发生的事——模型可能
// 记错编号，那封也可能在这一轮里已经被清掉。
//
// key 收成函数而不是直接吃 *mail.Session，是为了能测：真会话要连 IMAP 才
// 造得出来，而这段恰恰有个静默失败模式（key 格式拼错只是点下去没反应，
// 什么都不报）。
func resolveMailLinks(reply string, key func(no int) (string, bool)) string {
	// 不做 MatchString 预检：没匹配的话 ReplaceAllStringFunc 本来就是空操作，
	// 预检只是把同一段话多扫一遍。
	if key == nil {
		return reply
	}
	return mailLink.ReplaceAllStringFunc(reply, func(m string) string {
		p := mailLink.FindStringSubmatch(m)
		no, err := strconv.Atoi(p[2])
		if err != nil {
			return p[1]
		}
		k, ok := key(no)
		if !ok {
			return p[1]
		}
		// **转义一下。** 账号名是用户自己起的，可能是中文（「工作:42」），
		// 原样塞进 URL 的话 Swift 那边 URL(string:) 直接解不出来，
		// 链接变成不可点——而 org 的 ref 是 work.org:12，纯 ASCII，
		// 所以这个坑只在邮件这一侧。
		return fmt.Sprintf("[%s](jarvis://mail/%s)", p[1], url.PathEscape(k))
	})
}

// sessionMailKey 交出「编号 → key」的查法。
//
// 用 mailstore.Key —— 界面那份清单就是拿它当身份的。（mailbox 那边另有一个
// 「账号/UID」的值，但它已经改名叫 Dedup 了，只用于内存去重，不会再拿错。）
func sessionMailKey(sess *mail.Session) func(int) (string, bool) {
	if sess == nil {
		return nil
	}
	return func(no int) (string, bool) {
		it, err := sess.Item(no)
		if err != nil || it == nil || it.Msg == nil {
			return "", false
		}
		return mailstore.Key(it.Msg.Account, uint32(it.Msg.UID)), true
	}
}
