package health

import (
	"bufio"
	"encoding/json"
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

// LoadString 从一份 NDJSON 全文里读记录。手机那边文件内容是过桥传进来的，
// 不落盘，所以不能走 Load。
func LoadString(raw string) (*Store, error) {
	s := &Store{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r Resource
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.ResourceType == "" {
			continue
		}
		s.Resources = append(s.Resources, r)
	}
	return s, sc.Err()
}

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
}

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
			Kind: name, Value: strings.TrimPrefix(last.label(), name+" "),
			When: last.when(), Ago: agoCN(last.when(), now),
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
	var out []Resource
	for _, r := range s.Resources {
		if r.Code != nil && len(r.Code.Coding) > 0 && r.Code.Coding[0].Code == v.Code {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].when().Before(out[j].when()) })
	return out
}

// trendOf 说这一次比上一次差多少。
//
// 血压有两个数，说「+8/-2」而不是只说一个——只报收缩压的话，
// 舒张压的变化就被藏起来了。
func trendOf(prev, now Resource) string {
	if len(prev.Component) == 2 && len(now.Component) == 2 {
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
