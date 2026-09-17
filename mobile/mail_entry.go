package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/emersion/go-imap/v2"

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
}

type MailOutput struct {
	Items []MailItem `json:"items"`
	// Body 是展示用的原文。直接用 mailbox 的类型——照抄一份字段一样的
	// 结构体，除了多一个搬字段的循环，没有任何好处。
	Body *mailbox.FullBody `json:"body,omitempty"`
	// Message 是这次动作的附注，给界面显示用（拉到几封、清掉几封）。
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

func (o *MailOutput) normalize() {
	if o.Items == nil {
		o.Items = []MailItem{}
	}
	// 往下钻一层。数组补齐这件事在 marshal 出口统一做，不指望每个赋值点
	// 都记得——Swift 那边数组字段是非可选的，解到 null 整个出参作废。
	if o.Body != nil && o.Body.Attachments == nil {
		o.Body.Attachments = []mailbox.Attachment{}
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

// listLocal 把本地存的读出来。只有 list 这条路要真读盘。
func listLocal(store *mailstore.Store, accounts []MailAccount) []MailItem {
	var out []MailItem
	for _, a := range accounts {
		recs, err := store.Load(a.Name)
		if err != nil {
			continue
		}
		out = append(out, itemsOf(recs)...)
	}
	return out
}

// itemsOf 把记录转成过桥的样子。清理过的不显示——墓碑只是为了别再拉回来。
func itemsOf(recs []mailstore.Record) []MailItem {
	out := make([]MailItem, 0, len(recs))
	for _, r := range recs {
		if r.Deleted {
			continue
		}
		out = append(out, MailItem{
			Key: r.Key(), Account: r.Account, From: r.From, Addr: r.Addr,
			Subject: r.Subject, Date: r.Date.Format("2006-01-02 15:04"),
			Unread: r.Unread, Snippet: r.Snippet,
			Verdict: r.Verdict, Reason: r.Reason,
		})
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

	// **每个配好的账号都要过一遍**，不是只过这次拉到东西的那几个。
	// 只认 byAccount 的话，一次空手而归就等于谁都没合过，出参里一封都
	// 没有——界面上原来那一列直接清空，用户以为邮件全没了。
	// 连不上服务器的账号同理，本地存着的照样该给出来。
	//
	// 顺手接住 Merge 的返回值：它给的就是合并之后的全部记录，丢掉的话
	// 下面每一步都要再读一次盘——一次同步能读上三四遍同一个文件。
	var saveFail []string
	merged := map[string][]mailstore.Record{}
	known := map[string]mailstore.Triage{}
	for _, a := range in.Accounts {
		acct := a.Name
		after, err := store.Merge(acct, byAccount[acct])
		if err != nil {
			// 一个账号的索引坏了，不该把另一个账号的邮件也一起憋回去。
			saveFail = append(saveFail, acct+" 存不下来："+err.Error())
			continue
		}
		merged[acct] = after
		for _, r := range after {
			if r.Verdict != "" {
				known[r.Key()] = mailstore.Triage{Verdict: r.Verdict, Reason: r.Reason}
			}
		}
	}

	// **先看本地判过没有**。分拣要花一次模型调用，而同一封信的结论不会变——
	// 服务器重发、换设备、翻页拉回旧邮件，都会让已经判过的又出现在清单里。
	// 把已知结论填回会话，模型看到的清单上它们已经有标签了，不会重判。
	need := 0
	for _, it := range items {
		if t, ok := known[mailstore.Key(it.Msg.Account, uint32(it.Msg.UID))]; ok {
			// 编号是刚从 items 里取的，填不进去只可能是会话被换过——
			// 那样最多重判一次，不值得打断整次同步。
			_ = sess.SetTriage(it.No, mail.Triage{
				Verdict: mail.Verdict(t.Verdict), Reason: t.Reason})
			continue
		}
		need++
	}

	triaged := 0
	// 一封没判过的都没有就别调模型了——白花一次钱换一句「都判过了」。
	if need > 0 && in.Model.Provider != "" && in.Model.APIKey != "" {
		var terr error
		triaged, terr = runTriage(sess, in)
		if terr != nil {
			saveFail = append(saveFail, "分拣没做成："+terr.Error())
		}

		// 新判出来的**按账号攒起来一次写完**。一封一次的话，每封都要
		// 「读整份索引 → 解析 → 排序 → 序列化 → 写 → rename」，
		// 几十封就是 O(n²) 的磁盘读写。
		fresh := map[string]map[uint32]mailstore.Triage{}
		for _, it := range items {
			if it.Verdict == "" {
				continue
			}
			acct := it.Msg.Account
			if _, ok := known[mailstore.Key(acct, uint32(it.Msg.UID))]; ok {
				continue // 本来就是从本地填回去的，不用再写一遍
			}
			if fresh[acct] == nil {
				fresh[acct] = map[uint32]mailstore.Triage{}
			}
			fresh[acct][uint32(it.Msg.UID)] = mailstore.Triage{
				Verdict: string(it.Verdict), Reason: it.Reason}
		}
		for acct, vs := range fresh {
			if err := store.SetTriages(acct, vs); err != nil {
				// 分拣是花过一次模型调用换来的。存不下来就等于白花，
				// 而且下次同步还会再花一次——不能不吭声。
				saveFail = append(saveFail, acct+"："+err.Error())
			}
			// 手上这份也跟着更新，结尾就不用再读一次盘。
			for i := range merged[acct] {
				if t, ok := vs[merged[acct][i].UID]; ok {
					merged[acct][i].Verdict, merged[acct][i].Reason = t.Verdict, t.Reason
				}
			}
		}
	}

	msg := fmt.Sprintf("拉到 %d 封新的", len(items))
	switch {
	case len(items) == 0:
		msg = "没有新邮件"
	case triaged > 0:
		msg += fmt.Sprintf("，分拣了 %d 封", triaged)
	case need == 0:
		msg += "，都是判过的，没再麻烦模型"
	}
	if len(saveFail) > 0 {
		msg += "。" + strings.Join(saveFail, "；")
	}
	if len(res.Errors) > 0 {
		var parts []string
		for acct, err := range res.Errors {
			parts = append(parts, acct+"："+err.Error())
		}
		msg += "。有账号没拉成：" + strings.Join(parts, "；")
	}
	var out []MailItem
	for _, a := range in.Accounts {
		out = append(out, itemsOf(merged[a.Name])...)
	}
	return marshal(&MailOutput{Items: out, Message: msg})
}

// runTriage 让模型把还没判过的分拣一遍，返回判了几封和出了什么岔子。
//
// 走的是跟秘书同一套工具（list_mail + triage），不是另写一遍判断规则——
// 两处各判各的，手机上和电脑上对同一封信会给出不同结论。
func runTriage(sess *mail.Session, in MailInput) (int, error) {
	cfg := mobileAgentConfig(in.Model, in.Profile)
	ag, err := agent.Build(cfg, mail.NewSkill(sess))
	if err != nil {
		return 0, err
	}
	ag.Stream = false
	ag.Auto = agent.AutoClean
	_, err = ag.Ask(context.Background(),
		"把清单里**还没有处置标签**的邮件分拣一遍。已经有标签的跳过，"+
			"别重判。只调工具，不用写汇报。", nil)

	n := 0
	for _, it := range sess.Items() {
		if it.Verdict != "" {
			n++
		}
	}
	return n, err
}

func mailRead(store *mailstore.Store, in MailInput) string {
	acct, uid, ok := mailstore.SplitKey(in.Key)
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
	// 按 UID 直接取，不要求这封在「这次拉下来的清单」里——本地索引存着
	// 账号和 UID，那就够了。
	raw, err := sess.RawUID(acct, imap.UID(uid))
	if err != nil {
		return marshal(&MailOutput{Error: "读不出来：" + err.Error()})
	}
	// 正文只是缓存，存不下来下次重新去服务器拉一遍就是了，不用报给用户。
	_ = store.SetBody(acct, uid, string(raw))
	return marshal(&MailOutput{Body: parseMailBody(raw)})
}

// parseMailBody 把原始邮件解析成能展示的样子。
// 解析不了也别空着手回去——原样给出来总比什么都没有强。
func parseMailBody(raw []byte) *mailbox.FullBody {
	full, err := mailbox.ParseBody(raw)
	if err != nil || full == nil {
		return &mailbox.FullBody{Text: string(raw)}
	}
	return full
}

func mailClean(store *mailstore.Store, in MailInput) string {
	sess := mailSessionFor(in.Accounts)
	if sess == nil {
		return marshal(&MailOutput{Error: "还没配邮箱"})
	}

	byAccount := map[string][]imap.UID{}
	for _, k := range in.Keys {
		acct, uid, ok := mailstore.SplitKey(k)
		if !ok {
			continue
		}
		byAccount[acct] = append(byAccount[acct], imap.UID(uid))
	}
	if len(byAccount) == 0 {
		return marshal(&MailOutput{Error: "没说要清哪些"})
	}

	var done int
	var fails []string
	for acct, uids := range byAccount {
		if err := sess.CleanUIDs(acct, uids); err != nil {
			fails = append(fails, acct+"："+err.Error())
			continue
		}
		plain := make([]uint32, len(uids))
		for i, u := range uids {
			plain[i] = uint32(u)
		}
		if err := store.MarkDeleted(acct, plain); err != nil {
			// 服务器上已经移走了，本地墓碑没打上——下次同步这几封又会
			// 冒出来。说清楚，别让人以为 app 有毛病。
			fails = append(fails, acct+"：清掉了但本地没记下（"+err.Error()+"）")
		}
		done += len(uids)
	}

	// 一封都没清成就是失败，不能只在附注里写一句——界面看 error 决定
	// 要不要报警，只写进 message 的话用户以为清掉了，其实一封没动。
	if done == 0 && len(fails) > 0 {
		return marshal(&MailOutput{
			Items: listLocal(store, in.Accounts),
			Error: "一封都没清成：" + strings.Join(fails, "；")})
	}
	msg := fmt.Sprintf("清掉了 %d 封", done)
	if len(fails) > 0 {
		msg += "。没清成的：" + strings.Join(fails, "；")
	}
	return marshal(&MailOutput{Items: listLocal(store, in.Accounts), Message: msg})
}
