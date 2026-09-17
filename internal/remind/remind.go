// Package remind 提供日程到点提醒：前台 TUI 与后台 CLI 共用同一套检查逻辑。
package remind

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/org"

	"github.com/dabao/jarvis/internal/config"
)

// Lead 是提前提醒的时间窗口（到点前 5 分钟）。
const Lead = 5 * time.Minute

// Check 返回当前该提醒的事项文本，并把 key 记进 reminded（就地修改）。
// reminded 的值是首次提醒的时间戳，用来过期清理。
func Check(coll *org.Collection, now time.Time, reminded map[string]int64) []string {
	opts := agenda.DefaultOptions()
	opts.Now = now
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	ag := agenda.Build(coll, day0, 1, opts)

	var out []string
	for _, day := range ag.Days {
		for _, it := range day.Items {
			if it.Kind != agenda.KindScheduled || !it.HasTime {
				continue
			}
			if it.Start.Before(now) || it.Start.Sub(now) > Lead {
				continue
			}
			// key 必须带日期。只记时分的话，每周组会这种循环事件
			// 下一周的 key 跟这周一模一样，提醒过一次就再也不响了。
			key := fmt.Sprintf("%s:%d:%s", it.Doc.Path, it.Node.Line,
				it.Start.Format("2006-01-02 15:04"))
			if _, done := reminded[key]; done {
				continue
			}
			reminded[key] = now.Unix()
			out = append(out, fmt.Sprintf("⏰ %s — %s", it.Start.Format("15:04"), it.Node.Title))
		}
	}
	return out
}

// Notify 弹一条 macOS 系统通知。
//
// 这里必须等 osascript 跑完再返回：launchd 在任务退出时会把整个进程组
// 一起收掉，用 Start() 撒手不管的话，通知往往还没发出去就被杀了。
func Notify(text string) error {
	return exec.Command("osascript", "-e",
		fmt.Sprintf("display notification %s with title %s", aq(text), aq("Jarvis 提醒"))).Run()
}

func aq(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// Store 持久化「已提醒」记录，跨进程去重，避免 launchd 每次跑都重复弹。
type Store struct {
	Path string
}

// Load 读取已提醒记录；超过 24 小时的老记录会被清掉。
func (s *Store) Load() map[string]int64 {
	out := map[string]int64{}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return out
	}
	var m map[string]int64
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}
	cutoff := time.Now().Add(-24 * time.Hour).Unix()
	for k, v := range m {
		if v >= cutoff {
			out[k] = v
		}
	}
	return out
}

// Save 保存已提醒记录。时间戳是首次提醒的时刻，原样存回去——
// 要是每次都刷成当下，24 小时的过期清理就永远不会生效，
// 记录只进不出。
func (s *Store) Save(reminded map[string]int64) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(reminded, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteAtomic(s.Path, raw, 0o600)
}
