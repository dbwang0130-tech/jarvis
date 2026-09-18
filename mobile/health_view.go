package mobile

import (
	"sync"
	"time"

	"github.com/dabao/jarvis/internal/health"
)

// HealthView 是健康页要显示的东西。
//
// 解析 FHIR 这件事留在 Go 这边：Swift 那头再实现一遍 LOINC 查表、
// 血压分项、单位换算，两边迟早对不上——而这份数据的全部意义就在于
// 它是标准的。
type HealthView struct {
	// Profile 是基础信息那一行，如「男，41 岁」；没填就是空串。
	Profile string `json:"profile"`
	// Vitals 是每一项最近一次的测量，按固定顺序。
	Vitals []HealthVital `json:"vitals"`
	// Meals 是最近几天吃了什么。
	Meals []HealthEntry `json:"meals"`
	// Total 是记录总条数。
	Total int `json:"total"`
}

// HealthVital 是一项体征最近的样子。
type HealthVital struct {
	Kind  string `json:"kind"`  // 体重 / 血压 …
	Value string `json:"value"` // 已经带好单位的一串，如「72.5kg」「120/80 mmHg」
	When  string `json:"when"`  // 2026-09-18 09:00
	Ago   string `json:"ago"`   // 今天 / 3 天前
	// Trend 是跟上一次比的变化，如「+0.8kg」；只有一次记录时是空串。
	Trend string `json:"trend"`
	// Unit 是坐标轴上那个单位。
	Unit string `json:"unit"`
	// Points 是按时间升序的历史，给趋势图用。少于两个点就画不出线，
	// 界面据此决定显不显示图。
	Points []HealthPoint `json:"points"`
}

// HealthPoint 是趋势图上的一个点。血压一个点有两个值。
type HealthPoint struct {
	// T 是时间戳（RFC3339）。给的是时刻不是日期——一天量两次体重，
	// 只给日期的话两个点会叠在一起。
	T  string  `json:"t"`
	V  float64 `json:"v"`
	V2 float64 `json:"v2,omitempty"`
}

// HealthEntry 是一条饮食记录。
type HealthEntry struct {
	When string `json:"when"`
	Ago  string `json:"ago"`
	Text string `json:"text"`
}

func (v *HealthView) normalize() {
	if v.Vitals == nil {
		v.Vitals = []HealthVital{}
	}
	// 往下钻一层：Swift 那边数组字段是非可选的，解到 null 整份快照作废。
	for i := range v.Vitals {
		if v.Vitals[i].Points == nil {
			v.Vitals[i].Points = []HealthPoint{}
		}
	}
	if v.Meals == nil {
		v.Meals = []HealthEntry{}
	}
}

// healthCache 缓存上一次解析出来的记录。
//
// 跟 org 那边的 collectCached 同一个理由：下拉刷新、切回前台、每轮对话
// 之后的重解析——这些时刻健康记录几乎总是**逐字节相同**，重解一遍纯属
// 白做。步数一天一条，文件只会越长，白做的代价也跟着涨。
//
// 不用哈希：org 那边入参是一张 map 才需要摊平成一个键，这里只有一个
// 字符串，直接比就是了（先比长度再 memcmp，兆字节级也是微秒）。
var (
	healthMu    sync.Mutex
	healthRaw   string
	healthStore *health.Store
)

// healthCached 解析一份健康记录，内容没变就复用上次那份。
//
// 缓存的是**解析结果**而不是 HealthView：`now` 会变，「3 天前」这类
// 说法每次都得重算。
func healthCached(raw string) *health.Store {
	healthMu.Lock()
	defer healthMu.Unlock()
	if healthStore != nil && healthRaw == raw {
		return healthStore
	}
	st, err := health.LoadString(raw)
	if err != nil {
		return nil
	}
	healthRaw, healthStore = raw, st
	return st
}

// buildHealth 把健康记录整理成界面要的样子。
func buildHealth(raw string, now time.Time) *HealthView {
	st := healthCached(raw)
	if st == nil {
		return nil
	}
	v := &HealthView{Total: len(st.Resources)}
	v.Profile = st.ProfileLine(now)
	for _, s := range st.Summary(now) {
		pts := make([]HealthPoint, 0, len(s.Points))
		for _, p := range s.Points {
			pts = append(pts, HealthPoint{
				T: p.At.Format(time.RFC3339), V: p.V, V2: p.V2})
		}
		v.Vitals = append(v.Vitals, HealthVital{
			Kind: s.Kind, Value: s.Value, When: s.When.Format("2006-01-02 15:04"),
			Ago: s.Ago, Trend: s.Trend, Unit: s.Unit, Points: pts,
		})
	}
	for _, m := range st.RecentMeals(now, 7, 30) {
		v.Meals = append(v.Meals, HealthEntry{
			When: m.When.Format("01-02 15:04"), Ago: m.Ago, Text: m.Text,
		})
	}
	v.normalize()
	return v
}
