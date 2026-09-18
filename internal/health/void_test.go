package health

import (
	"strings"
	"testing"
	"time"
)

// 记错了的健康记录以前没有出路：体重打错一位，那条会一直参与趋势和
// 「最近一次」，只能到电脑上去改文件。
//
// FHIR 的规矩是不删——把 status 改成 entered-in-error，那条留在文件里，
// 读的人当它不存在。跟账本记错了用冲账是同一个道理。
const voidSample = `{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-16T09:00:00+08:00","valueQuantity":{"value":72.5,"unit":"kg"}}
{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-17T09:00:00+08:00","valueQuantity":{"value":725,"unit":"kg"}}
{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-18T09:00:00+08:00","valueQuantity":{"value":72.3,"unit":"kg"}}
`

func TestVoid(t *testing.T) {
	out, err := Void(voidSample, "体重", "2026-09-17T09:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "entered-in-error") {
		t.Fatalf("没标上：\n%s", out)
	}
	// **那一行还在**，只是状态变了。删掉的话说不清当时到底记过什么。
	if !strings.Contains(out, `"value":725`) {
		t.Fatalf("把记录删掉了：\n%s", out)
	}
	// 别的行一个字节都不许动。
	for _, keep := range []string{`"value":72.5`, `"value":72.3`} {
		if !strings.Contains(out, keep) {
			t.Fatalf("动了别的行（%s 不见了）：\n%s", keep, out)
		}
	}

	// 标完之后，那条不该再参与趋势和「最近一次」。
	s, err := LoadString(out)
	if err != nil {
		t.Fatal(err)
	}
	hist := s.history("体重")
	if len(hist) != 2 {
		t.Fatalf("还剩 %d 条，该是 2 条", len(hist))
	}
	for _, r := range hist {
		if r.ValueQuantity != nil && r.ValueQuantity.Value == 725 {
			t.Fatal("标了「录错了」的还在算数")
		}
	}
	if latest, ok := s.Latest("体重"); ok && latest.ValueQuantity.Value == 725 {
		t.Fatal("「最近一次」取到了录错的那条")
	}
}

// 标过的不能再标——重复操作该说清楚，而不是悄悄写一遍一样的东西。
func TestVoidTwice(t *testing.T) {
	out, err := Void(voidSample, "体重", "2026-09-17T09:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Void(out, "体重", "2026-09-17T09:00:00+08:00"); err == nil {
		t.Fatal("标过的再标该报错")
	}
}

// 定位靠体征名加时刻，对不上就别动——标错一条，人要过很久才发现。
func TestVoidNotFound(t *testing.T) {
	for _, tc := range []struct{ name, kind, at string }{
		{"时刻对不上", "体重", "2020-01-01T00:00:00+08:00"},
		{"体征对不上", "身高", "2026-09-17T09:00:00+08:00"},
		{"时刻看不懂", "体重", "昨天早上"},
		{"不认识的种类", "运气", "2026-09-17T09:00:00+08:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Void(voidSample, tc.kind, tc.at); err == nil {
				t.Error("该报错")
			}
		})
	}
}

// 别的工具（比如「健康」App 导出）写进来的 entered-in-error 也该认。
// 这是 FHIR 的标准状态，不是我们自己发明的约定。
func TestVoidedFromElsewhereIgnored(t *testing.T) {
	raw := `{"resourceType":"Observation","status":"entered-in-error","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-18T09:00:00+08:00","valueQuantity":{"value":999,"unit":"kg"}}
`
	s, err := LoadString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.history("体重")) != 0 {
		t.Fatal("别处标的「录错了」没认出来")
	}
	if len(s.Recent("体重", time.Time{}, 10)) != 0 {
		t.Fatal("Recent 把作废记录算进去了")
	}
}
