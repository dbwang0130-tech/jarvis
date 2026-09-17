// Package workflow 提供可复用的命名工作流：把反复要做的多步流程存下来，一键触发。
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dabao/jarvis/internal/agent"

	"github.com/dabao/jarvis/internal/config"
)

// Skill 是命名工作流的存储与执行技能。
type Skill struct {
	Path string
}

// New 构造工作流技能。
func New(path string) *Skill { return &Skill{Path: path} }

func (s *Skill) Name() string { return "工作流" }

func (s *Skill) Describe(b *strings.Builder) {
	m := s.load()
	if len(m) == 0 {
		b.WriteString("还没有保存的工作流（可用 save_workflow 保存）。\n")
		return
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	b.WriteString("已保存的工作流：")
	for _, n := range names {
		fmt.Fprintf(b, " %s（%d 步）", n, len(m[n]))
	}
	b.WriteString("\n")
}

func (s *Skill) Instructions() string {
	return `工作流：
1. 用户说「把这段流程存成 xxx」「以后经常要做 xxx」时，用 save_workflow 保存。
2. 用户说「跑一下 xxx」「执行 xxx 流程」时，用 run_workflow 取出步骤，先 plan 列出，再按顺序执行，每步完成调 step_done。
3. 执行中某一步结果不对，可以动态调整：重新 plan 或跳过该步。`
}

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{s.toolList(), s.toolSave(), s.toolDelete(), s.toolRun()}
}

type tool struct {
	spec     agent.ToolSpec
	run      func(args string) (string, bool, error)
	readonly bool
}

func (t tool) Spec() agent.ToolSpec                   { return t.spec }
func (t tool) Risk() agent.Risk                       { return agent.RiskNone }
func (t tool) Preview(string) (string, string, error) { return "", "", nil }
func (t tool) Run(args string) (string, bool, error)  { return t.run(args) }
func (t tool) ReadOnly() bool                         { return t.readonly }

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func arr(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func (s *Skill) load() map[string][]string {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return map[string][]string{}
	}
	var m map[string][]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string][]string{}
	}
	if m == nil {
		m = map[string][]string{}
	}
	return m
}

func (s *Skill) save(m map[string][]string) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteAtomic(s.Path, raw, 0o600)
}

func (s *Skill) toolList() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "list_workflows",
			Description: "列出已保存的工作流及其步骤数。",
			Parameters:  obj(map[string]any{}),
		},
		readonly: true,
		run: func(string) (string, bool, error) {
			m := s.load()
			if len(m) == 0 {
				return "还没有保存的工作流。\n", false, nil
			}
			names := make([]string, 0, len(m))
			for k := range m {
				names = append(names, k)
			}
			sort.Strings(names)
			var b strings.Builder
			for _, n := range names {
				fmt.Fprintf(&b, "- %s（%d 步）\n", n, len(m[n]))
			}
			return b.String(), false, nil
		},
	}
}

func (s *Skill) toolSave() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "save_workflow",
			Description: "保存一个命名工作流。用户说「把这段流程存成 xxx」时用。",
			Parameters: obj(map[string]any{
				"name":  str("工作流名称，如 晨间简报"),
				"steps": arr("按顺序排列的步骤，每步一句话"),
			}, "name", "steps"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Name  string   `json:"name"`
				Steps []string `json:"steps"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			name := strings.TrimSpace(a.Name)
			if name == "" {
				return "", false, fmt.Errorf("必须提供工作流名称")
			}
			var steps []string
			for _, st := range a.Steps {
				if st = strings.TrimSpace(st); st != "" {
					steps = append(steps, st)
				}
			}
			if len(steps) == 0 {
				return "", false, fmt.Errorf("工作流至少需要一步")
			}
			m := s.load()
			m[name] = steps
			if err := s.save(m); err != nil {
				return "", false, err
			}
			return fmt.Sprintf("已保存工作流「%s」（%d 步）。", name, len(steps)), false, nil
		},
	}
}

func (s *Skill) toolDelete() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "delete_workflow",
			Description: "删除一个命名工作流。",
			Parameters:  obj(map[string]any{"name": str("工作流名称")}, "name"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			name := strings.TrimSpace(a.Name)
			m := s.load()
			if _, ok := m[name]; !ok {
				return "", false, fmt.Errorf("没有名为 %q 的工作流", name)
			}
			delete(m, name)
			if err := s.save(m); err != nil {
				return "", false, err
			}
			return fmt.Sprintf("已删除工作流「%s」。", name), false, nil
		},
	}
}

func (s *Skill) toolRun() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "run_workflow",
			Description: "取回一个工作流的步骤，然后按计划执行。",
			Parameters:  obj(map[string]any{"name": str("工作流名称")}, "name"),
		},
		readonly: true,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			name := strings.TrimSpace(a.Name)
			m := s.load()
			steps, ok := m[name]
			if !ok {
				return "", false, fmt.Errorf("没有名为 %q 的工作流，可用 list_workflows 查看", name)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "工作流「%s」的步骤：\n", name)
			for i, st := range steps {
				fmt.Fprintf(&b, "%d. %s\n", i+1, st)
			}
			b.WriteString("\n请先调 plan 列出这些步骤，然后按顺序执行，每完成一步调 step_done；中途情况变化可重新 plan。")
			return b.String(), false, nil
		},
	}
}
