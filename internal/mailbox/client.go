package mailbox

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/config"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

const (
	// 做简报时只取正文开头：分类和摘要用不上全文，省流量也省 token。
	snippetBytes = 3072
	snippetChars = 1200
	// 读全文时取得多一些，够 agent 起草回信。
	fullBytes = 64 * 1024
	fullChars = 8000
	// 降级模式下整封取回的字节数：够覆盖头部加正文开头。
	fallbackBytes = 16 * 1024
)

// 这些头是判断群发/营销邮件最省事的硬信号。
var wantedHeaders = []string{"List-Unsubscribe", "List-Id", "Precedence", "Auto-Submitted"}

// Client 是连到单个账号的 IMAP 会话。
type Client struct {
	Account config.MailAccount
	c       *imapclient.Client
	junk    JunkResult
	// 有些服务器返回的 BODYSTRUCTURE 不符合 RFC，go-imap 解析不了。
	// 置位后改成整封取回、自己解析 MIME。
	noBodyStructure bool
}

// Connect 建立 TLS 连接并登录。
func Connect(a config.MailAccount) (*Client, error) {
	pw, err := a.Secret()
	if err != nil {
		return nil, err
	}
	opts := &imapclient.Options{
		TLSConfig:   &tls.Config{ServerName: a.Host, MinVersion: tls.VersionTLS12},
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}
	c, err := imapclient.DialTLS(a.Addr(), opts)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", a.Addr(), err)
	}
	if err := c.Login(a.User, pw).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("登录 %s 失败（检查是否用了授权码而非登录密码）: %w", a.Name, err)
	}
	return &Client{Account: a, c: c}, nil
}

// Close 登出并断开。
func (cl *Client) Close() {
	if cl.c != nil {
		cl.c.Logout().Wait()
		cl.c.Close()
	}
}

// JunkMailbox 返回本账号的垃圾箱路径。
func (cl *Client) JunkMailbox() (string, error) {
	r, err := cl.DetectJunk()
	return r.Mailbox, err
}

// DetectJunk 判定垃圾箱，并说明是怎么判出来的。结果在连接期间缓存。
func (cl *Client) DetectJunk() (JunkResult, error) {
	if cl.junk.Mailbox != "" {
		return cl.junk, nil
	}
	// 配置里写死的优先，且不强制校验它存在——服务器 LIST 出问题时这是唯一退路。
	if cl.Account.Junk != "" {
		cl.junk = JunkResult{cl.Account.Junk, "配置里指定"}
		return cl.junk, nil
	}
	boxes, err := cl.list()
	if err != nil {
		return JunkResult{}, err
	}
	r, ok := detectJunk(boxes)
	if !ok {
		return JunkResult{}, junkNotFound(cl.Account.Name, boxes)
	}
	cl.junk = r
	return r, nil
}

// list 列出所有文件夹。SPECIAL-USE 是可选扩展，QQ 邮箱这类服务器不支持，
// 带上 RETURN (SPECIAL-USE) 会被直接回 BAD，所以先看服务器认不认。
func (cl *Client) list() ([]*imap.ListData, error) {
	var opts imap.ListOptions
	if caps := cl.c.Caps(); caps.Has(imap.CapSpecialUse) {
		opts.ReturnSpecialUse = true
	}
	boxes, err := cl.c.List("", "*", &opts).Collect()
	if err != nil {
		return nil, fmt.Errorf("列出邮箱文件夹失败: %w", err)
	}
	return boxes, nil
}

