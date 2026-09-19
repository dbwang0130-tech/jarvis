package remind

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Slack 是「到点」的容差。
//
// launchd 每五分钟叫一次，设的 08:30 实际会在 08:30–08:35 之间被撞上。
// 不给容差的话，除非正好卡在整五分，否则一次都不会触发。
const Slack = 5 * time.Minute

// MailDue 报告现在是不是某个设定的收信时间点，以及是哪一个。
//
// 每个点每天只算一次：靠 reminded 里那把带日期的键挡住——五分钟跑一趟，
// 容差窗口里会撞上一到两次，不挡的话一个点能收两遍。
//
// 认不出来的时间写法直接跳过，不报错：配置是人手写的，写错一个
// 不该让整件事停摆，而这里没有地方把错误说给人听（launchd 的 stderr
// 进了日志文件，没人看）。
func MailDue(times []string, now time.Time, reminded map[string]int64) (string, bool) {
	for _, raw := range times {
		hh, mm, ok := parseHM(raw)
		if !ok {
			continue
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if now.Before(at) || now.Sub(at) >= Slack {
			continue
		}
		key := fmt.Sprintf("mailcheck:%s:%02d:%02d", now.Format("2006-01-02"), hh, mm)
		if _, done := reminded[key]; done {
			continue
		}
		reminded[key] = now.Unix()
		return fmt.Sprintf("%02d:%02d", hh, mm), true
	}
	return "", false
}

// parseHM 认 "8:30"、"08:30" 这两种写法。
func parseHM(s string) (hour, minute int, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}
