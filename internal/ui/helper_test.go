package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// newModel 在临时目录上构造一个界面模型，避免测试改写仓库里的样例文件。
func newModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	entries, err := os.ReadDir("../../testdata")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("../../testdata", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	coll, err := org.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Files = []string{dir}
	orgWS := orgtools.NewWorkspace(coll, cfg)
	mailS := mail.NewSession(cfg)
	m := New(filepath.Join(dir, "config.json"), cfg, coll, orgWS, mailS, nil, nil, nil, errNoAgent{})
	m.SetClock(func() time.Time { return testNow })
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

var testNow = time.Date(2026, 9, 14, 8, 0, 0, 0, time.Local)

type errNoAgent struct{}

func (errNoAgent) Error() string { return "测试环境未配置秘书" }

// press 模拟一次按键。
func press(m *Model, keys string) {
	for _, k := range strings.Split(keys, " ") {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m.Update(msg)
	}
}

func typeText(m *Model, text string) {
	for _, r := range text {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// gotoView 从默认的秘书面板切到列表视图。
func gotoView(t *testing.T, m *Model, v view) {
	t.Helper()
	for i := 0; m.view != v; i++ {
		if i > 4 {
			t.Fatalf("切不到视图 %v", v)
		}
		press(m, "tab")
	}
}

func selectTitle(t *testing.T, m *Model, title string) {
	t.Helper()
	for i, r := range m.rows {
		if r.item != nil && r.item.Node.Title == title {
			m.cursor = i
			return
		}
	}
	t.Fatalf("列表中没有「%s」：%s", title, strings.Join(rowTitles(m), " / "))
}

func rowTitles(m *Model) []string {
	var out []string
	for _, r := range m.rows {
		if r.item != nil {
			out = append(out, r.item.Node.Title)
		}
	}
	return out
}
