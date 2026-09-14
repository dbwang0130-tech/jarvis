package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/configskill"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/orgtools"
)

// loginResultMsg 是密钥校验的异步结果。
type loginResultMsg struct {
	model string
	err   error
}

// commands 是秘书面板里可用的斜杠命令。
var commands = [][2]string{
	{"/login [密钥]", "设置模型 API 密钥并保存到配置"},
	{"/provider <名称>", "切换模型服务商，如 deepseek、qwen、kimi、minimax"},
	{"/model <名称>", "切换模型，如 deepseek-chat"},
	{"/mail", "交互式添加一个邮箱账号"},
	{"/accounts", "列出已配置的邮箱账号"},
	{"/mailrm <名称>", "删除一个邮箱账号"},
	{"/ledger [路径]", "设置账本文件（ledger-cli 格式）"},
	{"/files <路径...>", "设置 org 文件或目录"},
	{"/inbox <路径>", "设置新建条目写入的文件"},
	{"/agenda [天数]", "看日程，例如 /agenda 30"},
	{"/todos [关键词]", "看待办，可带关键词过滤"},
	{"/clear", "清空与秘书的对话上下文"},
	{"/compact", "把之前的对话压缩成摘要，腾出上下文"},
	{"/help", "显示这份命令列表"},
	{"/quit", "退出（也可以按 ctrl+c）"},
}

func isCommand(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "/")
}

// buildSkills 按当前配置构造技能集合。
func (m *Model) buildSkills() []agent.Skill {
	var skills []agent.Skill
	if m.orgWS != nil {
		skills = append(skills, orgtools.NewSkill(m.orgWS))
	}
	if m.mailS != nil {
		skills = append(skills, mail.NewSkill(m.mailS))
	}
	if m.ledger != nil {
		skills = append(skills, m.ledger)
	} else {
		skills = append(skills, ledger.NewSkill(m.cfg.LedgerFile))
	}
	skills = append(skills, configskill.New(m.cfgPath))
	return skills
}

// runCommand 执行秘书面板里的斜杠命令。
func (m *Model) runCommand(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(strings.TrimSpace(input))
	name := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))

	switch name {
	case "/login":
		if arg == "" {
			return m.startPrompt(promptLogin, "粘贴 API 密钥", "输入后回车保存，esc 取消")
		}
		return m.applyLogin(arg)

	case "/provider":
		if arg == "" {
			m.chatNote("用法：/provider <名称>。可选：" + strings.Join(sortedPresets(), "、"))
			return m, nil
		}
		m.cfg.Agent.Provider = arg
		m.cfg.Agent.Model = ""
		m.cfg.Agent.APIKey = ""
		return m.reloadAgent(fmt.Sprintf("已切换到 %s", arg))

	case "/model":
		if arg == "" {
			m.chatNote("用法：/model <名称>")
			return m, nil
		}
		m.cfg.Agent.Model = arg
		return m.reloadAgent(fmt.Sprintf("已切换模型为 %s", arg))

	case "/mail":
		return m.startMailWizard()

	case "/accounts":
		if len(m.cfg.Enabled()) == 0 {
			m.chatNote("还没有邮箱账号，用 /mail 添加。")
			return m, nil
		}
		var b strings.Builder
		b.WriteString("已配置的邮箱账号：")
		for _, a := range m.cfg.Enabled() {
			fmt.Fprintf(&b, "\n  %s  %s（imap %s，smtp %s）", a.Name, a.User, a.Host, a.SMTPHost)
		}
		m.chatNote(b.String())
		return m, nil

	case "/mailrm":
		if arg == "" {
			m.chatNote("用法：/mailrm <账号名>")
			return m, nil
		}
		return m.removeMailAccount(arg)

	case "/ledger":
		return m.setLedgerFile(arg)

	case "/files":
		if arg == "" {
			m.chatNote("用法：/files <路径...>，例如 /files ~/org work.org")
			return m, nil
		}
		m.cfg.Files = fields[1:]
		return m.reloadOrg("已更新 org 文件")

	case "/inbox":
		if arg == "" {
			m.chatNote("用法：/inbox <路径>")
			return m, nil
		}
		m.cfg.Inbox = arg
		if err := m.cfg.Save(m.cfgPath); err != nil {
			m.setError(fmt.Errorf("配置保存失败：%w", err))
			return m, nil
		}
		m.setStatus("新建条目将写入 %s", arg)
		return m, nil

	case "/agenda":
		if n, err := strconv.Atoi(arg); err == nil && n > 0 {
			m.days = n
		}
		m.start = m.today()
		return m.switchView(viewAgenda)

	case "/todos":
		m.query = arg
		return m.switchView(viewTodos)

	case "/quit", "/exit":
		m.quitting = true
		if m.cancelAgent != nil {
			m.cancelAgent()
		}
		return m, tea.Quit

	case "/compact":
		return m.compactContext()

	case "/clear", "/reset":
		if m.agent != nil {
			m.agent.Reset()
		}
		m.chat = nil
		m.orgWS.ResetChanges()
		m.setStatus("已清空对话上下文")
		return m, nil

	case "/help":
		m.chatNote(m.commandHelp())
		return m, nil
	}

	m.chatNote(fmt.Sprintf("没有 %s 这个命令。\n%s", name, m.commandHelp()))
	return m, nil
}