// Mailboxes 列出所有文件夹，供 `mail boxes` 排查用。
func (cl *Client) Mailboxes() ([]string, error) {
	boxes, err := cl.list()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, b := range boxes {
		line := b.Mailbox
		if len(b.Attrs) > 0 {
			var at []string
			for _, a := range b.Attrs {
				at = append(at, string(a))
			}
			line += "  " + strings.Join(at, " ")
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out, nil
}

// SyncInfo 描述一次拉取在服务器那边的情况，增量同步靠它记进度。
type SyncInfo struct {
	// UIDValidity 变了说明服务器重建过索引，之前记的 UID 作废。
	UIDValidity uint32
	// MaxUID 是这次搜索命中的最大 UID，下次从它之后接着拉。
	MaxUID uint32
	// Matched 是搜索命中的总数，Fetched 是受 limit 限制后实际取回的数量。
	Matched, Fetched int
}

// Truncated 说明这次因为条数上限没把命中的邮件全取回来。
func (i SyncInfo) Truncated() bool { return i.Matched > i.Fetched }

// Fetch 拉取收件箱里最近 Days 天、最多 Limit 封邮件。
// 全程用 PEEK，不会把邮件标记成已读。
func (cl *Client) Fetch(unreadOnly bool) ([]*Message, error) {
	msgs, _, err := cl.search(&imap.SearchCriteria{
		Since: time.Now().AddDate(0, 0, -cl.Account.Days),
	}, unreadOnly, cl.Account.Limit)
	return msgs, err
}

// FetchBefore 拉取比 before 更早的最新一批，用来往前翻页。
// 不受配置里 Days 的限制——往前翻本来就是要看窗口之外的信。
// 注意 IMAP 的 BEFORE 只比较日期不比时间，同一天的邮件会再出现一次，
// 由调用方按 UID 去重。
func (cl *Client) FetchBefore(before time.Time, unreadOnly bool) ([]*Message, error) {
	msgs, _, err := cl.search(&imap.SearchCriteria{Before: before}, unreadOnly, cl.Account.Limit)
	return msgs, err
}

// FetchNew 只拉 UID 比 sinceUID 大的邮件，也就是上次同步之后新到的那些。
// sinceUID 为 0 表示没有同步记录，这时拉全部，由 limit 兜住量（limit<=0 为不限）。
//
// 用 UID 而不是时间划界：IMAP 的 SINCE 只比较日期不比时间，
// 按时间增量会把上次同步当天的邮件重新拉一遍。
func (cl *Client) FetchNew(sinceUID uint32, limit int) ([]*Message, SyncInfo, error) {
	criteria := &imap.SearchCriteria{}
	if sinceUID > 0 {
		var set imap.UIDSet
		set.AddRange(imap.UID(sinceUID+1), 0) // Stop 为 0 即 *，没有上界
		criteria.UID = []imap.UIDSet{set}
	}
	return cl.search(criteria, false, limit)
}

func (cl *Client) search(criteria *imap.SearchCriteria, unreadOnly bool, limit int) ([]*Message, SyncInfo, error) {
	msgs, info, err := cl.fetchOnce(criteria, unreadOnly, limit)
	if err == nil {
		return msgs, info, nil
	}
	// 界面开着不动时 IMAP 会被服务器掐掉，重新拉取正撞上这个。重连再来一次。
	if isConnErr(err) {
		if rerr := cl.reconnect(); rerr != nil {
			return nil, info, fmt.Errorf("连接已断开且重连失败: %w", rerr)
		}
		msgs, info, err = cl.fetchOnce(criteria, unreadOnly, limit)
		if err == nil {
			return msgs, info, nil
		}
	}
	if cl.noBodyStructure {
		return nil, info, err
	}
	// 讯飞用的 Coremail 会在 BODYSTRUCTURE 的 body-fld-enc 位置返回 NIL，
	// 这不符合 RFC 3501，go-imap 解析失败并会关掉连接。
	// 退一步：重连，改成整封取回自己解析 MIME。
	cl.noBodyStructure = true
	if rerr := cl.reconnect(); rerr != nil {
		return nil, info, err // 重连都不行，报原来那个错更有用
	}
	return cl.fetchOnce(criteria, unreadOnly, limit)
}

// reconnect 重建连接。解析失败会让 go-imap 关掉底层连接，只能重来。
func (cl *Client) reconnect() error {
	if cl.c != nil {
		cl.c.Close()
	}
	fresh, err := Connect(cl.Account)
	if err != nil {
		return err
	}
	cl.c = fresh.c
	// junk 和 noBodyStructure 是服务器的属性，不随连接变，留着省一次往返。
	return nil
}

func (cl *Client) fetchOnce(criteria *imap.SearchCriteria, unreadOnly bool, limit int) ([]*Message, SyncInfo, error) {
	var info SyncInfo

	sel, err := cl.c.Select("INBOX", nil).Wait()
	if err != nil {
		return nil, info, fmt.Errorf("打开 INBOX 失败: %w", err)
	}
	if sel != nil {
		info.UIDValidity = sel.UIDValidity
	}

	if unreadOnly {
		c := *criteria
		c.NotFlag = []imap.Flag{imap.FlagSeen}
		criteria = &c
	}
	res, err := cl.c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, info, fmt.Errorf("搜索邮件失败: %w", err)
	}
	uids := res.AllUIDs()
	// 不能指望服务器一定按序返回，自己排一遍，末尾才可靠地是最新的。
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })

	info.Matched = len(uids)
	if len(uids) == 0 {
		return nil, info, nil
	}
	info.MaxUID = uint32(uids[len(uids)-1])

	// 超量时只要最新的那批。
	if limit > 0 && len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	info.Fetched = len(uids)

	msgs, structs, err := cl.fetchMeta(uids)
	if err != nil {
		return nil, info, err
	}
	cl.fetchSnippets(msgs, structs)

	// 新邮件排前面。
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].Date.After(msgs[j].Date) })
	return msgs, info, nil
}

