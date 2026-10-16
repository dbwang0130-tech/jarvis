// Package mail 把邮箱的拉取、判断、清理与发信封装成秘书可用的会话与工具。
package mail

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/mailbox"
	"github.com/dabao/jarvis/internal/state"
)

// Verdict 是一封邮件的处置建议。
type Verdict string

const (
	VerdictImportant Verdict = "important" // 需要尽快处理
	VerdictInterest  Verdict = "interest"  // 想读但不急
	VerdictNormal    Verdict = "normal"    // 留着即可
	VerdictPromo     Verdict = "promo"     // 营销推广，可清理
	VerdictSpam      Verdict = "spam"      // 垃圾/诈骗，该清理
)

// ShouldClean 表示这类邮件适合移进垃圾箱。
func (v Verdict) ShouldClean() bool { return v == VerdictPromo || v == VerdictSpam }

// Label 是展示用的中文短标签。
func (v Verdict) Label() string {
	switch v {
	case VerdictImportant:
		return "要紧"
	case VerdictInterest:
		return "关注"
	case VerdictPromo:
		return "广告"
	case VerdictSpam:
		return "垃圾"
	case VerdictNormal:
		return "普通"
	}
	return "待定"
}

// Triage 是模型对一封邮件给出的判断。
type Triage struct {
	Verdict Verdict
	Reason  string
}

// Item 把一封邮件和它的处置意见绑在一起，并带上供人引用的编号。
type Item struct {
	No  int
	Msg *mailbox.Message
	Triage
}

// Session 持有一次运行期间的全部邮箱状态：连接、拉到的邮件、草稿。
type Session struct {
	cfg config.Config

	// UnreadOnly 让 Sync 只拉未读的。
	//
	// 手机上开着它：已读的多半是在别处处理过了，再拉回来只会让清单变长、
	// 分拣多花钱。桌面端默认关着——那边有完整的邮件视图，读过的也要能翻。
	UnreadOnly bool

	mu      sync.Mutex
	clients map[string]*mailbox.Client
	items   []*Item
	byNo    map[int]*Item
	drafts  map[string]*mailbox.Draft
	nextDID int

	// seen 记下已经加载过的邮件，往前翻页时去重——IMAP 的 BEFORE
	// 只比日期不比时间，同一天的邮件会被再搜出来一次。
	seen   map[string]bool
	nextNo int
}

// NewSession 建立一个空会话，此时还没有连接任何邮箱。
func NewSession(cfg config.Config) *Session {
	return &Session{
		cfg:     cfg,
		clients: map[string]*mailbox.Client{},
		byNo:    map[int]*Item{},
		drafts:  map[string]*mailbox.Draft{},
		seen:    map[string]bool{},
	}
}

// Close 关掉所有邮箱连接。
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clients {
		c.Close()
	}
	s.clients = map[string]*mailbox.Client{}
}

// SetConfig 更新会话持有的配置（界面里增删账号后调用）。
func (s *Session) SetConfig(cfg config.Config) { s.cfg = cfg }

// HasAccounts 报告是否配置了邮箱账号。
func (s *Session) HasAccounts() bool { return len(s.cfg.Enabled()) > 0 }

// client 按需连接某个账号，连上后在会话期间复用。
func (s *Session) client(name string) (*mailbox.Client, error) {
	s.mu.Lock()
	if c, ok := s.clients[name]; ok {
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	acct, err := s.cfg.Account(name)
	if err != nil {
		return nil, err
	}
	c, err := mailbox.Connect(acct)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.clients[name] = c
	s.mu.Unlock()
	return c, nil
}

// FetchResult 汇报一次拉取的结果，包含每个账号是否出错。
type FetchResult struct {
	Total  int
	Errors map[string]error
	// New 是本次新加进来的条目，翻页时只把这批交给模型看。
	New []*Item

	// 下面几项只有增量同步（Sync）会填。
	FirstSync []string
	Remaining map[string]int
	Since     map[string]time.Time
}

// collect 并发地对每个启用账号跑一遍 fetch，汇总结果和各自的错误。
func (s *Session) collect(fetch func(*mailbox.Client, config.MailAccount) ([]*mailbox.Message, error)) ([]*mailbox.Message, map[string]error) {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		all  []*mailbox.Message
		errs = map[string]error{}
	)
	for _, a := range s.cfg.Enabled() {
		wg.Add(1)
		go func(acct config.MailAccount) {
			defer wg.Done()
			c, err := s.client(acct.Name)
			if err != nil {
				mu.Lock()
				errs[acct.Name] = err
				mu.Unlock()
				return
			}
			msgs, err := fetch(c, acct)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[acct.Name] = err
				return
			}
			all = append(all, msgs...)
		}(a)
	}
	wg.Wait()
	sort.Slice(all, func(i, j int) bool { return all[i].Date.After(all[j].Date) })
	return all, errs
}

