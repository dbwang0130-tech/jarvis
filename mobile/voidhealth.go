package mobile

import (
	"encoding/json"
	"fmt"

	"github.com/dabao/jarvis/internal/health"
)

// VoidHealthInput 把一条健康记录标成「录错了」。
//
// 体重打错一位、饮食记重了，以前没有出路：那条会一直参与趋势和
// 「最近一次」，只能到电脑上去改文件。
type VoidHealthInput struct {
	// Health 是健康记录全文（NDJSON）。
	Health string `json:"health"`
	// Kind 是体征名（体重、血压…）或者「饮食」。
	Kind string `json:"kind"`
	// At 是这条记录的时刻，RFC3339。
	//
	// 按时刻定位而不是下标：下标会因为文件在别处被追加过而错位，而
	// 「什么时候量的」不会变。
	At string `json:"at"`

	TZ string `json:"tz"`
}

// VoidHealthOutput 只在真改过之后才带 Health。
type VoidHealthOutput struct {
	Message string `json:"message"`
	Health  string `json:"health,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (o *VoidHealthOutput) normalize() {}

// VoidHealth 把一条记录标成「录错了」。
//
// **不删那一行**：FHIR 里记错的记录是改 status 成 entered-in-error，
// 原样留在文件里——医疗记录跟账本一样不改历史。
func VoidHealth(inputJSON string) string {
	var in VoidHealthInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&VoidHealthOutput{
			Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	out, err := health.Void(in.Health, in.Kind, in.At)
	if err != nil {
		return marshal(&VoidHealthOutput{Error: err.Error()})
	}
	return marshal(&VoidHealthOutput{
		Message: "标成「录错了」了，这条不再算数",
		Health:  out,
	})
}