// fetchMeta 取信封、标志与群发相关的头，同时留下正文结构供第二轮取正文。
func (cl *Client) fetchMeta(uids []imap.UID) ([]*Message, map[imap.UID]imap.BodyStructure, error) {
	opts := &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		RFC822Size:   true,
		BodySection: []*imap.FetchItemBodySection{{
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: wantedHeaders,
			Peek:         true,
		}},
	}
	if cl.noBodyStructure {
		// 拿不到正文结构，就把整封开头取回来自己解析。
		opts.BodySection = append(opts.BodySection, &imap.FetchItemBodySection{
			Peek:    true,
			Partial: &imap.SectionPartial{Offset: 0, Size: fallbackBytes},
		})
	} else {
		opts.BodyStructure = &imap.FetchItemBodyStructure{}
	}
	buf, err := cl.c.Fetch(imap.UIDSetNum(uids...), opts).Collect()
	if err != nil {
		return nil, nil, fmt.Errorf("拉取邮件头失败: %w", err)
	}

	msgs := make([]*Message, 0, len(buf))
	structs := make(map[imap.UID]imap.BodyStructure, len(buf))
	for _, b := range buf {
		m := &Message{Account: cl.Account.Name, UID: b.UID, Size: b.RFC822Size}
		if b.Envelope != nil {
			m.Subject = decodeWord(b.Envelope.Subject)
			m.Date = b.Envelope.Date
			if len(b.Envelope.From) > 0 {
				f := b.Envelope.From[0]
				m.FromName = decodeWord(f.Name)
				m.FromAddr = f.Mailbox + "@" + f.Host
			}
		}
		if m.Date.IsZero() {
			m.Date = b.InternalDate
		}
		m.Unread = !hasFlag(b.Flags, imap.FlagSeen)
		for _, sec := range b.BodySection {
			if sec.Section != nil && sec.Section.Specifier == imap.PartSpecifierHeader {
				parseBulkHeaders(m, sec.Bytes)
				continue
			}
			// 降级模式下多取的那段整封内容。
			m.Snippet = extractFromRawMIME(sec.Bytes, snippetChars)
		}
		if b.BodyStructure != nil {
			structs[b.UID] = b.BodyStructure
		}
		msgs = append(msgs, m)
	}
	return msgs, structs, nil
}