// Fetch 并发拉取所有启用账号的收件箱，重建编号索引。
func (s *Session) Fetch(unreadOnly bool) FetchResult {
	all, errs := s.collect(func(c *mailbox.Client, a config.MailAccount) ([]*mailbox.Message, error) {
		return c.Fetch(unreadOnly)
	})

	s.mu.Lock()
	s.items = nil
	s.byNo = map[int]*Item{}
	s.seen = map[string]bool{}
	s.nextNo = 0
	added := s.appendLocked(all)
	total := len(s.items)
	s.mu.Unlock()

	return FetchResult{Total: total, Errors: errs, New: added}
}

// Sync 只拉上次同步之后新到的邮件。
// 没有同步记录的账号（首次运行，或服务器重建过索引）会全量拉一遍，
// 由 first_sync_limit 兜住量。只有拉成功的账号才推进进度。
func (s *Session) Sync() FetchResult {
	st := state.Load()

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		all       []*mailbox.Message
		errs      = map[string]error{}
		firstSync []string
		remaining = map[string]int{}
		since     = map[string]time.Time{}
		updates   = map[string]state.Account{}
	)

	for _, a := range s.cfg.Enabled() {
		wg.Add(1)
		go func(acct config.MailAccount) {
			defer wg.Done()

			c, err := s.client(acct.Name)
			if err != nil {
				mu.Lock()
				errs[acct.Name] = err
				mu.Unlock()
				return
			}
			validity, err := c.UIDValidity()
			if err != nil {
				mu.Lock()
				errs[acct.Name] = err
				mu.Unlock()
				return
			}

			prev := st.Get(acct.Name)
			lastUID, resumable := prev.Resume(validity)
			limit := acct.Limit
			if !resumable {
				limit = acct.FirstSync()
			}

			msgs, info, err := c.FetchNew(lastUID, limit, s.UnreadOnly)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs[acct.Name] = err
				return
			}
			all = append(all, msgs...)
			since[acct.Name] = prev.LastSync
			if !resumable {
				firstSync = append(firstSync, acct.Name)
			}
			if n := info.Matched - info.Fetched; n > 0 {
				remaining[acct.Name] = n
			}

			next := prev
			next.UIDValidity = validity
			next.LastSync = time.Now()
			next.SeenTotal += len(msgs)
			if info.MaxUID > next.LastUID {
				next.LastUID = info.MaxUID
			}
			updates[acct.Name] = next
		}(a)
	}
	wg.Wait()

	for name, acc := range updates {
		st.Set(name, acc)
	}
	if err := st.Save(); err != nil {
		errs["同步进度"] = err
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Date.After(all[j].Date) })

	s.mu.Lock()
	added := s.appendLocked(all)
	total := len(s.items)
	s.mu.Unlock()

	return FetchResult{
		Total: total, Errors: errs, New: added,
		FirstSync: firstSync, Remaining: remaining, Since: since,
	}
}

// LoadMore 往前再加载一批更早的邮件，追加到清单末尾。
func (s *Session) LoadMore(unreadOnly bool) FetchResult {
	edge := map[string]time.Time{}
	for _, it := range s.Items() {
		acct := it.Msg.Account
		if t, ok := edge[acct]; !ok || it.Msg.Date.Before(t) {
			edge[acct] = it.Msg.Date
		}
	}

	all, errs := s.collect(func(c *mailbox.Client, a config.MailAccount) ([]*mailbox.Message, error) {
		before, ok := edge[a.Name]
		if !ok {
			before = time.Now().AddDate(0, 0, -a.Days)
		}
		return c.FetchBefore(before, unreadOnly)
	})

	s.mu.Lock()
	added := s.appendLocked(all)
	s.mu.Unlock()

	return FetchResult{Total: len(added), Errors: errs, New: added}
}

// appendLocked 追加邮件并分配编号，跳过已经加载过的。调用方需持有锁。
func (s *Session) appendLocked(msgs []*mailbox.Message) []*Item {
	var added []*Item
	for _, m := range msgs {
		key := m.Dedup()
		if s.seen[key] {
			continue
		}
		s.seen[key] = true
		s.nextNo++
		it := &Item{No: s.nextNo, Msg: m}
		s.items = append(s.items, it)
		s.byNo[it.No] = it
		added = append(added, it)
	}
	return added
}

