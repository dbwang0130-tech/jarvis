// Package mailbox 封装 IMAP 拉取与整理操作。
package mailbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
)

// Message 是一封邮件被压扁后的表示，只保留分类与展示需要的字段。
type Message struct {
	Account string
	UID     imap.UID

	FromName string
	FromAddr string
	Subject  string
	Date     time.Time
	Size     int64
	Unread   bool

	// Snippet 是正文开头的纯文本，给界面显示用。
	// **不再随分拣清单发给模型**——正文是私人内容，为了打个标签
	// 就整段送出去，代价和收益不成比例。
	Snippet string

	// 群发信号：这些头几乎只出现在营销/通知类邮件上。
	ListUnsubscribe bool
	ListID          string
	AutoSubmitted   bool
	Precedence      string
}

// From 返回适合展示的发件人。
func (m Message) From() string {
	if m.FromName != "" {
		return m.FromName
	}
	if i := strings.IndexByte(m.FromAddr, '@'); i > 0 {
		return m.FromAddr[:i]
	}
	return m.FromAddr
}

// Key 是跨账号唯一的稳定标识。
// Dedup 是**内存里去重用的**键，格式无所谓，只要一封信一个值。
//
// 它以前叫 Key()，而 mailstore.Key() 才是这封信的持久身份（「账号:UID」，
// 索引文件和界面都认那个），两个都叫 Key 而格式不一样（这儿是「账号/UID」）。
// 写邮件链接改写那段时就拿错过一次——每条都翻不出来，而且一声不响，点下去
// 只是没反应。改个名字，从此拿不错。
func (m Message) Dedup() string { return fmt.Sprintf("%s/%d", m.Account, m.UID) }

// BulkHint 概括群发信号，作为提示词里的一行事实。
func (m Message) BulkHint() string {
	var s []string
	if m.ListUnsubscribe {
		s = append(s, "有退订链接")
	}
	if m.ListID != "" {
		s = append(s, "列表邮件")
	}
	if m.AutoSubmitted {
		s = append(s, "系统自动发送")
	}
	if p := strings.ToLower(m.Precedence); p == "bulk" || p == "junk" || p == "list" {
		s = append(s, "Precedence:"+p)
	}
	if len(s) == 0 {
		return "无群发特征"
	}
	return strings.Join(s, "、")
}