// fetchSnippets 取每封邮件正文开头。按 part 路径分组批量取，
// 绝大多数邮件落在同样的一两条路径上，通常两三轮就够。
func (cl *Client) fetchSnippets(msgs []*Message, structs map[imap.UID]imap.BodyStructure) {
	type target struct {
		part *imap.BodyStructureSinglePart
		path []int
	}
	byPath := map[string][]imap.UID{}
	targets := map[imap.UID]target{}

	for _, m := range msgs {
		bs, ok := structs[m.UID]
		if !ok {
			continue
		}
		part, path := pickTextPart(bs)
		if part == nil {
			continue
		}
		key := pathKey(path)
		byPath[key] = append(byPath[key], m.UID)
		targets[m.UID] = target{part, path}
	}

	index := map[imap.UID]*Message{}
	for _, m := range msgs {
		index[m.UID] = m
	}

	for key, uids := range byPath {
		path := targets[uids[0]].path
		sec := &imap.FetchItemBodySection{
			Part:    path,
			Peek:    true,
			Partial: &imap.SectionPartial{Offset: 0, Size: snippetBytes},
		}
		if len(path) == 0 {
			sec.Specifier = imap.PartSpecifierText
		}
		buf, err := cl.c.Fetch(imap.UIDSetNum(uids...),
			&imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
		if err != nil {
			// 单条路径取不到正文不致命，标题和发件人仍足以分类。
			continue
		}
		_ = key
		for _, b := range buf {
			m, ok := index[b.UID]
			if !ok || len(b.BodySection) == 0 {
				continue
			}
			t := targets[b.UID]
			m.Snippet = decodeText(b.BodySection[0].Bytes, t.part, snippetChars)
		}
	}

	// 按 part 路径分段取回未必管用：讯飞的 Coremail 对
	// BODY.PEEK[1]<0.3072> 会返回空，正文结构看着正常却一个字都拿不到。
	// 凡是到这儿还没正文的，统一用整封再兜一次。
	var missing []imap.UID
	for _, m := range msgs {
		if m.Snippet == "" {
			missing = append(missing, m.UID)
		}
	}
	if len(missing) > 0 {
		cl.fillFromRaw(index, missing)
	}
}

// fillFromRaw 整封取回开头一段，自己走 MIME 树补上正文。
// 这是最后的退路，取不到就算了——标题和发件人仍然够做判断。
func (cl *Client) fillFromRaw(index map[imap.UID]*Message, uids []imap.UID) {
	buf, err := cl.c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{{
			Peek:    true,
			Partial: &imap.SectionPartial{Offset: 0, Size: fallbackBytes},
		}},
	}).Collect()
	if err != nil {
		return
	}
	for _, b := range buf {
		m, ok := index[b.UID]
		if !ok || len(b.BodySection) == 0 {
			continue
		}
		m.Snippet = extractFromRawMIME(b.BodySection[0].Bytes, snippetChars)
	}
}

// MoveTo 把指定邮件移动到目标文件夹。服务器不支持 MOVE 时由库退化为 COPY+STORE。
func (cl *Client) MoveTo(uids []imap.UID, dest string) error {
	if len(uids) == 0 {
		return nil
	}
	err := cl.moveOnce(uids, dest)
	if err != nil && isConnErr(err) {
		if rerr := cl.reconnect(); rerr != nil {
			return fmt.Errorf("连接已断开且重连失败: %w", rerr)
		}
		err = cl.moveOnce(uids, dest)
	}
	return err
}

func (cl *Client) moveOnce(uids []imap.UID, dest string) error {
	if _, err := cl.c.Select("INBOX", nil).Wait(); err != nil {
		return fmt.Errorf("打开 INBOX 失败: %w", err)
	}
	if _, err := cl.c.Move(imap.UIDSetNum(uids...), dest).Wait(); err != nil {
		return fmt.Errorf("移动到 %s 失败: %w", dest, err)
	}
	return nil
}

// isConnErr 判断错误是不是连接已经不可用，这类错误重连后重试有意义。
func isConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	for _, s := range []string{
		"use of closed network connection", "broken pipe",
		"connection reset", "EOF", "i/o timeout", "connection refused",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func hasFlag(flags []imap.Flag, want imap.Flag) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

func pathKey(path []int) string {
	var b strings.Builder
	for i, p := range path {
		if i > 0 {
			b.WriteByte('.')
		}
		fmt.Fprintf(&b, "%d", p)
	}
	return b.String()
}

// ReadFull 取回单封邮件的完整信息与正文，供 agent 起草回信时参考。
func (cl *Client) ReadFull(uid imap.UID) (*Message, error) {
	if _, err := cl.c.Select("INBOX", nil).Wait(); err != nil {
		return nil, fmt.Errorf("打开 INBOX 失败: %w", err)
	}
	msgs, structs, err := cl.fetchMeta([]imap.UID{uid})
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("邮件 %d 不存在（可能已被移走）", uid)
	}
	m := msgs[0]

	if cl.noBodyStructure {
		// 没有正文结构可用，整封多取一些自己解析。
		buf, err := cl.c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
			UID: true,
			BodySection: []*imap.FetchItemBodySection{{
				Peek:    true,
				Partial: &imap.SectionPartial{Offset: 0, Size: fullBytes},
			}},
		}).Collect()
		if err == nil && len(buf) > 0 && len(buf[0].BodySection) > 0 {
			m.Snippet = extractFromRawMIME(buf[0].BodySection[0].Bytes, fullChars)
		}
		return m, nil
	}

	bs, ok := structs[uid]
	if !ok {
		return m, nil
	}
	part, path := pickTextPart(bs)
	if part == nil {
		return m, nil
	}
	sec := &imap.FetchItemBodySection{
		Part:    path,
		Peek:    true,
		Partial: &imap.SectionPartial{Offset: 0, Size: fullBytes},
	}
	if len(path) == 0 {
		sec.Specifier = imap.PartSpecifierText
	}
	buf, err := cl.c.Fetch(imap.UIDSetNum(uid),
		&imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil || len(buf) == 0 || len(buf[0].BodySection) == 0 {
		return m, nil
	}
	m.Snippet = decodeText(buf[0].BodySection[0].Bytes, part, fullChars)
	return m, nil
}

