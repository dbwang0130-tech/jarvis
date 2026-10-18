package mail

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/mailbox"
)

// 下面这几行是**别名，不是第二份实现**——指向 agent 里那一份唯一定义
// （见 agent/schema.go）。原先每个技能包各写一份函数体，而 num 已经在
// ledger/health 和 mail/orgtools 之间漂成了 number 和 integer 两种。
var (
	obj = agent.Obj
	str = agent.Str
	arr = agent.Arr
)

// Tool 是秘书能对邮箱执行的一个动作，实现 agent.Tool 接口。
type Tool struct {
	spec     agent.ToolSpec
	risk     agent.Risk
	sess     *Session
	readonly bool
	preview  func(s *Session, raw string) (title, detail string, err error)
	run      func(s *Session, raw string) (string, error)
}

func (t *Tool) Spec() agent.ToolSpec { return t.spec }
func (t *Tool) Risk() agent.Risk     { return t.risk }
func (t *Tool) ReadOnly() bool       { return t.readonly }
func (t *Tool) Preview(raw string) (string, string, error) {
	if t.preview == nil {
		return "", "", nil
	}
	return t.preview(t.sess, raw)
}
func (t *Tool) Run(raw string) (string, error) {
	out, err := t.run(t.sess, raw)
	return out, err
}

func spec(name, desc string, params map[string]any) agent.ToolSpec {
	return agent.ToolSpec{Name: name, Description: desc, Parameters: params}
}

// Tools 返回全部邮件工具，按名字索引。
func Tools() map[string]*Tool {
	list := []*Tool{
		toolListMail(), toolTriage(), toolRefreshMail(),
		toolLoadMore(), toolMarkRead(), toolCleanMail(), toolDraftReply(),
		toolDraftNew(), toolSendDraft(),
	}
	out := make(map[string]*Tool, len(list))
	for _, t := range list {
		out[t.spec.Name] = t
	}
	return out
}

