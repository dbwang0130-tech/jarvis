package health

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 记错了的健康记录按 FHIR 的规矩不删，而是把 status 改成 entered-in-error，
// 留在文件里、读的人当它不存在。别的工具（比如「健康」App 导出）写进来的
// 这种记录也得认——这是 FHIR 的标准状态，不是我们自己发明的约定。
func TestVoidedRecordsIgnored(t *testing.T) {
	raw := `{"resourceType":"Observation","status":"entered-in-error","code":{"coding":[{"system":"http://loinc.org","code":"29463-7"}],"text":"体重"},"effectiveDateTime":"2026-09-18T09:00:00+08:00","valueQuantity":{"value":999,"unit":"kg"}}
`
	path := filepath.Join(t.TempDir(), "health.ndjson")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.history("体重")) != 0 {
		t.Fatal("「录错了」的记录没认出来")
	}
	if len(s.Recent("体重", time.Time{}, 10)) != 0 {
		t.Fatal("Recent 把作废记录算进去了")
	}
}