// MessageID 取回邮件的 Message-ID 与 References，回信时用来挂回原会话。
func (cl *Client) MessageID(uid imap.UID) (msgID, refs string, err error) {
	if _, err := cl.c.Select("INBOX", nil).Wait(); err != nil {
		return "", "", err
	}
	opts := &imap.FetchOptions{
		UID: true,
		BodySection: []*imap.FetchItemBodySection{{
			Specifier:    imap.PartSpecifierHeader,
			HeaderFields: []string{"Message-Id", "References"},
			Peek:         true,
		}},
	}
	buf, err := cl.c.Fetch(imap.UIDSetNum(uid), opts).Collect()
	if err != nil || len(buf) == 0 || len(buf[0].BodySection) == 0 {
		return "", "", err
	}
	raw := buf[0].BodySection[0].Bytes
	if !bytes.HasSuffix(raw, []byte("\n\n")) {
		raw = append(append([]byte{}, raw...), '\n')
	}
	hdr, _ := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw))).ReadMIMEHeader()
	return hdr.Get("Message-Id"), hdr.Get("References"), nil
}

// MarkSeen 给邮件加上或去掉已读标记。
// 这是可逆操作：标错了用 seen=false 就能标回未读。
func (cl *Client) MarkSeen(uids []imap.UID, seen bool) error {
	if len(uids) == 0 {
		return nil
	}
	err := cl.markSeenOnce(uids, seen)
	if err != nil && isConnErr(err) {
		if rerr := cl.reconnect(); rerr != nil {
			return fmt.Errorf("连接已断开且重连失败: %w", rerr)
		}
		err = cl.markSeenOnce(uids, seen)
	}
	return err
}

func (cl *Client) markSeenOnce(uids []imap.UID, seen bool) error {
	if _, err := cl.c.Select("INBOX", nil).Wait(); err != nil {
		return fmt.Errorf("打开 INBOX 失败: %w", err)
	}
	op := imap.StoreFlagsAdd
	if !seen {
		op = imap.StoreFlagsDel
	}
	cmd := cl.c.Store(imap.UIDSetNum(uids...), &imap.StoreFlags{
		Op:     op,
		Silent: true,
		Flags:  []imap.Flag{imap.FlagSeen},
	}, nil)
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("标记已读状态失败: %w", err)
	}
	return nil
}

// UIDValidity 打开收件箱并返回它的 UIDVALIDITY。
// 增量同步前要先确认这个值没变，变了说明服务器重建过索引、记下的 UID 全部作废。
func (cl *Client) UIDValidity() (uint32, error) {
	sel, err := cl.c.Select("INBOX", nil).Wait()
	if err != nil {
		if isConnErr(err) {
			if rerr := cl.reconnect(); rerr != nil {
				return 0, fmt.Errorf("连接已断开且重连失败: %w", rerr)
			}
			sel, err = cl.c.Select("INBOX", nil).Wait()
		}
		if err != nil {
			return 0, fmt.Errorf("打开 INBOX 失败: %w", err)
		}
	}
	if sel == nil {
		return 0, nil
	}
	return sel.UIDValidity, nil
}