// Items 返回当前邮件列表的快照。
func (s *Session) Items() []*Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Item{}, s.items...)
}

// Item 按编号取一封邮件。
func (s *Session) Item(no int) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.byNo[no]
	if !ok {
		return nil, fmt.Errorf("没有编号为 %d 的邮件（当前共 %d 封）", no, len(s.items))
	}
	return it, nil
}

// SetTriage 记下模型对某封邮件的判断。
func (s *Session) SetTriage(no int, t Triage) error {
	it, err := s.Item(no)
	if err != nil {
		return err
	}
	s.mu.Lock()
	it.Triage = t
	s.mu.Unlock()
	return nil
}

// Remove 把已经移出收件箱的邮件从列表里摘掉，编号保持不变。
func (s *Session) Remove(nos []int) {
	drop := make(map[int]bool, len(nos))
	for _, no := range nos {
		drop[no] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	for _, it := range s.items {
		if drop[it.No] {
			delete(s.byNo, it.No)
			continue
		}
		kept = append(kept, it)
	}
	s.items = kept
}

// MarkSeen 把指定编号的邮件在服务器上标成已读或未读。
// auto 为真时这是 triage 之后的自动标记，只对配置里开了 mark_read 的账号生效。
func (s *Session) MarkSeen(nos []int, seen, auto bool) (done int, fails []string) {
	touched := s.pickForMark(nos, seen, auto)
	group := map[string][]imap.UID{}
	for acct, items := range touched {
		for _, it := range items {
			group[acct] = append(group[acct], it.Msg.UID)
		}
	}

	for acct, uids := range group {
		c, err := s.client(acct)
		if err != nil {
			fails = append(fails, acct+": "+err.Error())
			continue
		}
		if err := c.MarkSeen(uids, seen); err != nil {
			fails = append(fails, acct+": "+err.Error())
			continue
		}
		s.mu.Lock()
		for _, it := range touched[acct] {
			it.Msg.Unread = !seen
		}
		s.mu.Unlock()
		done += len(uids)
	}
	return done, fails
}

// pickForMark 挑出真正需要改标记的邮件，按账号分组。
func (s *Session) pickForMark(nos []int, seen, auto bool) map[string][]*Item {
	out := map[string][]*Item{}
	for _, no := range nos {
		it, err := s.Item(no)
		if err != nil {
			continue
		}
		acct := it.Msg.Account
		if auto {
			a, err := s.cfg.Account(acct)
			if err != nil || !a.MarksRead() {
				continue
			}
		}
		if it.Msg.Unread == !seen {
			continue // 已经是目标状态
		}
		out[acct] = append(out[acct], it)
	}
	return out
}

// Counts 按处置类别统计，用于简报头部。
func (s *Session) Counts() map[Verdict]int {
	out := map[Verdict]int{}
	for _, it := range s.Items() {
		if it.Verdict != "" {
			out[it.Verdict]++
		}
	}
	return out
}

// AddDraft 存一份草稿并返回它的引用 id。
func (s *Session) AddDraft(d *mailbox.Draft) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextDID++
	d.ID = "d" + strconv.Itoa(s.nextDID)
	s.drafts[d.ID] = d
	return d.ID
}

// Draft 按 id 取草稿。
func (s *Session) Draft(id string) (*mailbox.Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drafts[strings.TrimSpace(id)]
	if !ok {
		return nil, fmt.Errorf("没有编号为 %q 的草稿", id)
	}
	return d, nil
}

// DefaultAccount 在用户没指明用哪个邮箱发信时，取第一个启用的账号。
func (s *Session) DefaultAccount() config.MailAccount {
	if list := s.cfg.Enabled(); len(list) > 0 {
		return list[0]
	}
	return config.MailAccount{}
}

// Digest 把整份清单压成给模型看的文本。
func (s *Session) Digest() string { return render(s.Items()) }

// DigestOf 完整渲染指定的一批邮件，翻页后只把新的那批交给模型。
func (s *Session) DigestOf(items []*Item) string { return render(items) }

