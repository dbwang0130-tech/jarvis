package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

const voidHealthSample = `{"resourceType":"Observation","status":"final","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-17T09:00:00+08:00","valueQuantity":{"value":725,"unit":"kg"}}
`

func voidHealth(t *testing.T, in VoidHealthInput) VoidHealthOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out VoidHealthOutput
	if err := json.Unmarshal([]byte(VoidHealth(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

func TestVoidHealth(t *testing.T) {
	out := voidHealth(t, VoidHealthInput{
		Health: voidHealthSample, Kind: "体重",
		At: "2026-09-17T09:00:00+08:00", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("标不上：%s", out.Error)
	}
	if !strings.Contains(out.Health, "entered-in-error") {
		t.Fatalf("没标上：\n%s", out.Health)
	}
	// 那一行还在，只是状态变了。
	if !strings.Contains(out.Health, `"value":725`) {
		t.Fatalf("把记录删掉了：\n%s", out.Health)
	}
}

func TestVoidHealthRefuses(t *testing.T) {
	for _, tc := range []struct{ name, kind, at string }{
		{"时刻对不上", "体重", "2020-01-01T00:00:00+08:00"},
		{"体征对不上", "身高", "2026-09-17T09:00:00+08:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := voidHealth(t, VoidHealthInput{
				Health: voidHealthSample, Kind: tc.kind, At: tc.at,
				TZ: "Asia/Shanghai",
			})
			if out.Error == "" {
				t.Fatal("该报错")
			}
			if out.Health != "" {
				t.Error("没改成却把全文交回去了")
			}
		})
	}

	// 烂入参不许 panic。
	var out VoidHealthOutput
	if err := json.Unmarshal([]byte(VoidHealth("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}
}
