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

// Domain 返回发件人域名，用于规则匹配和展示。
func (m Message) Domain() string {
	if i := strings.LastIndexByte(m.FromAddr, '@'); i >= 0 && i+1 < len(m.FromAddr) {
		return strings.ToLower(m.FromAddr[i+1:])
	}
	return ""
}

// Key 是跨账号唯一的稳定标识。
func (m Message) Key() string { return fmt.Sprintf("%s/%d", m.Account, m.UID) }

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
