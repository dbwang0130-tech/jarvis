package agenda

import (
	"fmt"
	"sort"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

// DefaultDuration 是没有结束时间的日程按多长时间占用来估算。
const DefaultDuration = 30 * time.Minute

// Slot 是一天中的一段时间区间。
type Slot struct {
	Start, End time.Time
	// Title 在忙碌区间中记录占用它的条目标题。
	Title string
}

// Duration 返回区间时长。
func (s Slot) Duration() time.Duration { return s.End.Sub(s.Start) }

// WorkHours 描述一天中可安排事务的时段。
type WorkHours struct {
	StartHour, EndHour int
}

func (w WorkHours) bounds(date time.Time) (time.Time, time.Time) {
	d := dayOf(date)
	return d.Add(time.Duration(w.StartHour) * time.Hour),
		d.Add(time.Duration(w.EndHour) * time.Hour)
}

// Busy 返回某天已被占用的时间区间，按开始时间排序并合并重叠部分。
// 只有带具体时刻的条目才会占用时间。
func Busy(c *org.Collection, date time.Time, opts Options) []Slot {
	if opts.DeadlineWarnDays == 0 {
		opts.DeadlineWarnDays = 14
	}
	opts.ShowDone = false
	opts.HidePastOverdue = true
	ag := Build(c, date, 1, opts)

	var slots []Slot
	for _, it := range ag.Days[0].Items {
		if !it.HasTime || it.Kind == KindDeadline {
			continue // 截止只是提醒，不占用时间
		}
		end := it.Start.Add(effortOf(it.Node))
		if it.Time != nil && it.Time.HasEnd() && it.Time.End.After(it.Start) {
			end = it.Time.End
		}
		slots = append(slots, Slot{Start: it.Start, End: end, Title: it.Node.Title})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Start.Before(slots[j].Start) })

	// 合并重叠区间。
	var merged []Slot
	for _, s := range slots {
		if n := len(merged); n > 0 && !s.Start.After(merged[n-1].End) {
			if s.End.After(merged[n-1].End) {
				merged[n-1].End = s.End
				merged[n-1].Title += " / " + s.Title
			}
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

// effortOf 按 :EFFORT: 属性估算条目占用时长，缺省为 DefaultDuration。
func effortOf(n *org.Node) time.Duration {
	if d, ok := parseEffort(n.Props["EFFORT"]); ok {
		return d
	}
	return DefaultDuration
}

// parseEffort 解析 "2:00" 或 "90" 形式的工作量估计。
func parseEffort(s string) (time.Duration, bool) {
	if s == "" {
		return 0, false
	}
	var h, m int
	if n, _ := fmt.Sscanf(s, "%d:%d", &h, &m); n == 2 {
		return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, true
	}
	if n, _ := fmt.Sscanf(s, "%d", &m); n == 1 {
		return time.Duration(m) * time.Minute, true
	}
	return 0, false
}

// Free 返回某天在可安排时段内的空闲区间，短于 min 的区间会被忽略。
func Free(c *org.Collection, date time.Time, hours WorkHours, min time.Duration, opts Options) []Slot {
	dayStart, dayEnd := hours.bounds(date)
	// 今天的空档从当前时刻算起。
	if now := opts.now(); sameDay(now, date) && now.After(dayStart) {
		dayStart = now.Truncate(15 * time.Minute)
		if dayStart.Before(now) {
			dayStart = dayStart.Add(15 * time.Minute)
		}
	}
	if !dayStart.Before(dayEnd) {
		return nil
	}

	var free []Slot
	cur := dayStart
	for _, b := range Busy(c, date, opts) {
		if !b.End.After(cur) {
			continue
		}
		if b.Start.After(dayEnd) {
			break
		}
		if b.Start.After(cur) && b.Start.Sub(cur) >= min {
			free = append(free, Slot{Start: cur, End: minTime(b.Start, dayEnd)})
		}
		if b.End.After(cur) {
			cur = b.End
		}
	}
	if cur.Before(dayEnd) && dayEnd.Sub(cur) >= min {
		free = append(free, Slot{Start: cur, End: dayEnd})
	}
	return free
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
