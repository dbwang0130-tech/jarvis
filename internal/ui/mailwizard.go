package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/config"
)

// startMailWizard 启动 /mail 的逐步收集流程。
func (m *Model) startMailWizard() (tea.Model, tea.Cmd) {
	m.mailWizard = &mailWizard{}
	return m.startPrompt(promptMail, "添加邮箱 · 账号名", "如 gmail、qq、163")
}

// advanceMailWizard 按当前步骤推进配置向导。
func (m *Model) advanceMailWizard(value string) (tea.Model, tea.Cmd) {
	w := m.mailWizard
	if w == nil {
		return m, nil
	}
	value = strings.TrimSpace(value)

	switch w.step {
	case 0: // 账号名
		if value == "" {
			m.setError(fmt.Errorf("账号名不能为空"))
			return m.startPrompt(promptMail, "添加邮箱 · 账号名", "如 gmail、qq、163")
		}
		for _, a := range m.cfg.Accounts {
			if a.Name == value {
				m.setError(fmt.Errorf("账号名 %q 已存在", value))
				return m.startPrompt(promptMail, "添加邮箱 · 账号名", "换一个名字")
			}
		}
		w.acc.Name = value
		w.step++
		return m.startPrompt(promptMail, "添加邮箱 · 邮箱地址", "you@example.com")

	case 1: // 邮箱地址
		if !strings.Contains(value, "@") {
			m.setError(fmt.Errorf("邮箱地址不合法"))
			return m.startPrompt(promptMail, "添加邮箱 · 邮箱地址", "you@example.com")
		}
		w.acc.User = value
		w.step++
		return m.startPrompt(promptMail, "添加邮箱 · IMAP 服务器", "如 imap.gmail.com")

	case 2: // IMAP 服务器
		if value == "" {
			m.setError(fmt.Errorf("IMAP 服务器不能为空"))
			return m.startPrompt(promptMail, "添加邮箱 · IMAP 服务器", "如 imap.gmail.com")
		}
		w.acc.Host = value
		w.step++
		return m.startPrompt(promptMail, "添加邮箱 · IMAP 端口", "默认 993，直接回车")

	case 3: // 端口
		if value == "" {
			w.acc.Port = 993
		} else {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				m.setError(fmt.Errorf("端口不合法"))
				return m.startPrompt(promptMail, "添加邮箱 · IMAP 端口", "默认 993，直接回车")
			}
			w.acc.Port = n
		}
		w.step++
		_, cmd := m.startPrompt(promptMail, "添加邮箱 · 密码/授权码", "Gmail 用应用专用密码，QQ/163 用授权码")
		m.input.EchoMode = textinput.EchoPassword
		return m, cmd

	case 4: // 密码
		if value == "" {
			m.setError(fmt.Errorf("密码不能为空"))
			_, cmd := m.startPrompt(promptMail, "添加邮箱 · 密码/授权码", "Gmail 用应用专用密码，QQ/163 用授权码")
			m.input.EchoMode = textinput.EchoPassword
			return m, cmd
		}
		return m.finishMailWizard(value)
	}
	return m, nil
}

// finishMailWizard 落盘账号、写入钥匙串并刷新会话。
func (m *Model) finishMailWizard(password string) (tea.Model, tea.Cmd) {
	acc := m.mailWizard.acc
	acc.Normalize()
	if err := config.KeychainSet("mail:"+acc.Name, acc.User, password); err != nil {
		m.setError(err)
		m.mailWizard = nil
		return m, nil
	}

	m.cfg.Accounts = append(m.cfg.Accounts, acc)
	m.mailS.SetConfig(m.cfg)
	m.skills = m.buildSkills()
	if m.agent != nil {
		m.agent.SetSkills(m.skills)
	}
	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setError(fmt.Errorf("配置保存失败：%w", err))
	}
	m.mailWizard = nil
	m.rebuild()
	m.chatNote(fmt.Sprintf("已添加邮箱 %s（%s）。按 ctrl+r 同步邮件，或直接让我整理。", acc.Name, acc.User))
	if m.view == viewChat {
		return m, m.focusChat()
	}
	return m, nil
}
