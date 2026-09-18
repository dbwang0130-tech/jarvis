package health

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SourceHealthKit 是 Apple 健康那边 UUID 的命名空间。
//
// 每条导进来的记录都带上原系统的 UUID，导第二次时靠它认出「这条已经有了」。
// 不记的话每导一次就多一份，趋势图上一个点变成三个点，而且删不掉——
// 文件是只追加的。
const SourceHealthKit = "urn:apple:healthkit"

// Sample 是从别的系统拿来的一条原始读数。
//
// 故意做得很笨：种类是中文名，值是数，单位不带。**换算和 LOINC 查表都在
// 这边做**——手机那头只负责把 HealthKit 的样本翻成「体重 / 72.5 / 某时刻」，
// 再多一步它就得知道 LOINC 是什么了，而那份知识只该有一份。
type Sample struct {
	Kind   string    `json:"kind"`
	Value  float64   `json:"value"`
	Value2 float64   `json:"value2"` // 血压的舒张压，别的种类不用
	At     time.Time `json:"at"`
	UID    string    `json:"uid"`    // 原系统的 UUID
	Source string    `json:"source"` // 「Apple Watch」「体脂秤」，记进附注
}

// ImportResult 说这一趟导进来了什么。
type ImportResult struct {
	Added   int
	Skipped int // 已经有了
	Bad     int // 种类不认得，或者数离谱
	Kinds   map[string]int
}

// Summary 是给人看的一句话。
func (r ImportResult) Summary() string {
	if r.Added == 0 {
		switch {
		case r.Skipped > 0:
			return fmt.Sprintf("没有新的，%d 条之前已经导过了", r.Skipped)
		case r.Bad > 0:
			return fmt.Sprintf("没导进来什么，%d 条认不出来或者数不对劲", r.Bad)
		default:
			return "「健康」里没有可导的数据"
		}
	}
	// 按种类说清楚。只报一个总数的话，用户不知道到底是体重进来了
	// 还是只有步数——而这两件事的意义差得远。
	kinds := make([]string, 0, len(r.Kinds))
	for k := range r.Kinds {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if r.Kinds[kinds[i]] != r.Kinds[kinds[j]] {
			return r.Kinds[kinds[i]] > r.Kinds[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", k, r.Kinds[k]))
	}
	s := fmt.Sprintf("导入 %d 条（%s）", r.Added, strings.Join(parts, "、"))
	if r.Skipped > 0 {
		s += fmt.Sprintf("，%d 条之前导过跳了", r.Skipped)
	}
	if r.Bad > 0 {
		s += fmt.Sprintf("，%d 条认不出来或数不对劲", r.Bad)
	}
	return s
}

// ImportSamples 把外来的读数转成 FHIR 存进去。
//
// 三道关，按代价从小到大排：已经导过的直接跳（最常见，每次导都要过一遍
// 全部历史）；种类不认得的丢掉，不硬塞到相近的那一项里；数离谱的也丢掉——
// 秤没踩稳、表贴得不好都会冒出假读数，记进去以后看趋势的图就毁了。
func (s *Store) ImportSamples(samples []Sample) (ImportResult, error) {
	fresh, res := s.PrepareImport(samples)
	if len(fresh) == 0 {
		return res, nil
	}
	if err := s.AppendAll(fresh); err != nil {
		return ImportResult{Kinds: map[string]int{}}, err
	}
	return res, nil
}

// PrepareImport 做转换和去重，但**不落盘**。
//
// 手机上 Go 碰不了 iCloud 里的文件——沙箱授权、占位符、写协调那些讲究
// 全在 Swift 那边。所以那条路要的是「并出该追加的那几行」，
// 落盘由界面去做。电脑上走 ImportSamples，它接着调 AppendAll。
func (s *Store) PrepareImport(samples []Sample) ([]Resource, ImportResult) {
	res := ImportResult{Kinds: map[string]int{}}
	known := s.importedUIDs()

	var fresh []Resource
	for _, sp := range samples {
		if sp.UID != "" && known[sp.UID] {
			res.Skipped++
			continue
		}
		r, ok := s.toResource(sp)
		if !ok {
			res.Bad++
			continue
		}
		fresh = append(fresh, r)
		res.Added++
		res.Kinds[sp.Kind]++
		if sp.UID != "" {
			known[sp.UID] = true // 同一趟里重复给了也只记一条
		}
	}
	return fresh, res
}

// Lines 把资源序列化成文件里的那几行（各自带换行）。
func Lines(rs []Resource) (string, error) {
	var b strings.Builder
	for _, r := range rs {
		line, err := marshalLine(r)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

// importedUIDs 收集已经导进来过的原系统 UUID。
func (s *Store) importedUIDs() map[string]bool {
	out := map[string]bool{}
	for _, r := range s.Resources {
		for _, id := range r.Identifier {
			if id.System == SourceHealthKit && id.Value != "" {
				out[id.Value] = true
			}
		}
	}
	return out
}

func (s *Store) toResource(sp Sample) (Resource, bool) {
	kind := strings.TrimSpace(sp.Kind)
	v, ok := vitals[kind]
	if !ok || kind == "BMI" {
		// BMI 不导：它是体重除以身高算出来的，从那边搬一份过来，
		// 万一两边身高不一样就会跟自己算的那条打架。
		return Resource{}, false
	}
	at := sp.At
	if at.IsZero() {
		return Resource{}, false
	}
	note := "来自「健康」"
	if sp.Source != "" {
		note += " · " + sp.Source
	}

	var r Resource
	if kind == "血压" {
		if rangeCheck("收缩压", sp.Value) != nil || rangeCheck("舒张压", sp.Value2) != nil {
			return Resource{}, false
		}
		r = newBloodPressure(sp.Value, sp.Value2, at, note)
	} else {
		if rangeCheck(kind, sp.Value) != nil {
			return Resource{}, false
		}
		r = newObservation(kind, v, sp.Value, at, note)
	}
	if sp.UID != "" {
		r.Identifier = []Identifier{{System: SourceHealthKit, Value: sp.UID}}
	}
	return r, true
}

// AppendAll 一次写多条。
//
// 一条一次的话，导三个月的数据就是几百次开文件、写、关文件；更要紧的是
// 中间失败会留下一半——而这个文件没有「撤销」。攒成一次写下去，
// 要么整批进去，要么一条都没进。
func (s *Store) AppendAll(rs []Resource) error {
	if len(rs) == 0 {
		return nil
	}
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("还没配健康记录文件")
	}
	text, err := Lines(rs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开健康记录失败: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		return fmt.Errorf("写入健康记录失败: %w", err)
	}
	s.Resources = append(s.Resources, rs...)
	return nil
}
