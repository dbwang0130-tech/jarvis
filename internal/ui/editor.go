package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/org"
)

// editDoneMsg 是外部编辑器退出后的回调。
type editDoneMsg struct {
	path string
	doc  *org.Document
	line int
	err  error
}

// editorCommand 返回要调起的编辑器，依次看 $VISUAL、$EDITOR，都没有就用 vi。
func editorCommand() []string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			if fields := strings.Fields(v); len(fields) > 0 {
				return fields
			}
		}
	}
	return []string{"vi"}
}

// editCurrent 把当前条目的整棵子树丢给外部编辑器，编辑完再读回来。
func (m *Model) editCurrent() (tea.Model, tea.Cmd) {
	doc, n := m.currentNode()
	if n == nil {
		m.setError(fmt.Errorf("请先选中一个条目"))
		return m, nil
	}
	if doc.Path == "" {
		m.setError(fmt.Errorf("这个文档还没有对应的文件"))
		return m, nil
	}

	// 临时文件用 .org 后缀，编辑器才好开语法高亮。
	f, err := os.CreateTemp("", "orgtui-*.org")
	if err != nil {
		m.setError(err)
		return m, nil
	}
	content := strings.Join(n.Subtree(), "\n")
	if _, err := f.WriteString(content + "\n"); err != nil {
		f.Close()
		m.setError(err)
		return m, nil
	}
	f.Close()

	argv := append(editorCommand(), f.Name())
	cmd := exec.Command(argv[0], argv[1:]...)
	line := n.Line
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editDoneMsg{path: f.Name(), doc: doc, line: line, err: err}
	})
}

// applyEdit 把编辑器里改完的内容写回文档。
func (m *Model) applyEdit(msg editDoneMsg) {
	defer os.Remove(msg.path)

	if msg.err != nil {
		m.setError(fmt.Errorf("编辑器退出异常：%w", msg.err))
		return
	}
	data, err := os.ReadFile(msg.path)
	if err != nil {
		m.setError(err)
		return
	}

	n := msg.doc.NodeAt(msg.line)
	if n == nil {
		m.setError(fmt.Errorf("条目已不在原位，改动没有写回"))
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		m.setError(fmt.Errorf("内容是空的，没有写回"))
		return
	}
	if !org.IsHeadline(lines[0]) {
		m.setError(fmt.Errorf("第一行得是 org 标题（以 * 开头），改动没有写回"))
		return
	}

	m.orgWS.Snapshot(msg.doc)
	title := org.Plain(n.Title)
	msg.doc.ReplaceSubtree(n, lines)
	if err := msg.doc.Save(); err != nil {
		m.setError(err)
		return
	}
	m.orgWS.Record(msg.doc, "编辑「%s」", title)

	if m.detail != nil {
		m.detail.line = msg.line
		m.detail.box = 0
	}
	m.rebuild()
	m.setStatus("已保存「%s」的改动，按 u 可撤销", org.Plain(headlineTitle(lines[0])))
}

// headlineTitle 从一行标题里取出标题文字，用于提示。
func headlineTitle(line string) string {
	t := strings.TrimLeft(line, "*")
	return strings.TrimSpace(t)
}

// editorName 返回将要使用的编辑器名字，用于界面提示。
func editorName() string {
	return filepath.Base(editorCommand()[0])
}
