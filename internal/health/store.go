package health

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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

	// byCode 是 LOINC 码 → 在 Resources 里的下标。
	//
	// 没有它的话，「某一项最近一次是多少」要把全部记录扫一遍，而快照要问
	// 十三项，每轮对话就是十三遍全表扫描。步数一天一条，这个文件只会越来越长。
	byCode map[string][]int
}

// scan 读 NDJSON。
//
// **坏行跳过，不整份作废。** 这个文件可能被别的工具追加过、也可能上次
// 写到一半断电留了半行。为一行坏数据把用户几年的记录全判死，比丢那一行
// 糟得多。
//
// 只此一份：电脑上读文件、手机上读过桥传来的全文，走的必须是同一套规矩，
// 否则同一个文件两头读出来的东西会不一样。
func scan(r io.Reader) ([]Resource, error) {
	sc := bufio.NewScanner(r)
	// 一行一条资源，正常都很短；给大一点的缓冲防着某条带长附注的。
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var out []Resource
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var res Resource
		if err := json.Unmarshal([]byte(line), &res); err != nil || res.ResourceType == "" {
			continue
		}
		// 时间在这儿解析一次就够。留给 when() 现解的话，排序比较器、
		// 过滤、格式化每碰一次就解一次，一个文件能解上几万遍。
		res.at = res.parseWhen()
		out = append(out, res)
	}
	return out, sc.Err()
}

// Load 读一份健康记录。文件不在不算错——第一次用本来就没有。
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

	s.Resources, err = scan(f)
	s.reindex()
	return s, err
}

// LoadString 从一份 NDJSON 全文里读记录。手机那边文件内容是过桥传进来的，
// 不落盘，所以不能走 Load。
func LoadString(raw string) (*Store, error) {
	s := &Store{}
	var err error
	s.Resources, err = scan(strings.NewReader(raw))
	s.reindex()
	return s, err
}

// codes 是「LOINC 码 → 下标」，第一次用到时才建。
//
// 必须走这个口子，别直接摸 byCode：Store 有好几条不经过 scan 的出生路径
// （文件不存在、没配路径、New 里读失败兜底的那份），它们的 byCode 是 nil，
// 直接 append 进去就是空指针 panic——手机上崩一次就是整个 app 挂掉。
func (s *Store) codes() map[string][]int {
	if s.byCode == nil {
		s.reindex()
	}
	return s.byCode
}

func (s *Store) reindex() {
	s.byCode = make(map[string][]int, len(vitalList))
	for i := range s.Resources {
		if c := s.Resources[i].codeValue(); c != "" {
			s.byCode[c] = append(s.byCode[c], i)
		}
	}
}

// marshalLine 把一条资源序列化成文件里的一行（**带换行**）。
//
// 一行一条是这个格式的全部规矩，所以换行跟序列化绑在一处——
// 谁写文件都从这儿拿，不会有人忘了加。
func marshalLine(r Resource) (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
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

// AppendTo 把几条接到一份已有的全文后面，返回新的全文。
//
// 手机上 Go 碰不了 iCloud 里的文件（沙箱授权、占位符、写协调那些讲究都在
// Swift 那边），所以那条路要的是「并出新全文」而不是落盘。
// 「一行一条、接之前先确保有换行」这条格式规矩收在这儿，桥那层不用知道。
func AppendTo(existing string, rs []Resource) (string, error) {
	lines, err := Lines(rs)
	if err != nil {
		return "", err
	}
	if lines == "" {
		return existing, nil
	}
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		existing += "\n"
	}
	return existing + lines, nil
}

// Append 追加一条。
func (s *Store) Append(r Resource) error { return s.AppendAll([]Resource{r}) }

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
	idx := s.codes()
	for _, r := range rs {
		if c := r.codeValue(); c != "" {
			idx[c] = append(idx[c], len(s.Resources))
		}
		s.Resources = append(s.Resources, r)
	}
	return nil
}

// Void 把某项记录标成「录错了」，返回新的文件全文。
//
// **不删那一行。** FHIR 里记错的记录是改 status 成 entered-in-error，
// 原样留在文件里——医疗记录跟账本一样不改历史：删掉的话说不清当时到底
// 记过什么，别处（比如「健康」App）同步过来的那份也对不上。
//
// 按**体征名加时刻**定位，不按下标：下标会因为文件在别处被追加过而错位，
// 而一条记录的「什么时候量的」不会变。跟账本冲账要核对日期和收款方
// 是同一个道理——标错一条，人要过很久才发现。
//
// 按行重写，其余行一个字节都不碰：这个文件可能被别的工具追加过，也可能
// 有我们不认得的资源类型，整份重新生成会把它们弄坏。
func Void(raw, kind, at string) (string, error) {
	kind = strings.TrimSpace(kind)
	want, err := time.Parse(time.RFC3339, strings.TrimSpace(at))
	if err != nil {
		return raw, fmt.Errorf("时刻 %q 看不懂", at)
	}

	code := ""
	meals := kind == "饮食"
	if !meals {
		v, ok := vitals[kind]
		if !ok {
			return raw, fmt.Errorf("不认识的记录种类 %q", kind)
		}
		code = v.Code
	}

	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r Resource
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // 坏行跳过，跟 scan 一个规矩
		}
		if meals && r.ResourceType != "NutritionIntake" {
			continue
		}
		if code != "" && !r.hasCode(code) {
			continue
		}
		if !r.when().Equal(want) {
			continue
		}
		if r.voided() {
			return raw, fmt.Errorf("这条已经标过「录错了」")
		}
		r.Status = StatusVoided
		out, err := marshalLine(r)
		if err != nil {
			return raw, err
		}
		lines[i] = strings.TrimRight(out, "\n")
		return strings.Join(lines, "\n"), nil
	}
	return raw, fmt.Errorf("找不到这条记录，下拉刷新一下再试")
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
	h := s.history(name)
	if len(h) == 0 {
		return Resource{}, false
	}
	return h[len(h)-1], true
}

// Recent 返回最近的记录，新的在前。kind 为空表示不限种类，
// 也可以是 "饮食" 或某个体征名。
func (s *Store) Recent(kind string, since time.Time, limit int) []Resource {
	// 种类规整和查表在循环外做一次。写在循环里的话，每条记录都要
	// TrimSpace 一遍、查一次 map。
	kind = strings.TrimSpace(kind)
	meals := kind == "饮食"
	code := ""
	if !meals && kind != "" {
		v, ok := vitals[kind]
		if !ok {
			return nil
		}
		code = v.Code
	}
	var out []Resource
	for _, r := range s.Resources {
		if r.ResourceType == "Patient" {
			continue
		}
		// 标了「录错了」的当它不存在——那条仍在文件里，只是不算数。
		// 别处（比如「健康」App 导出）写进来的这个状态也认，它是 FHIR
		// 的标准状态，不是我们自己发明的约定。
		if r.voided() {
			continue
		}
		if code != "" && !r.hasCode(code) {
			continue
		}
		if meals && r.ResourceType != "NutritionIntake" {
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
