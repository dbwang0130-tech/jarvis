package health

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store 是本地那份健康记录：一个 NDJSON 文件，一行一条 FHIR 资源。
//
// 只追加。跟账本一样，这个文件手机和电脑两头都在写，整文件重写就是在
// 互相吃改动——一行一条，O_APPEND 写下去，两头同时写也只是交错，不会丢。
type Store struct {
	Path      string
	Resources []Resource
}

// Load 读一份健康记录。文件不在不算错——第一次用本来就没有。
//
// **坏行跳过，不整份作废。** 这个文件可能被别的工具追加过、也可能上次
// 写到一半断电留了半行。为一行坏数据把用户几年的记录全判死，比丢那一行
// 糟得多。
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	if strings.TrimSpace(path) == "" {
		return s, nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	// 一行一条资源，正常都很短；给大一点的缓冲防着某条带长附注的。
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

// Append 追加一条。
func (s *Store) Append(r Resource) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("还没配健康记录文件")
	}
	raw, err := json.Marshal(r)
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
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("写入健康记录失败: %w", err)
	}
	s.Resources = append(s.Resources, r)
	return nil
}

// Patient 返回最后写进去的那份基础信息。
//
// 同一个 id 可以写很多条，后来的算数——文件只追加，改资料就是再记一版。
func (s *Store) Patient() (Resource, bool) {
	for i := len(s.Resources) - 1; i >= 0; i-- {
		if s.Resources[i].ResourceType == "Patient" {
			return s.Resources[i], true
		}
	}
	return Resource{}, false
}

// Latest 返回某个体征最近一次的记录。
func (s *Store) Latest(name string) (Resource, bool) {
	v, ok := vitals[name]
	if !ok {
		return Resource{}, false
	}
	var best Resource
	found := false
	for _, r := range s.Resources {
		if r.Code == nil || len(r.Code.Coding) == 0 || r.Code.Coding[0].Code != v.Code {
			continue
		}
		if !found || r.when().After(best.when()) {
			best, found = r, true
		}
	}
	return best, found
}

// Recent 返回最近的记录，新的在前。kind 为空表示不限种类，
// 也可以是 "饮食" 或某个体征名。
func (s *Store) Recent(kind string, since time.Time, limit int) []Resource {
	var out []Resource
	for _, r := range s.Resources {
		if r.ResourceType == "Patient" {
			continue
		}
		if !matches(r, kind) {
			continue
		}
		if !since.IsZero() && r.when().Before(since) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].when().After(out[j].when()) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func matches(r Resource, kind string) bool {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return true
	}
	if kind == "饮食" {
		return r.ResourceType == "NutritionIntake"
	}
	v, ok := vitals[kind]
	if !ok {
		return false
	}
	return r.Code != nil && len(r.Code.Coding) > 0 && r.Code.Coding[0].Code == v.Code
}

// bmi 按最近的身高算 BMI。没量过身高就算不出来，返回 false——
// 拿个默认身高凑一个数出来，比不给更糟。
func (s *Store) bmi(weightKg float64) (float64, bool) {
	h, ok := s.Latest("身高")
	if !ok || h.ValueQuantity == nil || h.ValueQuantity.Value <= 0 {
		return 0, false
	}
	m := h.ValueQuantity.Value / 100
	return weightKg / (m * m), true
}
