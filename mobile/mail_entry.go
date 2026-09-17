package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/mailbox"
	"github.com/dabao/jarvis/internal/mailstore"
)

// MailInput 是邮件那几个动作的入参。
type MailInput struct {
	// Op：list 看本地存的 / sync 拉新的并分拣 / read 读正文 / clean 清理。
	Op string `json:"op"`
	// IndexDir 放索引（含分拣结论），BodyDir 放正文。分开是因为取舍相反：
	// 索引小但重算要花模型调用，正文大但随时能重拉。路径由界面给，
	// Go 不猜——iOS 上能写的地方就那几个。
	IndexDir string        `json:"index_dir"`
	BodyDir  string        `json:"body_dir"`
	Accounts []MailAccount `json:"accounts"`
	Model    ModelConfig   `json:"model"`
	Profile  string        `json:"profile"`
	// Keys 是操作对象，形如 "工作:1234"（账号:UID）。
	Keys []string `json:"keys"`
	Key  string   `json:"key"`
	TZ   string   `json:"tz"`
}

// MailItem 是列表里的一封。主角是 Reason（为什么归到这一类），
// 不是 Subject——「Re: Re: FW:」这种主题本身没什么信息量。
type MailItem struct {
	Key     string `json:"key"`
	Account string `json:"account"`
	From    string `json:"from"`
	Addr    string `json:"addr"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Unread  bool   `json:"unread"`
	Snippet string `json:"snippet"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
	HasBody bool   `json:"has_body"`
}

type MailOutput struct {
	Items []MailItem `json:"items"`
	// Body 是展示用的原文：HTML 优先，没有才用纯文本。
	Body *MailBody `json:"body,omitempty"`
	// Draft 是起草出来的回信。
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// MailBody 是一封邮件的原文。
//
// 账单、验证码、活动通知这类信的内容全在 HTML 的排版里，压成纯文本就只剩
// 一堆散字——所以 HTML 要原样留着，内联图片也要嵌进去。
type MailBody struct {
	HTML string `json:"html"`
	Text string `json:"text"`
	// Attachments 只给清单不给内容：几 MB 的东西过一次 JSON 桥没有意义。
	Attachments []MailAttachment `json:"attachments"`
	// InlineDropped 是因为太大没嵌进来的内联图片数，让人知道有图没显示。
	InlineDropped int `json:"inline_dropped"`
}

type MailAttachment struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Size int    `json:"size"`
}

func (o *MailOutput) normalize() {
	if o.Items == nil {
		o.Items = []MailItem{}
	}
}

