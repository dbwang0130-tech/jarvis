// Package memory 提供秘书的长期记忆：用户可以让它记住偏好，跨会话保留。
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dabao/jarvis/internal/agent"

	"github.com/dabao/jarvis/internal/config"
)

// Skill 是一份持久化记忆，内容来自用户明确要求记住的偏好。
type Skill struct {
	Path    string
	Content string
}

// New 构造记忆技能，从文件加载已有内容。
func New(path string) *Skill {
	s := &Skill{Path: path}
	raw, err := os.ReadFile(path)
	if err == nil {
		s.Content = string(raw)
	}
	return s
}

func (s *Skill) Name() string { return "记忆" }

// Describe 把长期偏好写进系统提示。
func (s *Skill) Describe(b *strings.Builder) {
	content := strings.TrimSpace(s.Content)
	if content == "" {
		return
	}
	b.WriteString("用户要求长期记住的偏好：\n")
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			fmt.Fprintf(b, "- %s\n", line)
		}
	}
}

func (s *Skill) Instructions() string {
	return "用户说「记住」「以后记得」「我习惯」「我不喜欢」这类话时，用 remember 把偏好存进长期记忆。"
}

// Tools 返回记忆工具。
func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{s.toolRemember()}
}

type tool struct {
	spec agent.ToolSpec
	run  func(args string) (string, bool, error)
}

func (t tool) Spec() agent.ToolSpec                   { return t.spec }
func (t tool) Risk() agent.Risk                       { return agent.RiskNone }
func (t tool) Preview(string) (string, string, error) { return "", "", nil }
func (t tool) Run(args string) (string, bool, error)  { return t.run(args) }

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func (s *Skill) toolRemember() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "remember",
			Description: "记住用户的一条长期偏好。用户明确说「记住/以后记得/我习惯」时用。",
			Parameters: obj(map[string]any{
				"note": str("要记住的内容，一句话说清"),
			}, "note"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Note string `json:"note"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			note := strings.TrimSpace(a.Note)
			if note == "" {
				return "", false, fmt.Errorf("要记住的内容不能为空")
			}
			if err := s.append(note); err != nil {
				return "", false, err
			}
			s.Content = strings.TrimSpace(s.Content+"\n"+note) + "\n"
			return "已记住：" + note, false, nil
		},
	}
}

func (s *Skill) append(note string) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, note)
	return err
}

// Save 用整理后的内容整体覆盖记忆文件。
func (s *Skill) Save(content string) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	// 原子写：整理记忆是整份覆盖，写到一半被杀掉就等于把记忆清空了。
	return config.WriteAtomic(s.Path, []byte(strings.TrimSpace(content)+"\n"), 0o600)
}
