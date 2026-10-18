package health

import (
	"fmt"
	"sort"
	"time"
)

// 这个文件把健康记录整理成「能直接显示的一行一行」。
//
// 放在这儿是因为它全是**FHIR 的知识**：
// 哪个 LOINC 码是体重、血压的两个数在分项里、单位怎么跟着数走。
// 重复实现迟早会对不上，而这份数据的全部意义就在于
// 它是标准的。

// VitalSummary 是一项体征最近的样子，【当下】里那张表一行一项。
type VitalSummary struct {
	Kind  string
	Value string
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
		sum := VitalSummary{Kind: name, Value: last.valueText(), Ago: agoCN(last.when(), now)}
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
		// 标了「录错了」的不算数。
		//
		// FHIR 里记错的记录不删，是把 status 改成 entered-in-error——
		// 医疗记录跟账本一样不改历史，那条仍留在文件里，但读的人要当它
		// 不存在。不滤掉的话，一条错的体重会一直参与趋势和「最近一次」，
		// 而且是从别的工具（比如「健康」App 导出）带进来的也照单全收。
		if s.Resources[i].voided() {
			continue
		}
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
