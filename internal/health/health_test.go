package health

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 9, 18, 9, 0, 0, 0, time.Local)

func newTest(t *testing.T) (*Skill, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "health.ndjson")
	s := New(path)
	s.Now = func() time.Time { return at }
	return s, path
}

func run(t *testing.T, s *Skill, name, args string) string {
	t.Helper()
	for _, tl := range s.Tools() {
		if tl.Spec().Name == name {
			out, err := tl.Run(args)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return out
		}
	}
	t.Fatalf("没有 %s 这个工具", name)
	return ""
}

func runErr(t *testing.T, s *Skill, name, args string) error {
	t.Helper()
	for _, tl := range s.Tools() {
		if tl.Spec().Name == name {
			_, err := tl.Run(args)
			return err
		}
	}
	t.Fatalf("没有 %s 这个工具", name)
	return nil
}

// 存进去的每一行都得是**独立合法的 FHIR 资源**。
//
// 这是选 NDJSON 的全部理由：哪天要导给医院、导进 Apple 健康，直接就能用。
// 哪怕只是漏了 resourceType 或者把 LOINC 码写进了自定义字段，这份数据就
// 只有我们自己认得，跟自己发明一套格式没区别了。
func TestEveryLineIsAValidFHIRResource(t *testing.T) {
	s, path := newTest(t)
	run(t, s, "set_health_profile", `{"birth_date":"1985-03-12","gender":"男"}`)
	run(t, s, "record_vital", `{"kind":"身高","value":175}`)
	run(t, s, "record_vital", `{"kind":"体重","value":72.5}`)
	run(t, s, "record_vital", `{"kind":"血压","systolic":120,"diastolic":80}`)
	run(t, s, "record_vital", `{"kind":"睡眠","value":7.5,"note":"睡得还行"}`)
	run(t, s, "record_meal", `{"meal":"早餐","items":["小米粥","鸡蛋"]}`)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	// 六次调用，加上体重那次派生出来的 BMI
	if len(lines) != 7 {
		t.Fatalf("行数 = %d，期望 7：\n%s", len(lines), raw)
	}
	seen := map[string]int{}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("第 %d 行不是合法 JSON：%v", i+1, err)
		}
		rt, _ := m["resourceType"].(string)
		if rt == "" {
			t.Fatalf("第 %d 行没有 resourceType：%s", i+1, line)
		}
		seen[rt]++
		if rt == "Observation" {
			if m["status"] != "final" {
				t.Errorf("第 %d 行 Observation 缺 status", i+1)
			}
			code, _ := m["code"].(map[string]any)
			coding, _ := code["coding"].([]any)
			if len(coding) == 0 {
				t.Fatalf("第 %d 行没有 LOINC 编码：%s", i+1, line)
			}
			first, _ := coding[0].(map[string]any)
			if first["system"] != "http://loinc.org" || first["code"] == "" {
				t.Errorf("第 %d 行编码系统不对：%v", i+1, first)
			}
		}
	}
	if seen["Patient"] != 1 || seen["NutritionIntake"] != 1 || seen["Observation"] != 5 {
		t.Errorf("资源种类分布不对：%v", seen)
	}
}

// 血压必须是**一条带两个分项**的记录。
//
// 分成收缩压、舒张压两条独立记录的话，别的系统读进去不知道这两个数是
// 一次量出来的——而血压只有成对才有意义。
func TestBloodPressureIsOneResourceWithComponents(t *testing.T) {
	s, path := newTest(t)
	run(t, s, "record_vital", `{"kind":"血压","systolic":128,"diastolic":83}`)

	raw, _ := os.ReadFile(path)
	if n := len(strings.Split(strings.TrimSpace(string(raw)), "\n")); n != 1 {
		t.Fatalf("血压写成了 %d 条", n)
	}
	var r Resource
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Component) != 2 {
		t.Fatalf("分项 = %d", len(r.Component))
	}
	if r.ValueQuantity != nil {
		t.Error("血压不该有顶层 valueQuantity，两个数都在分项里")
	}
	got := []string{r.Component[0].Code.Coding[0].Code, r.Component[1].Code.Coding[0].Code}
	if got[0] != "8480-6" || got[1] != "8462-4" {
		t.Errorf("分项编码 = %v，期望 [8480-6 8462-4]", got)
	}
}

// 记完要把「上一次是多少」一起说出来。
//
// 只报新值的话，模型手上另一个数是系统提示里那份这一轮开始时的快照，
// 它会把两者之差当成异常——记账那边就是这么报的假警。
func TestRecordReportsPreviousValue(t *testing.T) {
	s, _ := newTest(t)
	s.Now = func() time.Time { return at.AddDate(0, 0, -7) }
	run(t, s, "record_vital", `{"kind":"体重","value":71.7}`)
	s.Now = func() time.Time { return at }
	out := run(t, s, "record_vital", `{"kind":"体重","value":72.5}`)

	if !strings.Contains(out, "71.7") {
		t.Errorf("没说上一次是多少：\n%s", out)
	}
	if !strings.Contains(out, "7 天前") {
		t.Errorf("没说上一次是什么时候：\n%s", out)
	}
}

