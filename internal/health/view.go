package health

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 这个文件把健康记录整理成「能直接显示的一行一行」。
//
// 放在这儿而不是 mobile 或 Swift 里，是因为它全是**FHIR 的知识**：
// 哪个 LOINC 码是体重、血压的两个数在分项里、单位怎么跟着数走。
// 手机那头再实现一遍，两边迟早对不上——而这份数据的全部意义就在于
// 它是标准的。

// ProfileLine 是基础信息那一行，没填就是空串。
func (s *Store) ProfileLine(now time.Time) string {
	p, ok := s.Patient()
	if !ok {
		return ""
	}
	parts := []string{}
	if g := genderCN(p.Gender); g != "未填" {
		parts = append(parts, g)
	}
	if age, ok := ageOf(p.BirthDate, now); ok {
		parts = append(parts, fmt.Sprintf("%d 岁", age))
	}
	return strings.Join(parts, "，")
}

// VitalSummary 是一项体征最近的样子。
type VitalSummary struct {
	Kind  string
	Value string
	When  time.Time
	Ago   string
	// Trend 是跟上一次比的变化，如「+0.8kg」。只有一次记录时是空串——
	// 拿第一次当基线画个 0 出来，看着像「没变化」，其实是没得比。
	Trend string
	// Unit 是画图时坐标轴上那个单位。
	Unit string
	// Points 是按时间升序的历史，给趋势图用。
	Points []Point
}

// Point 是趋势图上的一个点。血压一个点有两个值。
type Point struct {
	At time.Time
	V  float64
	V2 float64
}

// seriesDays 是趋势图往回看几天。
//
// 半年。再长在手机那块屏上就挤成一条毛线了，看不出趋势；再短又盖不住
// 「今年比去年重了几斤」这种真正想看的事。
const seriesDays = 180

// seriesMax 是一项最多给几个点。
//
// 每天量一次，半年也就 180 个点。有人一天量好几次（体重秤连着 app），
// 那就会多出来——超了只留最近的，图的右半边才是要看的。
const seriesMax = 400

// Summary 给出每一项最近一次的测量，按 vitalNames 的顺序。
func (s *Store) Summary(now time.Time) []VitalSummary {
	var out []VitalSummary
	for _, name := range vitalNames {
		hist := s.history(name)
		if len(hist) == 0 {
			continue
		}
		last := hist[len(hist)-1]
		sum := VitalSummary{
			Kind: name, Value: last.valueText(),
			When: last.when(), Ago: agoCN(last.when(), now),
			Unit: vitals[name].Unit, Points: pointsOf(hist, now),
		}
		if len(hist) > 1 {
			sum.Trend = trendOf(hist[len(hist)-2], last)
		}
		out = append(out, sum)
	}
	return out
}

// history 返回某一项按时间升序的全部记录。
func (s *Store) history(name string) []Resource {
	v, ok := vitals[name]
	if !ok {
		return nil
	}
	// 走索引，不扫全表。快照要问十三项，全表扫十三遍的话，
	// 步数一天一条的文件过两年就有感觉了。
	idx := s.codes()[v.Code]
	if len(idx) == 0 {
		return nil
	}
	out := make([]Resource, 0, len(idx))
	sorted := true
	for _, i := range idx {
		if len(out) > 0 && s.Resources[i].when().Before(out[len(out)-1].when()) {
			sorted = false
		}
		out = append(out, s.Resources[i])
	}
	// 文件是只追加的，绝大多数时候本来就有序——导入补录旧数据才会乱。
	if !sorted {
		sort.SliceStable(out, func(i, j int) bool { return out[i].when().Before(out[j].when()) })
	}
	return out
}

// pointsOf 把历史削成能画的一条线。
func pointsOf(hist []Resource, now time.Time) []Point {
	cut := now.AddDate(0, 0, -seriesDays)
	out := make([]Point, 0, len(hist))
	for _, r := range hist {
		t := r.when()
		if t.Before(cut) {
			continue
		}
		switch {
		case r.isPanel():
			out = append(out, Point{At: t,
				V:  r.Component[0].ValueQuantity.Value,
				V2: r.Component[1].ValueQuantity.Value})
		case r.ValueQuantity != nil:
			out = append(out, Point{At: t, V: r.ValueQuantity.Value})
		}
	}
	if len(out) > seriesMax {
		out = out[len(out)-seriesMax:]
	}
	return out
}

// trendOf 说这一次比上一次差多少。
//
// 血压有两个数，说「+8/-2」而不是只说一个——只报收缩压的话，
// 舒张压的变化就被藏起来了。
func trendOf(prev, now Resource) string {
	if prev.isPanel() && now.isPanel() {
		d1 := now.Component[0].ValueQuantity.Value - prev.Component[0].ValueQuantity.Value
		d2 := now.Component[1].ValueQuantity.Value - prev.Component[1].ValueQuantity.Value
		if d1 == 0 && d2 == 0 {
			return "持平"
		}
		return fmt.Sprintf("%+.0f/%+.0f", d1, d2)
	}
	if prev.ValueQuantity == nil || now.ValueQuantity == nil {
		return ""
	}
	d := now.ValueQuantity.Value - prev.ValueQuantity.Value
	if d == 0 {
		return "持平"
	}
	sign := "+"
	if d < 0 {
		sign = "-"
		d = -d
	}
	return sign + trimNum(d) + now.ValueQuantity.Unit
}

// MealEntry 是一条饮食记录。
type MealEntry struct {
	When time.Time
	Ago  string
	Text string
}

// RecentMeals 给出最近几天吃了什么，新的在前。
func (s *Store) RecentMeals(now time.Time, days, limit int) []MealEntry {
	var out []MealEntry
	for _, r := range s.Recent("饮食", now.AddDate(0, 0, -days), limit) {
		out = append(out, MealEntry{When: r.when(), Ago: agoCN(r.when(), now), Text: r.label()})
	}
	return out
}