// SortedNames 返回按字母序排列的工具名。
func SortedNames(tools map[string]*Tool) []string {
	names := make([]string, 0, len(tools))
	for n := range tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ---------- 只读工具 ----------

func toolListMail() *Tool {
	return &Tool{
		spec: spec("list_mail",
			"列出当前已知的邮件清单（编号、发件人、主题、时间、正文开头）。这是本次拉取的结果，清理掉的邮件会从中消失。清单已在对话开头给过，只有需要重看时才调用。想从服务器重新同步用 refresh_mail。",
			obj(map[string]any{})),
		readonly: true,
		run:      func(s *Session, raw string) (string, error) { return s.Digest(), nil },
	}
}

func toolRefreshMail() *Tool {
	return &Tool{
		spec: spec("refresh_mail",
			"去服务器同步上次之后新到的邮件。用户说「刷新」「看看有没有新邮件」时用。已有邮件的编号不变，新邮件编号接着往下排。",
			obj(map[string]any{})),
		run: func(s *Session, raw string) (string, error) {
			res := s.Sync()
			var b strings.Builder
			if len(res.New) == 0 {
				b.WriteString("已同步，没有新邮件。\n")
			} else {
				fmt.Fprintf(&b, "已同步，新到 %d 封，编号接着往下排。\n", len(res.New))
			}
			for name, err := range res.Errors {
				fmt.Fprintf(&b, "账号 %s 同步失败：%v\n", name, err)
			}
			for name, n := range res.Remaining {
				fmt.Fprintf(&b, "账号 %s 还有 %d 封更早的没拉，需要的话用 load_more_mail。\n", name, n)
			}
			if len(res.New) > 0 {
				b.WriteString("\n以下是新到的，都还没判断过：\n\n")
				b.WriteString(s.DigestOf(res.New))
			}
			return b.String(), nil
		},
	}
}

func toolLoadMore() *Tool {
	return &Tool{
		spec: spec("load_more_mail",
			"往前再加载一批更早的邮件，追加到清单末尾，已有邮件的编号不变。用户说「再往前看看」「还有更早的吗」「翻下一页」时用。",
			obj(map[string]any{})),
		run: func(s *Session, raw string) (string, error) {
			res := s.LoadMore(false)
			if res.Total == 0 && len(res.Errors) == 0 {
				return "再往前没有更多邮件了。", nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "又加载了 %d 封更早的邮件，编号接着往下排。\n", res.Total)
			for name, err := range res.Errors {
				fmt.Fprintf(&b, "账号 %s 加载失败：%v\n", name, err)
			}
			if res.Total > 0 {
				b.WriteString("\n以下是新加载的这批，都还没判断过：\n\n")
				b.WriteString(s.DigestOf(res.New))
			}
			return b.String(), nil
		},
	}
}

// 这里原来有个 read_mail，把整封正文交给模型。删掉了。
//
// 正文是私人内容，**一个字都不往模型那儿去**。分拣靠发件人、主题和群发头
// 就够了；真要看原文，用户在邮件页自己看——那条路（Mail 的 read 动作）
// 只经过手机本地和邮件服务器，不经过模型。
//
// 靠提示词说「别读正文」是不够的：工具摆在那儿，模型迟早会在某个它觉得
// 「实在拿不准」的时候调一次。把工具拿掉，这条路才是真的没有。
// TestNoToolLeaksTheBody 守着这件事。

func toolTriage() *Tool {
	return &Tool{
		spec: spec("triage",
			"给邮件打上处置标签。一次把所有邮件都传进来，不要一封一封调。",
			obj(map[string]any{
				"items": arr(obj(map[string]any{
					"no":      agent.Int("邮件编号"),
					"verdict": map[string]any{"type": "string", "enum": []string{"important", "interest", "normal", "promo", "spam"}, "description": "important=需要尽快处理; interest=想读但不急; normal=留着即可; promo=营销推广; spam=垃圾或诈骗"},
					"reason":  str("一句话理由，15 字以内"),
				}, "no", "verdict", "reason"), "所有邮件的判断结果"),
			}, "items")),
		run: func(s *Session, raw string) (string, error) {
			var a struct {
				Items []struct {
					No      int     `json:"no"`
					Verdict Verdict `json:"verdict"`
					Reason  string  `json:"reason"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", err
			}
			var bad []string
			ok := 0
			for _, it := range a.Items {
				if err := s.SetTriage(it.No, Triage{Verdict: it.Verdict, Reason: it.Reason}); err != nil {
					bad = append(bad, err.Error())
					continue
				}
				ok++
			}
			msg := fmt.Sprintf("已标注 %d 封。", ok)
			if len(bad) > 0 {
				msg += " 以下失败：" + strings.Join(bad, "；")
			}
			var nos []int
			for _, it := range a.Items {
				nos = append(nos, it.No)
			}
			if n, fails := s.MarkSeen(nos, true, true); n > 0 || len(fails) > 0 {
				msg += fmt.Sprintf(" 其中 %d 封已在服务器上标为已读。", n)
				if len(fails) > 0 {
					msg += " 标记失败：" + strings.Join(fails, "；")
				}
			}
			return msg, nil
		},
	}
}

// ---------- 需确认的工具 ----------

func toolMarkRead() *Tool {
	return &Tool{
		spec: spec("mark_read",
			"把邮件在服务器上标成已读或未读。用户说「这几封我看过了」「把那封标回未读」时用。",
			obj(map[string]any{
				"nos":  arr(map[string]any{"type": "integer"}, "邮件编号"),
				"read": map[string]any{"type": "boolean", "description": "true 标为已读，false 标回未读。默认 true"},
			}, "nos")),
		run: func(s *Session, raw string) (string, error) {
			var a struct {
				Nos  []int `json:"nos"`
				Read *bool `json:"read"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", err
			}
			if len(a.Nos) == 0 {
				return "", fmt.Errorf("没有指定要标记的邮件")
			}
			seen := a.Read == nil || *a.Read
			done, fails := s.MarkSeen(a.Nos, seen, false)
			word := "已读"
			if !seen {
				word = "未读"
			}
			msg := fmt.Sprintf("已把 %d 封标为%s。", done, word)
			if len(fails) > 0 {
				msg += " 失败：" + strings.Join(fails, "；")
			}
			return msg, nil
		},
	}
}

func toolCleanMail() *Tool {
	return &Tool{
		spec: spec("clean_mail",
			"把指定邮件移进垃圾箱。只对确定是广告或垃圾的邮件用。用户会先看到清单并确认。",
			obj(map[string]any{
				"nos": arr(map[string]any{"type": "integer"}, "要清理的邮件编号"),
			}, "nos")),
		risk: agent.RiskReversible,
		preview: func(s *Session, raw string) (string, string, error) {
			nos, err := parseNos(raw)
			if err != nil {
				return "", "", err
			}
			var b strings.Builder
			for _, no := range nos {
				it, err := s.Item(no)
				if err != nil {
					return "", "", err
				}
				fmt.Fprintf(&b, "  [%d] %s — %s（%s）\n", it.No, it.Msg.From(), it.Msg.Subject, it.Reason)
			}
			return fmt.Sprintf("把 %d 封邮件移进垃圾箱", len(nos)), b.String(), nil
		},
		run: func(s *Session, raw string) (string, error) {
			nos, err := parseNos(raw)
			if err != nil {
				return "", err
			}
			done, fails := s.Clean(nos)
			msg := fmt.Sprintf("已移入垃圾箱 %d 封，它们已从清单中移除。", done)
			if len(fails) > 0 {
				msg += " 失败：" + strings.Join(fails, "；")
			}
			return msg, nil
		},
	}
}

func toolDraftReply() *Tool {
	return &Tool{
		spec: spec("draft_reply",
			"为某封邮件起草回信。body 要写成可以直接发出去的完整正文，不要写占位符。起草不会发出，用户确认后才由 send_draft 发送。",
			obj(map[string]any{
				"no":      agent.Int("要回复的邮件编号"),
				"body":    str("回信正文全文，中文，包含称呼和结尾"),
				"subject": str("可选。留空则自动用 Re: 原主题"),
			}, "no", "body")),
		run: func(s *Session, raw string) (string, error) {
			var a struct {
				No      int    `json:"no"`
				Body    string `json:"body"`
				Subject string `json:"subject"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", err
			}
			it, err := s.Item(a.No)
			if err != nil {
				return "", err
			}
			acct, err := s.cfg.Account(it.Msg.Account)
			if err != nil {
				return "", err
			}
			c, err := s.client(it.Msg.Account)
			if err != nil {
				return "", err
			}
			msgID, refs, _ := c.MessageID(it.Msg.UID)

			subject := a.Subject
			if subject == "" {
				subject = it.Msg.Subject
				if !strings.HasPrefix(strings.ToLower(subject), "re:") {
					subject = "Re: " + subject
				}
			}
			d := &mailbox.Draft{
				Account:        acct.Name,
				To:             []string{it.Msg.FromAddr},
				Subject:        subject,
				Body:           withSignature(a.Body, acct.Signature),
				InReplyTo:      msgID,
				References:     refs,
				ReplyToSubject: it.Msg.Subject,
			}
			id := s.AddDraft(d)
			return fmt.Sprintf("草稿 %s 已生成（收件人 %s，主题「%s」）。已展示给用户，等待确认后再调用 send_draft。",
				id, d.To[0], d.Subject), nil
		},
	}
}

func toolDraftNew() *Tool {
	return &Tool{
		spec: spec("draft_new",
			"起草一封新邮件。body 要写成可以直接发出去的完整正文。起草不会发出。",
			obj(map[string]any{
				"to":      arr(map[string]any{"type": "string"}, "收件人邮箱地址"),
				"subject": str("邮件主题"),
				"body":    str("正文全文，中文，包含称呼和结尾"),
				"cc":      arr(map[string]any{"type": "string"}, "可选，抄送地址"),
				"account": str("可选，用哪个账号发；留空用默认账号"),
			}, "to", "subject", "body")),
		run: func(s *Session, raw string) (string, error) {
			var a struct {
				To      []string `json:"to"`
				Cc      []string `json:"cc"`
				Subject string   `json:"subject"`
				Body    string   `json:"body"`
				Account string   `json:"account"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", err
			}
			acct := s.DefaultAccount()
			if a.Account != "" {
				got, err := s.cfg.Account(a.Account)
				if err != nil {
					return "", err
				}
				acct = got
			}
			for _, to := range append(append([]string{}, a.To...), a.Cc...) {
				if _, err := mail.ParseAddress(to); err != nil {
					return "", fmt.Errorf("收件地址 %q 不合法", to)
				}
			}
			d := &mailbox.Draft{
				Account: acct.Name,
				To:      a.To,
				Cc:      a.Cc,
				Subject: a.Subject,
				Body:    withSignature(a.Body, acct.Signature),
			}
			id := s.AddDraft(d)
			return fmt.Sprintf("草稿 %s 已生成（收件人 %s，主题「%s」）。已展示给用户，等待确认后再调用 send_draft。",
				id, strings.Join(d.To, ", "), d.Subject), nil
		},
	}
}

func toolSendDraft() *Tool {
	return &Tool{
		spec: spec("send_draft",
			"把已起草的邮件真正发出去。只有用户明确表示要发时才调用。",
			obj(map[string]any{"draft_id": str("草稿 id，形如 d1")}, "draft_id")),
		risk: agent.RiskIrreversible,
		preview: func(s *Session, raw string) (string, string, error) {
			d, err := draftOf(s, raw)
			if err != nil {
				return "", "", err
			}
			acct, _ := s.cfg.Account(d.Account)
			var b strings.Builder
			fmt.Fprintf(&b, "  发件: %s <%s>\n", acct.From, acct.User)
			fmt.Fprintf(&b, "  收件: %s\n", strings.Join(d.To, ", "))
			if len(d.Cc) > 0 {
				fmt.Fprintf(&b, "  抄送: %s\n", strings.Join(d.Cc, ", "))
			}
			fmt.Fprintf(&b, "  主题: %s\n\n%s\n", d.Subject, indent(d.Body, "  "))
			return fmt.Sprintf("发送邮件给 %s", strings.Join(d.To, ", ")), b.String(), nil
		},
		run: func(s *Session, raw string) (string, error) {
			d, err := draftOf(s, raw)
			if err != nil {
				return "", err
			}
			acct, err := s.cfg.Account(d.Account)
			if err != nil {
				return "", err
			}
			if err := mailbox.Send(acct, *d); err != nil {
				return "", err
			}
			return fmt.Sprintf("已发送给 %s，主题「%s」。", strings.Join(d.To, ", "), d.Subject), nil
		},
	}
}

// ---------- 小工具 ----------

func parseNos(raw string) ([]int, error) {
	var a struct {
		Nos []int `json:"nos"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, err
	}
	if len(a.Nos) == 0 {
		return nil, fmt.Errorf("没有指定要处理的邮件")
	}
	return a.Nos, nil
}

func draftOf(s *Session, raw string) (*mailbox.Draft, error) {
	var a struct {
		DraftID string `json:"draft_id"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, err
	}
	return s.Draft(a.DraftID)
}

func withSignature(body, sig string) string {
	body = strings.TrimRight(body, "\n")
	if sig == "" {
		return body
	}
	if strings.Contains(body, strings.TrimSpace(sig)) {
		return body
	}
	return body + "\n\n" + sig
}

func indent(s, pad string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
