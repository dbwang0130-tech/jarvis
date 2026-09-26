package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Step 是计划里的一步。
type Step struct {
	Text string
	Done bool
}

// planTool 让模型把多步任务先列成计划。
type planTool struct{ a *Agent }

func (t planTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "plan",
		Description: "把要做的事列成步骤清单，让用户看得见你的计划。三步以上的任务先调它；执行过程中情况有变可以重新调它更新计划。",
		Parameters: Obj(map[string]any{
			"steps": Arr(map[string]any{"type": "string"}, "按顺序排列的步骤，每步一句话"),
		}, "steps"),
	}
}
func (t planTool) Risk() Risk                             { return RiskNone }
func (t planTool) Preview(string) (string, string, error) { return "", "", nil }
func (t planTool) Run(raw string) (string, bool, error) {
	var a struct {
		Steps []string `json:"steps"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", false, err
	}
	var steps []Step
	for _, s := range a.Steps {
		if s = strings.TrimSpace(s); s != "" {
			steps = append(steps, Step{Text: s})
		}
	}
	if len(steps) == 0 {
		return "", false, fmt.Errorf("计划不能为空")
	}
	t.a.Plan = steps
	return fmt.Sprintf("计划已列出（%d 步），已展示给用户。按计划执行，每完成一步调 step_done。", len(steps)), false, nil
}

// stepDoneTool 标记计划的某一步完成。
type stepDoneTool struct{ a *Agent }

func (t stepDoneTool) Spec() ToolSpec {
	return ToolSpec{
		Name:        "step_done",
		Description: "标记计划里的第几步已完成（从 1 开始）。每完成一步就调一次，用户才能看到进度。",
		Parameters:  Obj(map[string]any{"step": Int("第几步，从 1 开始")}, "step"),
	}
}
func (t stepDoneTool) Risk() Risk                             { return RiskNone }
func (t stepDoneTool) Preview(string) (string, string, error) { return "", "", nil }
func (t stepDoneTool) Run(raw string) (string, bool, error) {
	var a struct {
		Step int `json:"step"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", false, err
	}
	if a.Step < 1 || a.Step > len(t.a.Plan) {
		return "", false, fmt.Errorf("步骤 %d 超出计划范围（共 %d 步）", a.Step, len(t.a.Plan))
	}
	t.a.Plan[a.Step-1].Done = true
	done := 0
	for _, s := range t.a.Plan {
		if s.Done {
			done++
		}
	}
	return fmt.Sprintf("第 %d 步完成（%d/%d）。", a.Step, done, len(t.a.Plan)), false, nil
}
