package health

import (
	"strings"
	"testing"
	"time"
)

func sample(kind string, v float64, uid string, day int) Sample {
	return Sample{
		Kind: kind, Value: v, UID: uid, Source: "Apple Watch",
		At: time.Date(2026, 9, day, 8, 0, 0, 0, time.Local),
	}
}

// **导第二次不能变两份。**
//
// 文件是只追加的，没有撤销。重复导一次，趋势图上一个点就变成两个点，
// 而且删不掉。靠的是每条带着原系统的 UUID。
func TestImportIsIdempotent(t *testing.T) {
	s, _ := newTest(t)
	batch := []Sample{
		sample("体重", 72.5, "uuid-a", 16),
		sample("体重", 72.1, "uuid-b", 17),
		sample("静息心率", 58, "uuid-c", 17),
	}

	r1, err := s.Store.ImportSamples(batch)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Added != 3 || r1.Skipped != 0 {
		t.Fatalf("第一次：加 %d 跳 %d", r1.Added, r1.Skipped)
	}

	// 原样再导一遍
	r2, err := s.Store.ImportSamples(batch)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Added != 0 || r2.Skipped != 3 {
		t.Fatalf("第二次该全跳过：加 %d 跳 %d", r2.Added, r2.Skipped)
	}

	// 换个进程重新读一遍，去重靠的是文件里存下来的 identifier，
	// 不是内存里那份表——关掉 app 再导同样不能重复。
	reopened, err := Load(s.Store.Path)
	if err != nil {
		t.Fatal(err)
	}
	r3, err := reopened.ImportSamples(append(batch, sample("体重", 71.9, "uuid-d", 18)))
	if err != nil {
		t.Fatal(err)
	}
	if r3.Added != 1 || r3.Skipped != 3 {
		t.Fatalf("重开之后：加 %d 跳 %d", r3.Added, r3.Skipped)
	}
}

// 认不出来的种类、离谱的读数都丢掉，不硬塞。
//
// 秤没踩稳、表没贴好都会冒出假读数。记进去之后趋势图就毁了，
// 而当时人不会注意到。
func TestImportDropsJunk(t *testing.T) {
	s, _ := newTest(t)
	r, err := s.Store.ImportSamples([]Sample{
		sample("体重", 72.5, "ok", 16),
		sample("血脂", 5.2, "unknown-kind", 16),   // 还没支持的项目
		sample("体重", 725, "misplaced-dot", 16),  // 点错小数点
		sample("体温", 0, "sensor-glitch", 16),    // 传感器抽风
		{Kind: "体重", Value: 70, UID: "no-time"}, // 没时间戳
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.Added != 1 {
		t.Errorf("该只进来一条，实际 %d", r.Added)
	}
	if r.Bad != 4 {
		t.Errorf("该丢掉四条，实际 %d", r.Bad)
	}
}

// BMI 不从「健康」导：它是体重除以身高算出来的，搬一份过来，
// 万一两边身高不一样就会跟自己算的那条打架。
func TestImportSkipsDerivedBMI(t *testing.T) {
	s, _ := newTest(t)
	r, _ := s.Store.ImportSamples([]Sample{sample("BMI", 23.7, "bmi-1", 16)})
	if r.Added != 0 {
		t.Errorf("BMI 不该导进来")
	}
}

// 导进来的血压同样是一条带两个分项的记录，跟手记的一个样。
func TestImportedBloodPressureKeepsComponents(t *testing.T) {
	s, _ := newTest(t)
	sp := sample("血压", 126, "bp-1", 16)
	sp.Value2 = 82
	if _, err := s.Store.ImportSamples([]Sample{sp}); err != nil {
		t.Fatal(err)
	}
	r, ok := s.Store.Latest("血压")
	if !ok {
		t.Fatal("没导进来")
	}
	if len(r.Component) != 2 {
		t.Fatalf("分项 = %d", len(r.Component))
	}
	if r.label() != "血压 126/82 mmHg" {
		t.Errorf("显示成了 %q", r.label())
	}
	// 来源要留痕：将来分不清哪条是自己量的、哪条是表记的就麻烦了。
	if len(r.Note) == 0 || !strings.Contains(r.Note[0].Text, "Apple Watch") {
		t.Errorf("没记下来源：%v", r.Note)
	}
	if len(r.Identifier) != 1 || r.Identifier[0].System != SourceHealthKit {
		t.Errorf("没记下原系统 UUID：%v", r.Identifier)
	}
}

// 一条都没新增时别去碰文件。
func TestImportNothingLeavesFileAlone(t *testing.T) {
	s, _ := newTest(t)
	fresh, res := s.Store.PrepareImport(nil)
	if len(fresh) != 0 || res.Added != 0 {
		t.Fatal("空输入不该产出任何东西")
	}
	if got := res.Summary(); !strings.Contains(got, "没有可导") {
		t.Errorf("说法不对：%q", got)
	}
}