// Mail 是邮件页的唯一入口。
//
// list 只读本地，不连服务器——打开页面要秒开。sync 才去拉，拉完顺手把
// 还没判过的分拣一遍。正文优先读本地，没有才去服务器要。
func Mail(inputJSON string) string {
	var in MailInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&MailOutput{Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	if strings.TrimSpace(in.IndexDir) == "" {
		return marshal(&MailOutput{Error: "没说邮件库放哪儿"})
	}
	store, err := mailstore.New(in.IndexDir, in.BodyDir)
	if err != nil {
		return marshal(&MailOutput{Error: err.Error()})
	}

	switch in.Op {
	case "list", "":
		return marshal(&MailOutput{Items: listLocal(store, in.Accounts)})
	case "sync":
		return mailSync(store, in)
	case "read":
		return mailRead(store, in)
	case "clean":
		return mailClean(store, in)
	default:
		return marshal(&MailOutput{Error: fmt.Sprintf("不认识的动作 %q", in.Op)})
	}
}

// listLocal 把本地存的读出来，清理过的不显示（墓碑只是为了别再拉回来）。
func listLocal(store *mailstore.Store, accounts []MailAccount) []MailItem {
	var out []MailItem
	for _, a := range accounts {
		recs, err := store.Load(a.Name)
		if err != nil {
			continue
		}
		for _, r := range recs {
			if r.Deleted {
				continue
			}
			out = append(out, MailItem{
				Key: r.Key(), Account: r.Account, From: r.From, Addr: r.Addr,
				Subject: r.Subject, Date: r.Date.Format("2006-01-02 15:04"),
				Unread: r.Unread, Snippet: r.Snippet,
				Verdict: r.Verdict, Reason: r.Reason, HasBody: r.HasBody,
			})
		}
	}
	return out
}

func mailSync(store *mailstore.Store, in MailInput) string {
	sess := mailSessionFor(in.Accounts)
	if sess == nil {
		return marshal(&MailOutput{Error: "还没配邮箱"})
	}

	res := sess.Sync()
	items := sess.Items()

	// 先把拉到的并进本地，再分拣——万一分拣那步出岔子（没配模型、网络断了），
	// 邮件本身已经存下来了，不用再拉一遍。
	byAccount := map[string][]mailstore.Record{}
	for _, it := range items {
		m := it.Msg
		byAccount[m.Account] = append(byAccount[m.Account], mailstore.Record{
			Account: m.Account, UID: uint32(m.UID),
			From: m.From(), Addr: m.FromAddr, Subject: m.Subject,
			Date: m.Date, Unread: m.Unread, Snippet: m.Snippet,
		})
	}
	for acct, recs := range byAccount {
		if _, err := store.Merge(acct, recs); err != nil {
			return marshal(&MailOutput{Error: "存不下来：" + err.Error()})
		}
	}

	// **先看本地判过没有**。分拣要花一次模型调用，而同一封信的结论不会变——
	// 服务器重发、换设备、翻页拉回旧邮件，都会让已经判过的又出现在清单里。
	// 把已知结论填回会话，模型看到的清单上它们已经有标签了，不会重判。
	known := knownVerdicts(store, byAccount)
	need := 0
	for _, it := range items {
		v, ok := known[mailKeyOf(it.Msg.Account, uint32(it.Msg.UID))]
		if ok {
			_ = sess.SetTriage(it.No, v)
			continue
		}
		need++
	}

	triaged := 0
	// 一封没判过的都没有就别调模型了——白花一次钱换一句「都判过了」。
	if need > 0 && in.Model.Provider != "" && in.Model.APIKey != "" {
		triaged = runTriage(sess, in)
		for _, it := range items {
			if it.Verdict == "" {
				continue
			}
			_ = store.SetTriage(it.Msg.Account, uint32(it.Msg.UID),
				string(it.Verdict), it.Reason)
		}
	}

	msg := fmt.Sprintf("拉到 %d 封新的", len(items))
	switch {
	case triaged > 0:
		msg += fmt.Sprintf("，分拣了 %d 封", triaged)
	case need == 0 && len(items) > 0:
		msg += "，都是判过的，没再麻烦模型"
	}
	if len(res.Errors) > 0 {
		var parts []string
		for acct, err := range res.Errors {
			parts = append(parts, acct+"："+err.Error())
		}
		msg += "。有账号没拉成：" + strings.Join(parts, "；")
	}
	return marshal(&MailOutput{Items: listLocal(store, in.Accounts), Message: msg})
}

// knownVerdicts 从本地索引里捞出已经判过的结论，键是「账号:UID」。
func knownVerdicts(store *mailstore.Store, byAccount map[string][]mailstore.Record) map[string]mail.Triage {
	out := map[string]mail.Triage{}
	for acct := range byAccount {
		recs, err := store.Load(acct)
		if err != nil {
			continue
		}
		for _, r := range recs {
			if r.Verdict == "" {
				continue
			}
			out[mailKeyOf(r.Account, r.UID)] = mail.Triage{
				Verdict: mail.Verdict(r.Verdict), Reason: r.Reason,
			}
		}
	}
	return out
}

func mailKeyOf(account string, uid uint32) string {
	return account + ":" + strconv.FormatUint(uint64(uid), 10)
}

// runTriage 让模型把还没判过的分拣一遍，返回判了几封。
//
// 走的是跟秘书同一套工具（list_mail + triage），不是另写一遍判断规则——
// 两处各判各的，手机上和电脑上对同一封信会给出不同结论。
func runTriage(sess *mail.Session, in MailInput) int {
	cfg := mobileAgentConfig(in.Model, in.Profile)
	ag, err := agent.Build(cfg, mail.NewSkill(sess))
	if err != nil {
		return 0
	}
	ag.Stream = false
	ag.Auto = agent.AutoClean
	_, _ = ag.Ask(context.Background(),
		"把清单里**还没有处置标签**的邮件分拣一遍。已经有标签的跳过，"+
			"别重判。只调工具，不用写汇报。", nil)

	n := 0
	for _, it := range sess.Items() {
		if it.Verdict != "" {
			n++
		}
	}
	return n
}

func mailRead(store *mailstore.Store, in MailInput) string {
	acct, uid, ok := splitMailKey(in.Key)
	if !ok {
		return marshal(&MailOutput{Error: fmt.Sprintf("引用 %q 不合法", in.Key)})
	}

	// 本地存的是整封原始字节，解析每次现做——解析结果（嵌了图的 HTML）
	// 比原始字节还大，存它不划算。
	if raw, ok := store.Body(acct, uid); ok {
		return marshal(&MailOutput{Body: parseMailBody([]byte(raw))})
	}

	sess := mailSessionFor(in.Accounts)
	if sess == nil {
		return marshal(&MailOutput{Error: "本地没存正文，而且还没配邮箱"})
	}
	no, ok := noForUID(sess, acct, uid)
	if !ok {
		return marshal(&MailOutput{
			Error: "本地没存正文，这封也不在这次拉下来的清单里——先同步一下"})
	}
	raw, err := sess.Raw(no)
	if err != nil {
		return marshal(&MailOutput{Error: "读不出来：" + err.Error()})
	}
	_ = store.SetBody(acct, uid, string(raw))
	return marshal(&MailOutput{Body: parseMailBody(raw)})
}

func parseMailBody(raw []byte) *MailBody {
	full, err := mailbox.ParseBody(raw)
	if err != nil || full == nil {
		// 解析不了也别空着手回去——原样给出来总比什么都没有强。
		return &MailBody{Text: string(raw), Attachments: []MailAttachment{}}
	}
	out := &MailBody{
		HTML: full.HTML, Text: full.Text,
		InlineDropped: full.InlineDropped,
		Attachments:   make([]MailAttachment, 0, len(full.Attachments)),
	}
	for _, a := range full.Attachments {
		out.Attachments = append(out.Attachments,
			MailAttachment{Name: a.Name, Type: a.Type, Size: a.Size})
	}
	return out
}

func mailClean(store *mailstore.Store, in MailInput) string {
	sess := mailSessionFor(in.Accounts)
	if sess == nil {
		return marshal(&MailOutput{Error: "还没配邮箱"})
	}

	byAccount := map[string][]uint32{}
	var nos []int
	for _, k := range in.Keys {
		acct, uid, ok := splitMailKey(k)
		if !ok {
			continue
		}
		byAccount[acct] = append(byAccount[acct], uid)
		if no, ok := noForUID(sess, acct, uid); ok {
			nos = append(nos, no)
		}
	}
	if len(nos) == 0 {
		return marshal(&MailOutput{
			Error: "这些邮件不在这次拉下来的清单里，先同步一下再清理"})
	}

	done, fails := sess.Clean(nos)
	for acct, uids := range byAccount {
		_ = store.MarkDeleted(acct, uids)
	}

	msg := fmt.Sprintf("清掉了 %d 封", done)
	if len(fails) > 0 {
		msg += "。没清成的：" + strings.Join(fails, "；")
	}
	return marshal(&MailOutput{Items: listLocal(store, in.Accounts), Message: msg})
}

// noForUID 把「账号 + UID」换成这一轮会话里的编号。
// 会话是跨轮次留着的，但只装这次拉下来的那些。
func noForUID(sess *mail.Session, account string, uid uint32) (int, bool) {
	for _, it := range sess.Items() {
		if it.Msg.Account == account && uint32(it.Msg.UID) == uid {
			return it.No, true
		}
	}
	return 0, false
}

// splitMailKey 拆 "工作:1234"。账号名里可能有冒号，从最后一个切。
func splitMailKey(key string) (account string, uid uint32, ok bool) {
	i := strings.LastIndex(key, ":")
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.ParseUint(key[i+1:], 10, 32)
	if err != nil {
		return "", 0, false
	}
	return key[:i], uint32(n), true
}
