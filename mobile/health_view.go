package mobile

import (
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
	if v.Meals == nil {
		v.Meals = []HealthEntry{}
	}
}

// buildHealth 把健康记录整理成界面要的样子。
func buildHealth(raw string, now time.Time) *HealthView {
	st, err := health.LoadString(raw)
	if err != nil || st == nil {
		return nil
	}
	v := &HealthView{Total: len(st.Resources)}
	v.Profile = st.ProfileLine(now)
	for _, s := range st.Summary(now) {
		v.Vitals = append(v.Vitals, HealthVital{
			Kind: s.Kind, Value: s.Value, When: s.When.Format("2006-01-02 15:04"),
			Ago: s.Ago, Trend: s.Trend,
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
