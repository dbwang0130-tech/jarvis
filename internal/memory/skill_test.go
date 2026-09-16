package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func remember(t *testing.T, s *Skill, note string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"note": note})
	out, _, err := s.Tools()[0].Run(string(raw))
	if err != nil {
		t.Fatalf("记不住：%v", err)
	}
	return out
}

func TestRememberPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	s := New(path)

	remember(t, s, "我习惯早上处理邮件")
	remember(t, s, "不喜欢周五开会")

	// 重新打开一份，该读得到刚才记的东西。
	again := New(path)
	for _, want := range []string{"早上处理邮件", "不喜欢周五开会"} {
		if !strings.Contains(again.Content, want) {
			t.Fatalf("记忆里没有 %q：\n%s", want, again.Content)
		}
	}
}

// 记忆要进系统提示，否则模型看不到。
func TestDescribeFeedsThePrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.md")
	s := New(path)
	remember(t, s, "称呼我先生")

	var b strings.Builder
	s.Describe(&b)
	if !strings.Contains(b.String(), "称呼我先生") {
		t.Fatalf("系统提示里没带上记忆：%q", b.String())
	}
}

// 空记忆不该往系统提示里塞废话。
func TestDescribeEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "没有这个文件.md"))
	var b strings.Builder
	s.Describe(&b)
	if b.Len() != 0 {
		t.Fatalf("没有记忆时该什么都不说，却写了 %q", b.String())
	}
}

func TestNewOnMissingFile(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "还没有.md"))
	if s.Content != "" {
		t.Fatalf("文件不存在时内容该是空的，得到 %q", s.Content)
	}
	// 而且这时候记一条也要能成。
	remember(t, s, "第一条")
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatalf("该把文件建出来：%v", err)
	}
}

// 空的备注不该往记忆里塞空行。
func TestRememberRejectsEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory.md"))
	raw, _ := json.Marshal(map[string]any{"note": "   "})
	if _, _, err := s.Tools()[0].Run(string(raw)); err == nil {
		t.Fatal("空备注该被挡下来")
	}
}
