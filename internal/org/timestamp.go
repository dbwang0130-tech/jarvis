package org

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Unit 是时间戳中重复器/提前量使用的时间单位。
type Unit byte

const (
	UnitHour  Unit = 'h'
	UnitDay   Unit = 'd'
	UnitWeek  Unit = 'w'
	UnitMonth Unit = 'm'
	UnitYear  Unit = 'y'
)

// RepeatKind 区分 org 的三种重复语义。
type RepeatKind int

const (
	// RepeatCumulate 对应 "+1w"：在原时间上累加，可能仍落在过去。
	RepeatCumulate RepeatKind = iota
	// RepeatCatchUp 对应 "++1w"：累加直到超过当前时间。
	RepeatCatchUp
	// RepeatRestart 对应 ".+1w"：从完成当天重新起算。
	RepeatRestart
)

// Delay 是一个带单位的时间偏移，用于重复器和 deadline 提前量。
type Delay struct {
	N    int
	Unit Unit
}

func (d Delay) String() string { return fmt.Sprintf("%d%c", d.N, d.Unit) }

// Add 把偏移量加到 t 上。
func (d Delay) Add(t time.Time) time.Time {
	switch d.Unit {
	case UnitHour:
		return t.Add(time.Duration(d.N) * time.Hour)
	case UnitDay:
		return t.AddDate(0, 0, d.N)
	case UnitWeek:
		return t.AddDate(0, 0, 7*d.N)
	case UnitMonth:
		return t.AddDate(0, d.N, 0)
	case UnitYear:
		return t.AddDate(d.N, 0, 0)
	}
	return t
}

// Repeater 描述 "+1w" / "++1w" / ".+1w" 形式的重复规则。
type Repeater struct {
	Kind  RepeatKind
	Delay Delay
}

func (r Repeater) String() string {
	switch r.Kind {
	case RepeatCatchUp:
		return "++" + r.Delay.String()
	case RepeatRestart:
		return ".+" + r.Delay.String()
	default:
		return "+" + r.Delay.String()
	}
}

// Next 按重复语义计算 base 之后的下一次发生时间。now 用于 ++ 和 .+ 语义。
func (r Repeater) Next(base, now time.Time) time.Time {
	switch r.Kind {
	case RepeatRestart:
		// 从 now 当天（保留原时刻）重新起算。
		anchor := time.Date(now.Year(), now.Month(), now.Day(),
			base.Hour(), base.Minute(), 0, 0, base.Location())
		return r.Delay.Add(anchor)
	case RepeatCatchUp:
		next := r.Delay.Add(base)
		for !next.After(now) {
			step := r.Delay.Add(next)
			if !step.After(next) { // 防御零偏移导致死循环
				break
			}
			next = step
		}
		return next
	default:
		return r.Delay.Add(base)
	}
}

// Timestamp 是一个 org 时间戳，尽量保留原始文本以便无损写回。
type Timestamp struct {
	Start   time.Time
	End     time.Time // 零值表示没有结束时间
	HasTime bool      // 是否带具体时刻（否则是整天）
	Active  bool      // <...> 为 true，[...] 为 false
	Repeat  *Repeater
	Warn    *Delay // deadline 的提前提醒量，如 "-2d"
	Raw     string // 原始文本，写回时优先使用
}

// HasEnd 报告时间戳是否带结束时间。
func (t *Timestamp) HasEnd() bool { return !t.End.IsZero() }

