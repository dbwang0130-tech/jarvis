// Package session 负责把对话会话持久化到磁盘，支持继续上次会话。
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// Store 管理一个会话目录。会话用时间戳做 id，天然可排序，最新即最后。
type Store struct {
	Dir string
}

// New 构造会话存储，必要时创建目录。
func New(dir string) *Store {
	_ = os.MkdirAll(dir, 0o700)
	return &Store{Dir: dir}
}

// NewID 生成一个新会话 id。
func (s *Store) NewID() string {
	return time.Now().Format("20060102-150405.000000000")
}

// Path 返回会话文件路径。
func (s *Store) Path(id string) string {
	return filepath.Join(s.Dir, id+".json")
}

// Save 保存会话历史。
func (s *Store) Save(id string, history []agent.Message) error {
	raw, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path(id), raw, 0o600)
}

// Load 读取会话历史；文件不存在时返回空历史，不算错误。
func (s *Store) Load(id string) ([]agent.Message, error) {
	raw, err := os.ReadFile(s.Path(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h []agent.Message
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return h, nil
}

// LastID 返回最近一次会话的 id；没有任何会话时返回 false。
func (s *Store) LastID() (string, bool) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return "", false
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	if len(ids) == 0 {
		return "", false
	}
	sort.Strings(ids)
	return ids[len(ids)-1], true
}

// Delete 删除一个会话。
// Delete 删掉一个会话。本来就不在的话当作已经删过，不算错——
// 删除做成幂等的，调用方不必先去查一遍。
func (s *Store) Delete(id string) error {
	if err := os.Remove(s.Path(id)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
