package mailbox

import (
	"fmt"
	"sort"
	"strings"

	"github.com/emersion/go-imap/v2"
)

// JunkResult 是一次垃圾箱判定的结果，带上判定依据便于排查。
type JunkResult struct {
	Mailbox string
	Reason  string
}

// 各家邮箱的垃圾箱名。比较时统一小写、并且只看路径的最后一段，
// 所以 "[Gmail]/垃圾邮件" 和 "其他文件夹/垃圾邮件" 都能命中。
var junkExact = []string{
	"junk", "spam", "junk e-mail", "junk email", "junkmail", "bulk mail",
	"垃圾邮件", "垃圾郵件", "垃圾箱", "广告邮件", "廣告郵件",
	"迷惑メール", "スパム", "스팸",
	"correo no deseado", "courrier indésirable", "unerwünscht", "posta indesiderata",
}

// 精确表没命中时的关键词，覆盖 "Junk E-mail (7)" 这类带后缀的写法。
var junkKeywords = []string{"junk", "spam", "垃圾", "迷惑", "스팸"}

// 废纸篓不是垃圾箱。识别出来只为了在找不到垃圾箱时给用户一个明确建议，
// 不会自动拿它当垃圾箱用——把广告扔进废纸篓和扔进垃圾箱不是一回事。
var trashExact = []string{
	"trash", "deleted messages", "deleted items", "bin",
	"已删除", "已刪除", "已删除邮件", "已刪除郵件", "废纸篓", "廢紙簍", "回收站",
}

// leafName 取文件夹路径的最后一段并小写，delim 是服务器声明的层级分隔符。
func leafName(box string, delim rune) string {
	name := box
	if delim != 0 {
		if i := strings.LastIndexFunc(box, func(r rune) bool { return r == delim }); i >= 0 {
			name = box[i+len(string(delim)):]
		}
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// selectable 排除不能存邮件的容器节点，例如 QQ 邮箱的 "其他文件夹"。
func selectable(d *imap.ListData) bool {
	for _, a := range d.Attrs {
		if a == imap.MailboxAttrNoSelect {
			return false
		}
	}
	return true
}

// detectJunk 从文件夹列表里判定垃圾箱。判定分三档，越靠前越可信：
//  1. 服务器自己标了 \Junk（SPECIAL-USE，最可靠）
//  2. 名字正好是各家已知的垃圾箱名
//  3. 名字里含 junk / spam / 垃圾 这类词
//
// 纯函数，便于对着各家邮箱的真实文件夹名写测试。
func detectJunk(boxes []*imap.ListData) (JunkResult, bool) {
	for _, b := range boxes {
		if !selectable(b) {
			continue
		}
		for _, a := range b.Attrs {
			if a == imap.MailboxAttrJunk {
				return JunkResult{b.Mailbox, "服务器标记了 \\Junk"}, true
			}
		}
	}

	for _, b := range boxes {
		if !selectable(b) {
			continue
		}
		leaf := leafName(b.Mailbox, b.Delim)
		for _, want := range junkExact {
			if leaf == want {
				return JunkResult{b.Mailbox, "名字匹配已知垃圾箱"}, true
			}
		}
	}

	for _, b := range boxes {
		if !selectable(b) {
			continue
		}
		leaf := leafName(b.Mailbox, b.Delim)
		for _, kw := range junkKeywords {
			if strings.Contains(leaf, kw) {
				return JunkResult{b.Mailbox, "名字含「" + kw + "」"}, true
			}
		}
	}

	return JunkResult{}, false
}

// detectTrash 找废纸篓，仅用于在找不到垃圾箱时给建议。
func detectTrash(boxes []*imap.ListData) (string, bool) {
	for _, b := range boxes {
		if !selectable(b) {
			continue
		}
		for _, a := range b.Attrs {
			if a == imap.MailboxAttrTrash {
				return b.Mailbox, true
			}
		}
	}
	for _, b := range boxes {
		if !selectable(b) {
			continue
		}
		leaf := leafName(b.Mailbox, b.Delim)
		for _, want := range trashExact {
			if leaf == want {
				return b.Mailbox, true
			}
		}
	}
	return "", false
}

// junkNotFound 组织一条能让用户直接照做的错误。
func junkNotFound(account string, boxes []*imap.ListData) error {
	var names []string
	for _, b := range boxes {
		if selectable(b) {
			names = append(names, b.Mailbox)
		}
	}
	sort.Strings(names)

	var b strings.Builder
	fmt.Fprintf(&b, "没能自动判断出 %s 的垃圾箱", account)
	if trash, ok := detectTrash(boxes); ok {
		fmt.Fprintf(&b, "（只找到废纸篓 %q，没拿它当垃圾箱——语义不同）", trash)
	}
	fmt.Fprintf(&b, "。\n请在配置里给该账号写明 junk = \"文件夹名\"，可选：\n  %s",
		strings.Join(names, "\n  "))
	return fmt.Errorf("%s", b.String())
}