func (m *Model) commandHelp() string {
	var b strings.Builder
	b.WriteString("可用命令：")
	for _, c := range commands {
		b.WriteString(fmt.Sprintf("\n  %s  %s", padTo(c[0], 18), c[1]))
	}
	return b.String()
}

func sortedPresets() []string {
	names := agent.PresetNames()
	sort.Strings(names)
	return names
}

func (m *Model) chatNote(text string) {
	m.chat = append(m.chat, chatEntry{role: "note", text: text})
}

// reloadOrg 按当前配置重新加载 org 文件并保存配置。
func (m *Model) reloadOrg(note string) (tea.Model, tea.Cmd) {
	coll, err := LoadOrg(m.cfg)
	if err != nil {
		m.setError(err)
		return m, nil
	}
	m.coll = coll
	m.coll.SetLogStates(m.cfg.LogStateChanges)
	m.orgWS.Coll = coll
	m.orgWS.Cfg = m.cfg
	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setError(fmt.Errorf("配置保存失败：%w", err))
		return m, nil
	}
	m.rebuild()
	m.setStatus("%s，已写入 %s", note, m.cfgPath)
	return m, nil
}

// removeMailAccount 删除邮箱账号。
func (m *Model) removeMailAccount(name string) (tea.Model, tea.Cmd) {
	kept := m.cfg.Accounts[:0]
	found := false
	for _, a := range m.cfg.Accounts {
		if a.Name == name {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		m.setError(fmt.Errorf("没有名为 %q 的邮箱账号", name))
		return m, nil
	}
	m.cfg.Accounts = kept
	m.mailS.SetConfig(m.cfg)
	m.skills = m.buildSkills()
	if m.agent != nil {
		m.agent.SetSkills(m.skills)
	}
	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setError(fmt.Errorf("配置保存失败：%w", err))
		return m, nil
	}
	m.setStatus("已删除邮箱账号 %s", name)
	return m, nil
}

// setLedgerFile 设置或查看账本文件。
func (m *Model) setLedgerFile(arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if m.cfg.LedgerFile == "" {
			m.chatNote("还没有配置账本文件。用法：/ledger <路径>，例如 /ledger ~/finance/ledger.dat")
			return m, nil
		}
		m.chatNote("账本文件：" + m.cfg.LedgerFile + "。直接问我余额，或说「记一笔」即可。")
		return m, nil
	}

	path := config.Expand(arg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.setError(err)
		return m, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			m.setError(err)
			return m, nil
		}
	}

	m.cfg.LedgerFile = path
	m.ledger = ledger.NewSkill(path)
	m.skills = m.buildSkills()
	if m.agent != nil {
		m.agent.SetSkills(m.skills)
	}
	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setError(fmt.Errorf("配置保存失败：%w", err))
		return m, nil
	}
	m.chatNote("账本已设为 " + path + "。现在可以说「记一笔：午餐 50」试试。")
	return m, nil
}

// compactContext 手动把之前的对话压成摘要。
func (m *Model) compactContext() (tea.Model, tea.Cmd) {
	if m.agent == nil {
		m.chatNote("秘书还没配置，没有上下文可压缩。")
		return m, nil
	}
	if m.busy {
		m.setError(fmt.Errorf("秘书正忙，等它说完再压缩"))
		return m, nil
	}
	before, _ := m.agent.ContextUsage()
	ag := m.agent
	m.busy = true
	m.chatNote(fmt.Sprintf("正在压缩上下文（约 %d tokens）…", before))

	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		done, err := ag.Compact(ctx, 2)
		after, _ := ag.ContextUsage()
		return compactDoneMsg{before: before, after: after, done: done, err: err}
	})
}

// applyLogin 保存密钥、重建秘书，并异步校验密钥是否可用。
func (m *Model) applyLogin(key string) (tea.Model, tea.Cmd) {
	key = strings.TrimSpace(key)
	if key == "" {
		return m, nil
	}
	if m.cfg.Agent.Provider == "" {
		m.cfg.Agent.Provider = "deepseek"
		m.chatNote("未指定服务商，默认使用 deepseek。可用 /provider 切换。")
	}
	m.cfg.Agent.APIKey = key

	_, cmd := m.reloadAgent(fmt.Sprintf("已保存 %s 的密钥 %s", m.cfg.Agent.Provider, maskKey(key)))
	if m.agent == nil {
		return m, cmd
	}
	client := m.agent.Client
	m.verifying = true
	return m, tea.Batch(cmd, m.spinner.Tick, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return loginResultMsg{model: client.Model, err: client.Verify(ctx)}
	})
}

// reloadAgent 按当前配置重建秘书并写回配置文件。
func (m *Model) reloadAgent(note string) (tea.Model, tea.Cmd) {
	ag, err := agent.Build(m.cfg, m.buildSkills()...)
	m.agent, m.agentErr = ag, err
	m.skills = m.buildSkills()

	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setError(fmt.Errorf("配置保存失败：%w", err))
	} else {
		m.setStatus("%s，已写入 %s", note, m.cfgPath)
	}
	if m.agentErr != nil {
		m.chatNote("秘书仍不可用：" + m.agentErr.Error())
		return m, nil
	}
	m.chatNote(note + "。配置已保存到 " + m.cfgPath)
	return m, nil
}

// maskKey 只保留密钥首尾。
func maskKey(key string) string {
	runes := []rune(key)
	if len(runes) <= 8 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}