func render(items []*Item) string {
	if len(items) == 0 {
		return "（没有邮件）"
	}
	var b strings.Builder
	for _, it := range items {
		m := it.Msg
		fmt.Fprintf(&b, "[%d] %s | 发件人: %s <%s> | 主题: %s | %s | %s",
			it.No, m.Account, m.From(), m.FromAddr, m.Subject,
			humanTime(m.Date), m.BulkHint())
		switch {
		case it.Verdict != "":
			fmt.Fprintf(&b, " | 已判:%s", it.Verdict.Label())
		case m.Unread:
			b.WriteString(" | 未读")
		}
		b.WriteString("\n")
	}
	// **正文不往外发**。分拣靠发件人、主题和群发头（List-Unsubscribe、
	// Precedence 这些）就够判了——推广和垃圾的信号全在头里。而正文是私人
	// 内容，为了给一封信打个标签就把它整段发给模型，代价和收益不成比例。
	//
	// 拿不准的那几封也不给正文——判成 normal 留着就是了，误判一个标签的
	// 代价，远小于把一封私人邮件整段发出去。
	//
	// 正文不发之后，原来那套「前 100 封给详细、后面给简略」的截断也没用了——
	// 它存在的理由就是控制正文体积，现在每封都只有一行。
	b.WriteString("\n（只给发件人、主题和群发标记。正文一个字都不外发，" +
		"也没有任何工具能读到它。就按这些判断；拿不准就判 normal 留着。）\n")
	return b.String()
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d分钟前", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d小时前", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d天前", int(d.Hours()/24))
	}
	return t.Format("01-02")
}

// Clean 按清单编号把邮件移进垃圾箱，返回成功的封数和失败说明。
//
// 编号是**给模型看的东西**——它只会说「把 3、5、7 清掉」。所以这里只负责
// 把编号翻成账号和 UID，真正的活交给 CleanUIDs。「移到哪个垃圾箱」
// 「失败了要不要从清单里摘掉」这类判断只有那一份。
func (s *Session) Clean(nos []int) (done int, fails []string) {
	group := map[string][]imap.UID{}
	for _, no := range nos {
		it, err := s.Item(no)
		if err != nil {
			fails = append(fails, err.Error())
			continue
		}
		group[it.Msg.Account] = append(group[it.Msg.Account], it.Msg.UID)
	}
	for acct, uids := range group {
		if err := s.CleanUIDs(acct, uids); err != nil {
			fails = append(fails, acct+": "+err.Error())
			continue
		}
		done += len(uids)
	}
	return done, fails
}

// MarkSeenUIDs 按账号和 UID 把邮件标成已读，不要求它们在当前清单里。
//
// 跟 CleanUIDs 同一个道理：MarkSeen 走的是清单编号，那套编号只对「这次
// 拉下来的那批」有效；手机上用户看的是本地存的那份，里面的邮件多半早就
// 不在当前会话里了。
func (s *Session) MarkSeenUIDs(account string, uids []imap.UID, seen bool) error {
	if len(uids) == 0 {
		return nil
	}
	c, err := s.client(account)
	if err != nil {
		return err
	}
	return c.MarkSeen(uids, seen)
}

// CleanUIDs 按账号和 UID 直接把邮件移进垃圾箱，不要求它们在当前清单里。
//
// Clean 走的是清单编号，那套编号只对「这次拉下来的那批」有效。手机上
// 邮件存在本地，用户看的是本地那份，里面的邮件多半早就不在当前会话里了——
// 让他为了清一封信先去同步一次，是拿实现细节为难人。
func (s *Session) CleanUIDs(account string, uids []imap.UID) error {
	if len(uids) == 0 {
		return nil
	}
	c, err := s.client(account)
	if err != nil {
		return err
	}
	junk, err := c.JunkMailbox()
	if err != nil {
		return err
	}
	if err := c.MoveTo(uids, junk); err != nil {
		return err
	}
	// 清单里如果正好有这几封，一并摘掉，免得界面上还留着。
	var nos []int
	for _, it := range s.Items() {
		if it.Msg.Account != account {
			continue
		}
		for _, u := range uids {
			if it.Msg.UID == u {
				nos = append(nos, it.No)
			}
		}
	}
	s.Remove(nos)
	return nil
}

// RawUID 按账号和 UID 直接取原始邮件，同样不要求它在当前清单里。
func (s *Session) RawUID(account string, uid imap.UID) ([]byte, error) {
	c, err := s.client(account)
	if err != nil {
		return nil, err
	}
	return c.ReadRaw(uid)
}

// 这里原来有个 Body(no)：按清单编号拉整封正文，read_mail 靠它。
// 两个一起删了——正文只经过手机本地和邮件服务器，不经过模型，
// 留着一个现成的「按编号取正文」摆在这儿，早晚有人把它接回工具表里。
//
// 界面要显示原文走的是另一条：邮件工具的 Mail(read) → RawUID
// 给 Swift 渲染，全程不进模型的上下文。
