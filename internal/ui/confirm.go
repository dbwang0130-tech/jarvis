package ui

import tea "github.com/charmbracelet/bubbletea"

// pendingAction 是一次等待用户点头的危险操作。
type pendingAction struct {
	question string
	run      func()
}

// askConfirm 弹出确认提示，用户按 y 才执行。
func (m *Model) askConfirm(question string, run func()) (tea.Model, tea.Cmd) {
	m.confirm = &pendingAction{question: question, run: run}
	m.prompt = promptConfirm
	m.input.Blur()
	return m, nil
}

// updateConfirm 处理确认提示下的按键：y 执行，其余一律取消。
func (m *Model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action := m.confirm
	m.confirm = nil
	m.prompt = promptNone

	switch msg.String() {
	case "y", "Y", "enter":
		if action != nil {
			action.run()
		}
	default:
		m.setStatus("已取消")
	}
	// 秘书面板下确认完把焦点还回输入框。
	if m.view == viewChat && !m.chatBrowse && !m.busy {
		return m, m.focusChat()
	}
	return m, nil
}
