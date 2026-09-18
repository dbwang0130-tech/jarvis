package mobile

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/health"
)

// HealthImportInput 是从「健康」App 导数据的入参。
//
// 文件全文过桥传进来、改完再传回去，跟 Ask 一个路子——Go 在 iOS 上碰不了
// 沙箱外的文件，iCloud 那些讲究全留在 Swift 的 Vault 里。
type HealthImportInput struct {
	// Health 是现有的健康记录全文，没有就留空。
	Health string `json:"health"`
	// Samples 是从 HealthKit 读出来的读数。
	Samples []health.Sample `json:"samples"`
	TZ      string          `json:"tz"`
}

// HealthImportOutput 是导完的结果。
type HealthImportOutput struct {
	// Health 只在真有新增时才给——一条都没加就别让界面白写一遍文件。
	Health  string `json:"health,omitempty"`
	Added   int    `json:"added"`
	Skipped int    `json:"skipped"`
	Bad     int    `json:"bad"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

// ImportHealth 把「健康」App 里的读数并进本地那份 FHIR 记录。
//
// 去重靠每条带着的原系统 UUID：导第二次时认出「这条已经有了」。
// 所以界面尽管放心重复导，不会越导越多。
func ImportHealth(inputJSON string) string {
	var in HealthImportInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&HealthImportOutput{
			Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	st, err := health.LoadString(in.Health)
	if err != nil {
		return marshal(&HealthImportOutput{Error: "现有记录读不出来：" + err.Error()})
	}
	// 没有路径，写盘那条走不通——这里要的是「并出一份新全文」，
	// 落盘交给 Swift 那边的 Vault（它才知道 iCloud 的讲究）。
	fresh, res := st.PrepareImport(in.Samples)
	lines, err := health.Lines(fresh)
	if err != nil {
		return marshal(&HealthImportOutput{Error: err.Error()})
	}
	out := &HealthImportOutput{
		Added: res.Added, Skipped: res.Skipped, Bad: res.Bad,
		Message: res.Summary(),
	}
	if res.Added > 0 {
		out.Health = ensureNewline(in.Health) + lines
	}
	return marshal(out)
}

// ensureNewline 保证接着往下写的时候不会跟最后一条挤在同一行。
func ensureNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

func (o *HealthImportOutput) normalize() {}
