package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func toolNamed(t *testing.T, s *Skill, name string) agent.Tool {
	t.Helper()
	for _, tl := range s.Tools() {
		if tl.Spec().Name == name {
			return tl
		}
	}
	t.Fatalf("没有名为 %s 的工具", name)
	return nil
}

func call(t *testing.T, s *Skill, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, _, err := toolNamed(t, s, name).Run(string(raw))
	return out, err
}

func newSkill(t *testing.T) *Skill {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "workflows.json"))
}

func TestSaveListRunRoundTrip(t *testing.T) {
	s := newSkill(t)

	if _, err := call(t, s, "save_workflow", map[string]any{
		"name": "晨间简报", "steps": []string{"同步邮件", "挑出要紧的", "写成简报"},
	}); err != nil {
		t.Fatal(err)
	}

	list, err := call(t, s, "list_workflows", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "晨间简报") {
		t.Fatalf("列表里没有刚存的：%q", list)
	}

	// 跑一个工作流，拿到的应该是那几步，交给模型照着做。
	got, err := call(t, s, "run_workflow", map[string]any{"name": "晨间简报"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"同步邮件", "挑出要紧的", "写成简报"} {
		if !strings.Contains(got, step) {
			t.Fatalf("跑起来少了「%s」：%q", step, got)
		}
	}
}

// 换个实例读同一个文件，存过的东西还在。
func TestPersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflows.json")
	s := New(path)
	if _, err := call(t, s, "save_workflow", map[string]any{
		"name": "周报", "steps": []string{"翻本周日志"},
	}); err != nil {
		t.Fatal(err)
	}

	again := New(path)
	got, err := call(t, again, "list_workflows", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "周报") {
		t.Fatalf("换个实例就读不到了：%q", got)
	}
}

func TestSaveRejectsIncomplete(t *testing.T) {
	s := newSkill(t)
	cases := []map[string]any{
		{"name": "", "steps": []string{"一步"}},
		{"name": "没有步骤", "steps": []string{}},
		{"name": "全是空白", "steps": []string{"  ", ""}},
	}
	for _, c := range cases {
		if _, err := call(t, s, "save_workflow", c); err == nil {
			t.Errorf("%v 该被挡下来", c)
		}
	}
}

func TestDelete(t *testing.T) {
	s := newSkill(t)
	if _, err := call(t, s, "save_workflow", map[string]any{
		"name": "临时的", "steps": []string{"随便一步"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, "delete_workflow", map[string]any{"name": "临时的"}); err != nil {
		t.Fatal(err)
	}
	got, _ := call(t, s, "list_workflows", map[string]any{})
	if strings.Contains(got, "临时的") {
		t.Fatalf("删掉了还在列表里：%q", got)
	}
}

// 跑一个不存在的工作流要说清楚，而不是返回空。
func TestRunMissing(t *testing.T) {
	s := newSkill(t)
	out, err := call(t, s, "run_workflow", map[string]any{"name": "没这个"})
	if err == nil && !strings.Contains(out, "没这个") {
		t.Fatalf("该提示找不到：%q", out)
	}
}

// 文件坏了不能让秘书起不来——当作一个都没有就行。
func TestBrokenFileDegradesGracefully(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workflows.json")
	if err := writeFile(path, "这不是 JSON"); err != nil {
		t.Fatal(err)
	}
	s := New(path)
	if _, err := call(t, s, "list_workflows", map[string]any{}); err != nil {
		t.Fatalf("文件坏了不该炸：%v", err)
	}
	// 而且还能照常存新的。
	if _, err := call(t, s, "save_workflow", map[string]any{
		"name": "新的", "steps": []string{"一步"},
	}); err != nil {
		t.Fatalf("坏文件之后存不进去：%v", err)
	}
}