// 明显不可能的数要拦下来。
//
// 挡的是点错小数点这类事：体重 725 公斤、体温 368 度。这种数记进去之后
// 看趋势的图就全毁了，而当时用户多半没注意到。
func TestRefusesImpossibleValues(t *testing.T) {
	s, _ := newTest(t)
	cases := []struct{ name, args string }{
		{"体重点错小数点", `{"kind":"体重","value":725}`},
		{"体温点错小数点", `{"kind":"体温","value":368}`},
		{"睡了 30 小时", `{"kind":"睡眠","value":30}`},
		{"收缩压离谱", `{"kind":"血压","systolic":1200,"diastolic":80}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := runErr(t, s, "record_vital", c.args); err == nil {
				t.Error("该拦下来的")
			}
		})
	}
	// 不认得的项目直说，别硬塞到相近的那一项里
	if err := runErr(t, s, "record_vital", `{"kind":"尿酸","value":420}`); err == nil {
		t.Error("不认得的体征该报错")
	} else if !strings.Contains(err.Error(), "体重") {
		t.Errorf("报错该列出认得哪些：%v", err)
	}
}

// 量了体重顺手记 BMI——身高在库里，这是能算准的派生值，
// 交给模型自己乘除只会时对时错。没量过身高就不算，别拿个默认身高凑数。
func TestBMIDerivedOnlyWhenHeightKnown(t *testing.T) {
	s, _ := newTest(t)
	run(t, s, "record_vital", `{"kind":"体重","value":72.5}`)
	if _, ok := s.Store.Latest("BMI"); ok {
		t.Error("没量过身高就不该有 BMI")
	}

	run(t, s, "record_vital", `{"kind":"身高","value":175}`)
	out := run(t, s, "record_vital", `{"kind":"体重","value":72.5}`)
	if !strings.Contains(out, "BMI") {
		t.Fatalf("量了身高之后该带出 BMI：\n%s", out)
	}
	b, ok := s.Store.Latest("BMI")
	if !ok || b.ValueQuantity == nil {
		t.Fatal("BMI 没记下来")
	}
	// 72.5 / 1.75² = 23.67
	if v := b.ValueQuantity.Value; v < 23.6 || v > 23.7 {
		t.Errorf("BMI = %v，期望 23.7 左右", v)
	}
}

// 改基础信息时，没传的那项要沿用上一版，不能把已经填好的抹成空。
func TestProfileKeepsUnsetFields(t *testing.T) {
	s, _ := newTest(t)
	run(t, s, "set_health_profile", `{"birth_date":"1985-03-12","gender":"男"}`)
	run(t, s, "set_health_profile", `{"gender":"女"}`)

	p, ok := s.Store.Patient()
	if !ok {
		t.Fatal("没有基础信息")
	}
	if p.BirthDate != "1985-03-12" {
		t.Errorf("生日被抹掉了：%q", p.BirthDate)
	}
	if p.Gender != "female" {
		t.Errorf("性别 = %q", p.Gender)
	}
}

// 文件里有一行坏数据，不能让整份记录作废——别的工具追加过、
// 或者上次写到一半断电，都会留下半行。丢那一行远好过丢几年的记录。
func TestBadLineDoesNotKillTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.ndjson")
	good := `{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-18T09:00:00+08:00","valueQuantity":{"value":72.5,"unit":"kg"}}`
	os.WriteFile(path, []byte(good+"\n{半行坏数据\n\n"+good+"\n"), 0o600)

	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Resources) != 2 {
		t.Errorf("读出来 %d 条，期望 2", len(st.Resources))
	}
}

// 环境快照要给出「最近一次是多少、多久以前」——模型判断这次的数正不正常、
// 跟上次比怎么样，只有这一个依据。
func TestDescribeShowsLatestOfEach(t *testing.T) {
	s, _ := newTest(t)
	run(t, s, "set_health_profile", `{"birth_date":"1985-03-12","gender":"男"}`)
	run(t, s, "record_vital", `{"kind":"体重","value":72.5}`)
	run(t, s, "record_vital", `{"kind":"血压","systolic":120,"diastolic":80}`)
	run(t, s, "record_meal", `{"meal":"早餐","items":["小米粥"]}`)

	var b strings.Builder
	s.Describe(&b)
	got := b.String()
	// 断言的是**内容**不是排版：项目名、值、时间各自在不在。
	// 钉住整行的话，调一下列宽就红，而那不是这个测试要守的东西。
	// （旧版这里把项目名印了两遍——`%-6s` 给 name，`%-16s` 给 label，
	//  而 label 自己就以项目名打头。换成 Summary 之后没这毛病了。）
	for _, want := range []string{"41 岁", "体重", "72.5kg", "血压", "120/80 mmHg",
		"小米粥", "这一轮对话开始时"} {
		if !strings.Contains(got, want) {
			t.Errorf("快照里少了 %q：\n%s", want, got)
		}
	}
}
