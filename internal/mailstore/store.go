// Package mailstore 把拉下来的邮件存在本地，下次不用重拉。
//
// # 为什么不用 Maildir
//
// Maildir 是 Unix 邮件客户端的事实标准，但它的两个设计目标我们都用不上：
// 免锁并发（靠先写 tmp 再 rename 的原子重命名）和跨客户端互操作。手机的
// 沙箱里没有第二个客户端，也没有别的写入者。代价倒是实打实的——一封一个
// 文件，几千封就是几千个 inode，而 iOS 上的文件系统调用并不便宜。
//
// 所以走「头部索引 + 正文分离」：
//
//   - 每个账号一个索引文件，存发件人、主题、日期、摘要、分拣结论。
//     列表页要的全在这儿，读一个文件就够。
//   - 正文按 UID 单独存，点开才读。正文占体积的九成以上，塞进索引会让
//     每次开列表都要把几 MB 读进来。
//
// 存在设备本地，**不进 iCloud**：邮件体积大、变化频繁，塞进 iCloud 只会
// 拖慢同步还容易撞车。对话记录也是同样的道理。
package mailstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Record 是索引里的一封邮件：列表页要显示的全部内容，不含正文。
type Record struct {
	Account string    `json:"account"`
	UID     uint32    `json:"uid"`
	From    string    `json:"from"`
	Addr    string    `json:"addr"`
	Subject string    `json:"subject"`
	Date    time.Time `json:"date"`
	Unread  bool      `json:"unread"`
	Snippet string    `json:"snippet"`

	// 分拣结论。空的表示还没分拣过——下次同步时只需要分拣这些，
	// 已经判过的不必再花一次模型调用。
	Verdict string `json:"verdict,omitempty"`
	Reason  string `json:"reason,omitempty"`

	// HasBody 表示正文已经存下来了，点开不用再连服务器。
	HasBody bool `json:"has_body"`
	// Deleted 是本地的墓碑：清理过的邮件不再显示，但记号要留着，
	// 否则下次同步又把它拉回来。
	Deleted bool `json:"deleted,omitempty"`
}

// Key 是一封邮件在存储里的身份。UID 只在单个邮箱内唯一，得带上账号。
func (r Record) Key() string { return r.Account + ":" + fmt.Sprint(r.UID) }

// Store 是本地邮件库。
type Store struct {
	dir string
	mu  sync.Mutex
}

// New 打开（必要时建立）一个邮件库。
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "body"), 0o700); err != nil {
		return nil, fmt.Errorf("建不了邮件库目录：%w", err)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) indexPath(account string) string {
	return filepath.Join(s.dir, safeName(account)+".json")
}

func (s *Store) bodyPath(account string, uid uint32) string {
	return filepath.Join(s.dir, "body", fmt.Sprintf("%s-%d.txt", safeName(account), uid))
}

// safeName 把账号名压成能当文件名的样子。账号名是用户起的，
// 里面可能有斜杠、冒号、空格。
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "account"
	}
	return b.String()
}

// Load 读出一个账号的全部索引，按日期新的在前。
// 文件不存在不算错误——第一次用就是这样。
func (s *Store) Load(account string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(account)
}

func (s *Store) loadLocked(account string) ([]Record, error) {
	raw, err := os.ReadFile(s.indexPath(account))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []Record
	if err := json.Unmarshal(raw, &recs); err != nil {
		// 索引坏了不能把人挡在门外：当成空的重新攒，大不了重拉一次。
		// 邮件本身在服务器上，本地这份只是缓存。
		return nil, nil
	}
	sortByDate(recs)
	return recs, nil
}

// Merge 把新拉到的邮件并进索引，返回并进去之后的全部记录。
//
// 已经存在的邮件**保留本地的分拣结论和墓碑**，只更新未读状态这类会变的。
// 不这么做的话，每次同步都会把判过的邮件重新变成未分拣，白花模型调用。
func (s *Store) Merge(account string, incoming []Record) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	old, err := s.loadLocked(account)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]int, len(old))
	for i, r := range old {
		byKey[r.Key()] = i
	}

	for _, in := range incoming {
		in.Account = account
		i, ok := byKey[in.Key()]
		if !ok {
			old = append(old, in)
			byKey[in.Key()] = len(old) - 1
			continue
		}
		// 已有的：服务器说了算的字段更新，本地攒下来的保留。
		keep := old[i]
		keep.From, keep.Addr, keep.Subject = in.From, in.Addr, in.Subject
		keep.Date, keep.Unread = in.Date, in.Unread
		if in.Snippet != "" {
			keep.Snippet = in.Snippet
		}
		old[i] = keep
	}

	sortByDate(old)
	return old, s.saveLocked(account, old)
}

// Save 整份写回。
func (s *Store) Save(account string, recs []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(account, recs)
}

func (s *Store) saveLocked(account string, recs []Record) error {
	raw, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	// 先写临时文件再改名：写到一半断电或被系统杀掉的话，
	// 原来那份还是完整的，而不是半截 JSON。
	tmp := s.indexPath(account) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath(account))
}

// SetTriage 记下一封邮件的分拣结论。
func (s *Store) SetTriage(account string, uid uint32, verdict, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.loadLocked(account)
	if err != nil {
		return err
	}
	for i := range recs {
		if recs[i].UID == uid {
			recs[i].Verdict, recs[i].Reason = verdict, reason
			return s.saveLocked(account, recs)
		}
	}
	return fmt.Errorf("没有 UID 为 %d 的邮件", uid)
}

// MarkDeleted 打上墓碑。不真删记录——删了的话下次同步又会把它拉回来。
func (s *Store) MarkDeleted(account string, uids []uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.loadLocked(account)
	if err != nil {
		return err
	}
	want := make(map[uint32]bool, len(uids))
	for _, u := range uids {
		want[u] = true
	}
	for i := range recs {
		if want[recs[i].UID] {
			recs[i].Deleted = true
			_ = os.Remove(s.bodyPath(account, recs[i].UID)) // 正文不必留着占地方
			recs[i].HasBody = false
		}
	}
	return s.saveLocked(account, recs)
}

// Body 读一封邮件的正文。没存过返回空串和 false，调用方再去服务器拉。
func (s *Store) Body(account string, uid uint32) (string, bool) {
	raw, err := os.ReadFile(s.bodyPath(account, uid))
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// SetBody 存下一封邮件的正文，并在索引里标上。
func (s *Store) SetBody(account string, uid uint32, body string) error {
	if err := os.WriteFile(s.bodyPath(account, uid), []byte(body), 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, err := s.loadLocked(account)
	if err != nil {
		return err
	}
	for i := range recs {
		if recs[i].UID == uid {
			recs[i].HasBody = true
			return s.saveLocked(account, recs)
		}
	}
	return nil
}

// MaxUID 返回索引里见过的最大 UID，用来向服务器要增量。
// 没有记录时返回 0，调用方按首次同步处理。
func (s *Store) MaxUID(account string) uint32 {
	recs, err := s.Load(account)
	if err != nil {
		return 0
	}
	var max uint32
	for _, r := range recs {
		if r.UID > max {
			max = r.UID
		}
	}
	return max
}

// Reset 清掉一个账号的全部本地邮件。
// UIDVALIDITY 变了就得这么办——服务器重建过索引，之前记的 UID 全部作废。
func (s *Store) Reset(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	recs, _ := s.loadLocked(account)
	for _, r := range recs {
		_ = os.Remove(s.bodyPath(account, r.UID))
	}
	return os.Remove(s.indexPath(account))
}

func sortByDate(recs []Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		return recs[i].Date.After(recs[j].Date)
	})
}
