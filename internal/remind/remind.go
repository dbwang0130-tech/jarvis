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

// BriefHour 是每天什么时候说那一句。
//
// 八点：多数人已经起来、还没开始干活，这会儿知道「今天有什么」还来得及
// 调整；再早会把人吵醒，再晚就只是通报了。
const BriefHour = 8

// Brief 攒出每天早上那一句：今天几件事、哪些今天截止、有几件逾期。
//
// **为什么要有这一句**：Check 只认排了具体钟点的事，而截止和逾期本来就
// 没有钟点——「周五前交报告」不会响，「昨天就该做完的」也不会响，
// 而那两样恰恰是最容易忘的。
//
// 为什么是一句而不是一堆：给每条截止都排个通知，一天弹七八次，人会把
// 通知整个关掉，那就一条都收不到了。
//
// 每天只发一次：靠 reminded 里那把带日期的键挡住——launchd 每五分钟跑
// 一趟，不挡的话八点到九点之间会响十二次。
func Brief(coll *org.Collection, now time.Time, reminded map[string]int64) string {
	if now.Hour() != BriefHour {
		return ""
	}
	key := "brief:" + now.Format("2006-01-02")
	if _, done := reminded[key]; done {
		return ""
	}

	opts := agenda.DefaultOptions()
	opts.Now = now
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	ag := agenda.Build(coll, day0, 1, opts)
	if len(ag.Days) == 0 {
		return ""
	}

	// 逾期项就混在第一天里（Kind 是 KindOverdue），不是单独一摊。
	// 混着数的话，「今天 5 件事」里其实有 3 件是上周欠的——那是两回事。
	var todo, due, overdue int
	var first string
	for _, it := range ag.Days[0].Items {
		if it.Node.IsDone() {
			continue
		}
		if it.Kind == agenda.KindOverdue {
			overdue++
			continue
		}
		todo++
		if it.Kind == agenda.KindDeadline && it.DaysLeft <= 0 {
			due++
		}
		// 先办哪件：按 agenda 排好的顺序取第一条，排了钟点的带上钟点。
		if first == "" {
			if it.HasTime {
				first = it.Start.Format("15:04") + " " + org.Plain(it.Node.Title)
			} else {
				first = org.Plain(it.Node.Title)
			}
		}
	}

	var parts []string
	if todo > 0 {
		parts = append(parts, fmt.Sprintf("%d 件事", todo))
	}
	if due > 0 {
		parts = append(parts, fmt.Sprintf("%d 件今天截止", due))
	}
	if overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d 件逾期", overdue))
	}
	// 什么都没有就别响——「今天没事」也是一次打扰。
	if len(parts) == 0 {
		return ""
	}

	reminded[key] = now.Unix()
	text := "今天：" + strings.Join(parts, " · ")
	if first != "" {
		text += "。先办 " + first
	}
	return text
}