// String 按 org 语法渲染时间戳。
func (t *Timestamp) String() string {
	open, close := "<", ">"
	if !t.Active {
		open, close = "[", "]"
	}
	var b strings.Builder
	b.WriteString(open)
	b.WriteString(t.Start.Format("2006-01-02 Mon"))
	if t.HasTime {
		b.WriteString(t.Start.Format(" 15:04"))
		if t.HasEnd() && sameDay(t.Start, t.End) {
			b.WriteString("-" + t.End.Format("15:04"))
		}
	}
	if t.Repeat != nil {
		b.WriteString(" " + t.Repeat.String())
	}
	if t.Warn != nil {
		b.WriteString(" -" + t.Warn.String())
	}
	b.WriteString(close)
	// 跨日区间用 <a>--<b> 表示。
	if t.HasEnd() && !sameDay(t.Start, t.End) {
		end := &Timestamp{Start: t.End, HasTime: t.HasTime, Active: t.Active}
		b.WriteString("--" + end.String())
	}
	return b.String()
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// 主体：<2026-09-14 Mon 10:00-12:00 +1w -2d>
var tsRe = regexp.MustCompile(`([<\[])(\d{4})-(\d{2})-(\d{2})` + // 1 括号 2-4 日期
	`(?:\s+[^\s\]>\d+\-.]+)?` + // 星期几，忽略（可能被本地化）
	`(?:\s+(\d{1,2}):(\d{2})(?:-(\d{1,2}):(\d{2}))?)?` + // 5-8 时刻与结束时刻
	`((?:\s+[.+]?[+-]\d+[hdwmy])*)` + // 9 重复器与提前量
	`\s*([>\]])`) // 10 闭合括号

var repeatRe = regexp.MustCompile(`([.+]?[+-])(\d+)([hdwmy])`)

// ParseTimestamp 解析 s 中的第一个时间戳，并自动合并 "a--b" 形式的区间。
func ParseTimestamp(s string) *Timestamp {
	ts, _ := parseTimestampAt(s, 0)
	return ts
}

// FindTimestamps 返回 s 中出现的全部时间戳，按出现顺序排列。
func FindTimestamps(s string) []*Timestamp {
	var out []*Timestamp
	for pos := 0; pos < len(s); {
		ts, end := parseTimestampAt(s, pos)
		if ts == nil {
			break
		}
		out = append(out, ts)
		pos = end
	}
	return out
}

// parseTimestampAt 从 from 处开始查找一个时间戳，返回它和其在 s 中的结束偏移。
func parseTimestampAt(s string, from int) (*Timestamp, int) {
	if from >= len(s) {
		return nil, len(s)
	}
	loc := tsRe.FindStringSubmatchIndex(s[from:])
	if loc == nil {
		return nil, len(s)
	}
	ts := buildTimestamp(s[from:], loc)
	end := from + loc[1]

	// 区间语法 <a>--<b>：把后一个时间戳作为结束时间。
	if strings.HasPrefix(s[end:], "--") {
		if second, secEnd := parseTimestampAt(s, end+2); second != nil && secEnd > end {
			ts.End = second.Start
			if second.HasTime {
				ts.HasTime = true
			}
			ts.Raw = s[from+loc[0] : secEnd]
			return ts, secEnd
		}
	}
	return ts, end
}

func buildTimestamp(s string, loc []int) *Timestamp {
	group := func(i int) string {
		if loc[2*i] < 0 {
			return ""
		}
		return s[loc[2*i]:loc[2*i+1]]
	}
	atoi := func(v string) int { n, _ := strconv.Atoi(v); return n }

	year, month, day := atoi(group(2)), atoi(group(3)), atoi(group(4))
	ts := &Timestamp{
		Active: group(1) == "<",
		Raw:    s[loc[0]:loc[1]],
	}

	hour, min := 0, 0
	if group(5) != "" {
		ts.HasTime = true
		hour, min = atoi(group(5)), atoi(group(6))
	}
	ts.Start = time.Date(year, time.Month(month), day, hour, min, 0, 0, time.Local)
	if group(7) != "" {
		ts.End = time.Date(year, time.Month(month), day, atoi(group(7)), atoi(group(8)), 0, 0, time.Local)
	}

	for _, m := range repeatRe.FindAllStringSubmatch(group(9), -1) {
		d := Delay{N: atoi(m[2]), Unit: Unit(m[3][0])}
		switch m[1] {
		case "-":
			ts.Warn = &d
		case "+":
			ts.Repeat = &Repeater{Kind: RepeatCumulate, Delay: d}
		case "++":
			ts.Repeat = &Repeater{Kind: RepeatCatchUp, Delay: d}
		case ".+":
			ts.Repeat = &Repeater{Kind: RepeatRestart, Delay: d}
		}
	}
	return ts
}

var weekdayCN = [...]string{"日", "一", "二", "三", "四", "五", "六"}

// FormatDate 返回 "2026-09-14 周一" 形式的日期，供界面与模型提示使用。
func FormatDate(t time.Time) string {
	return fmt.Sprintf("%s 周%s", t.Format("2006-01-02"), weekdayCN[int(t.Weekday())])
}

// 相对日期词，用于解析人或模型给出的时间。
var relativeDays = map[string]int{
	"今天": 0, "今日": 0, "明天": 1, "明日": 1, "后天": 2, "大后天": 3,
}

// singleRepeatRe 匹配单独成词的重复器或提前量，如 "+1w"、"++2d"、".+1m"、"-3d"。
var singleRepeatRe = regexp.MustCompile(`^([.+]?[+-])(\d+)([hdwmy])$`)

// ParseUserTime 解析人类或模型给出的时间，支持以下形式：
//
//	2006-01-02
//	2006-01-02 15:04
//	2006-01-02 15:04-16:30
//	明天 09:00
//	2006-01-02 +1w          每周重复
//	2006-01-02 09:00 .+2d   完成后隔两天再来
//	2006-01-02 -3d          截止前三天开始提醒
//	<2006-01-02 Mon 15:04>
//
// base 用于换算相对日期。
func ParseUserTime(s string, base time.Time) (*Timestamp, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("时间为空")
	}
	if strings.HasPrefix(s, "<") || strings.HasPrefix(s, "[") {
		if ts := ParseTimestamp(s); ts != nil {
			return ts, nil
		}
	}

	// 先把重复器和提前量摘出来，剩下的才是日期与时刻。
	var repeat *Repeater
	var warn *Delay
	var rest []string
	for _, f := range strings.Fields(s) {
		m := singleRepeatRe.FindStringSubmatch(f)
		if m == nil {
			rest = append(rest, f)
			continue
		}
		n, _ := strconv.Atoi(m[2])
		d := Delay{N: n, Unit: Unit(m[3][0])}
		switch m[1] {
		case "-":
			warn = &d
		case "+":
			repeat = &Repeater{Kind: RepeatCumulate, Delay: d}
		case "++":
			repeat = &Repeater{Kind: RepeatCatchUp, Delay: d}
		case ".+":
			repeat = &Repeater{Kind: RepeatRestart, Delay: d}
		}
	}
	s = strings.Join(rest, " ")
	if s == "" {
		return nil, fmt.Errorf("只给了重复规则，还需要一个日期")
	}

	var day time.Time
	for word, offset := range relativeDays {
		if strings.HasPrefix(s, word) {
			day = base.AddDate(0, 0, offset)
			s = strings.TrimSpace(strings.TrimPrefix(s, word))
			break
		}
	}
	if day.IsZero() {
		if len(s) < 10 {
			return nil, fmt.Errorf("时间 %q 缺少日期，请写成 2006-01-02 [15:04[-15:04]] [+1w]", s)
		}
		d, err := time.ParseInLocation("2006-01-02", s[:10], time.Local)
		if err != nil {
			return nil, fmt.Errorf("无法解析日期 %q，请使用 2006-01-02 格式", s)
		}
		day, s = d, strings.TrimSpace(s[10:])
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)

	ts := &Timestamp{Start: day, Active: true, Repeat: repeat, Warn: warn}
	if s == "" {
		return ts, nil
	}
	parts := strings.SplitN(s, "-", 2)
	start, err := parseClockTime(day, parts[0])
	if err != nil {
		return nil, err
	}
	ts.Start, ts.HasTime = start, true
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		end, err := parseClockTime(day, parts[1])
		if err != nil {
			return nil, err
		}
		ts.End = end
	}
	return ts, nil
}

