package remind

import (
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 9, 19, h, m, 0, 0, time.Local)
}

// launchd 每五分钟叫一次，设的 08:30 实际会在 08:30–08:35 之间被撞上。
// 不给容差的话，除非正好卡在整五分，否则一次都不会触发。
func TestMailDueWithinSlack(t *testing.T) {
	times := []string{"08:30", "12:00", "18:00"}
	for _, tc := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{"正点", at(8, 30), true},
		{"晚三分钟", at(8, 33), true},
		{"晚五分钟就过了", at(8, 35), false},
		{"早一分钟还没到", at(8, 29), false},
		{"不相干的时候", at(15, 0), false},
		{"另一个点", at(12, 2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, got := MailDue(times, tc.now, map[string]int64{})
			if got != tc.want {
				t.Errorf("%s 时该是 %v", tc.now.Format("15:04"), tc.want)
			}
		})
	}
}

// 一个点一天只收一次。五分钟跑一趟，容差窗口里会撞上一到两次，
// 不挡的话一个点能收两遍。
func TestMailDueOncePerSlot(t *testing.T) {
	reminded := map[string]int64{}
	times := []string{"08:30"}

	if _, ok := MailDue(times, at(8, 30), reminded); !ok {
		t.Fatal("头一次就该收")
	}
	if _, ok := MailDue(times, at(8, 33), reminded); ok {
		t.Error("同一个点收了第二次")
	}
	// 另一个点不受影响。
	if _, ok := MailDue([]string{"08:30", "12:00"}, at(12, 1), reminded); !ok {
		t.Error("12 点那个点被 8 点那次挡住了")
	}
}

// 配置是人手写的，写错一个不该让整件事停摆——尤其这里没有地方把错误
// 说给人听（launchd 的 stderr 进了日志文件，没人看）。
func TestMailDueIgnoresJunk(t *testing.T) {
	times := []string{"八点半", "25:00", "8:70", "", "08:30"}
	slot, ok := MailDue(times, at(8, 31), map[string]int64{})
	if !ok {
		t.Fatal("好的那个也被坏的拖累了")
	}
	if slot != "08:30" {
		t.Errorf("认出来的是 %q", slot)
	}
}

// 一个都没设就什么都不做。
func TestMailDueEmpty(t *testing.T) {
	if _, ok := MailDue(nil, at(8, 30), map[string]int64{}); ok {
		t.Error("没设时间却说到点了")
	}
}
