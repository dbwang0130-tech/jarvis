// Package state 记住每个账号同步到哪儿了，好让下次只拉新邮件。
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dabao/jarvis/internal/config"
)

// Account 是一个账号的同步进度。
//
// 用 UID 而不是时间来划增量：IMAP 的 SINCE 只比较日期不比较时间，
// 按时间增量会把上次同步当天的邮件重新拉一遍。UID 在邮箱里单调递增，
// 拿它划界既精确又不会重复。
type Account struct {
	LastUID uint32 `json:"last_uid"`
	// UIDValidity 变了说明服务器重建过索引，之前记的 UID 全部作废，要重新全量。
	UIDValidity uint32    `json:"uid_validity"`
	LastSync    time.Time `json:"last_sync"`
	// SeenTotal 是至今一共见过多少封，只用于展示。
	SeenTotal int `json:"seen_total"`
}

// State 是所有账号的同步进度。
type State struct {
	Accounts map[string]Account `json:"accounts"`
}

// Path 返回状态文件路径。它由程序自己维护，用户不用编辑。
func Path() string { return filepath.Join(config.Dir(), "state.json") }

// Load 读取状态。文件不存在时返回空状态，不算错误——首次运行就是这样。
func Load() *State {
	s := &State{Accounts: map[string]Account{}}
	raw, err := os.ReadFile(Path())
	if err != nil {
		return s
	}
	if err := json.Unmarshal(raw, s); err != nil {
		// 状态文件坏了不该让工具没法用，当成首次运行重来一遍即可。
		return &State{Accounts: map[string]Account{}}
	}
	if s.Accounts == nil {
		s.Accounts = map[string]Account{}
	}
	return s
}

// Save 落盘。先写临时文件再改名，避免中途崩溃留下半个文件。
func (s *State) Save() error {
	if err := os.MkdirAll(config.Dir(), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := Path()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("写状态文件失败: %w", err)
	}
	return os.Rename(tmp, path)
}

// Get 取一个账号的进度，没有记录时返回零值。
func (s *State) Get(name string) Account { return s.Accounts[name] }

// Set 记下一个账号的进度。
func (s *State) Set(name string, a Account) {
	if s.Accounts == nil {
		s.Accounts = map[string]Account{}
	}
	s.Accounts[name] = a
}

// Resume 判断能不能接着上次的进度增量拉取。
// 没同步过、或者服务器的 UIDVALIDITY 变了，都得从头全量来一遍。
func (a Account) Resume(currentValidity uint32) (lastUID uint32, ok bool) {
	if a.LastUID == 0 {
		return 0, false
	}
	if a.UIDValidity != 0 && a.UIDValidity != currentValidity {
		return 0, false
	}
	return a.LastUID, true
}