// ParseRepeat 解析单独给出的重复规则，如 "+1w"、"每周"。空字符串表示清除。
func ParseRepeat(s string) (*Repeater, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if r, ok := repeatAliases[s]; ok {
		s = r
	}
	m := singleRepeatRe.FindStringSubmatch(s)
	if m == nil || m[1] == "-" {
		return nil, fmt.Errorf("看不懂重复规则 %q，可以写 +1w（每周）、+1d（每天）、+1m（每月），"+
			"或 .+3d（完成后隔三天）", s)
	}
	n, _ := strconv.Atoi(m[2])
	d := Delay{N: n, Unit: Unit(m[3][0])}
	switch m[1] {
	case "++":
		return &Repeater{Kind: RepeatCatchUp, Delay: d}, nil
	case ".+":
		return &Repeater{Kind: RepeatRestart, Delay: d}, nil
	default:
		return &Repeater{Kind: RepeatCumulate, Delay: d}, nil
	}
}

// repeatAliases 让常见的中文说法也能直接用。
var repeatAliases = map[string]string{
	"每天": "+1d", "每日": "+1d", "天": "+1d",
	"每周": "+1w", "每星期": "+1w", "周": "+1w",
	"每两周": "+2w", "每月": "+1m", "月": "+1m",
	"每年": "+1y", "年": "+1y",
	"工作日": "+1d",
}

// Describe 用中文说明重复规则，便于界面展示。
func (r Repeater) Describe() string {
	unit := map[Unit]string{
		UnitHour: "小时", UnitDay: "天", UnitWeek: "周",
		UnitMonth: "个月", UnitYear: "年",
	}[r.Delay.Unit]
	switch r.Kind {
	case RepeatRestart:
		return fmt.Sprintf("完成后隔 %d%s", r.Delay.N, unit)
	case RepeatCatchUp:
		return fmt.Sprintf("每 %d%s（跳过错过的）", r.Delay.N, unit)
	default:
		if r.Delay.N == 1 {
			return "每" + unit
		}
		return fmt.Sprintf("每 %d%s", r.Delay.N, unit)
	}
}

// parseClockTime 解析 "15:04" 或 "15" 形式的时刻，落到 day 这一天。
func parseClockTime(day time.Time, s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.ParseInLocation("15:04", s, time.Local)
	if err != nil {
		if t, err = time.ParseInLocation("15", s, time.Local); err != nil {
			return time.Time{}, fmt.Errorf("无法解析时刻 %q，请使用 15:04 格式", s)
		}
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, time.Local), nil
}
