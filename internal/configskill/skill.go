// Package configskill 让秘书能读写自己的配置文件，实现自我配置。
//
// 配置结构有变化时，同步更新这里的 read_config / set_config /
// remove_mail_account 三个工具，让秘书始终知道并能够改全部配置项。
package configskill

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
)

// Skill 是对 jarvis 配置文件的读写技能。
type Skill struct {
	Path string
}

// New 构造配置技能。
func New(path string) *Skill { return &Skill{Path: path} }

func (s *Skill) Name() string { return "配置" }

// Describe 展示当前关键配置，让模型知道现在的状态。
func (s *Skill) Describe(b *strings.Builder) {
	cfg, err := config.Load(s.Path)
	if err != nil {
		fmt.Fprintf(b, "配置文件：%s（读取失败：%v）\n", s.Path, err)
		return
	}
	fmt.Fprintf(b, "配置文件：%s\n", s.Path)
	fmt.Fprintf(b, "工作时段：%02d:00-%02d:00，日程提前提醒 %d 天，agenda 默认 %d 天\n",
		cfg.WorkHours.Start, cfg.WorkHours.End, cfg.DeadlineWarnDays, cfg.AgendaDays)
	fmt.Fprintf(b, "模型：%s / %s（自动档：%s）\n",
		cfg.Agent.Provider, cfg.Agent.Model, cfg.Behavior.AutoConfirm)
	fmt.Fprintf(b, "org 文件：%s\n", strings.Join(cfg.Files, "、"))
	if cfg.LedgerFile != "" {
		fmt.Fprintf(b, "账本：%s\n", cfg.LedgerFile)
	}
}

// Instructions 是配置技能的行为规则。
func (s *Skill) Instructions() string {
	return `配置管理：
1. 用户要调整设置时，用 read_config 先看现状，再用 set_config 修改，改完说明改了什么。
2. set_config 只传要改的字段，没传的字段保持不变。
3. 改完配置立即生效，无需重启。`
}

// Tools 返回配置读写工具。
func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{s.toolRead(), s.toolSet(), s.toolRemoveAccount()}
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

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any  { return map[string]any{"type": "number", "description": desc} }
func flag(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func arr(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

func (s *Skill) toolRead() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "read_config",
			Description: "读取当前配置文件内容。想了解或修改设置前先看这个。",
			Parameters:  obj(map[string]any{}),
		},
		run: func(string) (string, bool, error) {
			cfg, err := config.Load(s.Path)
			if err != nil {
				return "", false, err
			}
			// 密钥不进模型上下文，只留首尾。
			if cfg.Agent.APIKey != "" {
				cfg.Agent.APIKey = mask(cfg.Agent.APIKey)
			}
			raw, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return "", false, err
			}
			return string(raw), false, nil
		},
	}
}

func (s *Skill) toolSet() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "set_config",
			Description: "修改配置并立即生效。只传要改的字段，其余保持不变。",
			Parameters: obj(map[string]any{
				"work_hours_start":   num("每天可安排事务的起始小时，如 9"),
				"work_hours_end":     num("每天可安排事务的结束小时，如 22"),
				"deadline_warn_days": num("截止项提前多少天开始提醒"),
				"agenda_days":        num("日程视图默认跨度天数"),
				"log_state_changes":  flag("切换 TODO 状态时是否记录到 LOGBOOK"),
				"auto_confirm":       str("邮件自动档：off / clean / all"),
				"interests":          str("用户自述，用于判断什么算要紧"),
				"provider":           str("模型服务商，如 deepseek、qwen、kimi、minimax"),
				"model":              str("模型名"),
				"base_url":           str("OpenAI 兼容接口地址"),
				"temperature":        num("采样温度，如 0.3"),
				"max_steps":          num("一次提问最多执行多少轮工具调用"),
				"auto_compact":       flag("上下文接近上限时是否自动压缩"),
				"context_limit":      num("触发压缩的 token 阈值"),
				"proxy":              str("模型接口代理：none 表示直连，或填代理地址"),
				"ledger_file":        str("账本文件路径（ledger-cli 格式）"),
				"files":              arr("org 文件或目录列表"),
				"inbox":              str("新建条目默认写入的文件"),
			}),
		},
		run: func(raw string) (string, bool, error) {
			var p patch
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				return "", false, fmt.Errorf("参数不是合法 JSON: %w", err)
			}
			cfg, err := config.Load(s.Path)
			if err != nil {
				return "", false, err
			}
			p.apply(&cfg)

			if err := cfg.Save(s.Path); err != nil {
				return "", false, err
			}
			// 立即生效：界面会在收到本工具结果后重载配置。
			return "配置已更新并保存：" + p.summary(), true, nil
		},
	}
}

func (s *Skill) toolRemoveAccount() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "remove_mail_account",
			Description: "删除一个邮箱账号。用户说「删掉/移除/去掉某个邮箱」时用。",
			Parameters: obj(map[string]any{
				"name": str("要删除的账号名，如 gmail、qq"),
			}, "name"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			name := strings.TrimSpace(a.Name)
			if name == "" {
				return "", false, fmt.Errorf("必须提供账号名")
			}
			cfg, err := config.Load(s.Path)
			if err != nil {
				return "", false, err
			}
			kept := cfg.Accounts[:0]
			found := false
			for _, acc := range cfg.Accounts {
				if acc.Name == name {
					found = true
					continue
				}
				kept = append(kept, acc)
			}
			if !found {
				return "", false, fmt.Errorf("没有名为 %q 的邮箱账号", name)
			}
			cfg.Accounts = kept
			if err := cfg.Save(s.Path); err != nil {
				return "", false, err
			}
			return fmt.Sprintf("已删除邮箱账号 %s。", name), true, nil
		},
	}
}

// patch 是 set_config 可修改的字段，用指针区分「没传」和「传了零值」。
type patch struct {
	WorkHoursStart   *int      `json:"work_hours_start"`
	WorkHoursEnd     *int      `json:"work_hours_end"`
	DeadlineWarnDays *int      `json:"deadline_warn_days"`
	AgendaDays       *int      `json:"agenda_days"`
	LogStateChanges  *bool     `json:"log_state_changes"`
	AutoConfirm      *string   `json:"auto_confirm"`
	Interests        *string   `json:"interests"`
	Provider         *string   `json:"provider"`
	Model            *string   `json:"model"`
	BaseURL          *string   `json:"base_url"`
	Temperature      *float64  `json:"temperature"`
	MaxSteps         *int      `json:"max_steps"`
	AutoCompact      *bool     `json:"auto_compact"`
	ContextLimit     *int      `json:"context_limit"`
	Proxy            *string   `json:"proxy"`
	LedgerFile       *string   `json:"ledger_file"`
	Files            *[]string `json:"files"`
	Inbox            *string   `json:"inbox"`
}

func (p *patch) apply(cfg *config.Config) {
	if p.WorkHoursStart != nil {
		cfg.WorkHours.Start = *p.WorkHoursStart
	}
	if p.WorkHoursEnd != nil {
		cfg.WorkHours.End = *p.WorkHoursEnd
	}
	if p.DeadlineWarnDays != nil {
		cfg.DeadlineWarnDays = *p.DeadlineWarnDays
	}
	if p.AgendaDays != nil {
		cfg.AgendaDays = *p.AgendaDays
	}
	if p.LogStateChanges != nil {
		cfg.LogStateChanges = *p.LogStateChanges
	}
	if p.AutoConfirm != nil {
		cfg.Behavior.AutoConfirm = *p.AutoConfirm
	}
	if p.Interests != nil {
		cfg.Profile.Interests = *p.Interests
	}
	if p.Provider != nil {
		cfg.Agent.Provider = *p.Provider
	}
	if p.Model != nil {
		cfg.Agent.Model = *p.Model
	}
	if p.BaseURL != nil {
		cfg.Agent.BaseURL = *p.BaseURL
	}
	if p.Temperature != nil {
		cfg.Agent.Temperature = *p.Temperature
	}
	if p.MaxSteps != nil {
		cfg.Agent.MaxSteps = *p.MaxSteps
	}
	if p.AutoCompact != nil {
		cfg.Agent.AutoCompact = *p.AutoCompact
	}
	if p.ContextLimit != nil {
		cfg.Agent.ContextLimit = *p.ContextLimit
	}
	if p.Proxy != nil {
		cfg.Agent.Proxy = *p.Proxy
	}
	if p.LedgerFile != nil {
		cfg.LedgerFile = *p.LedgerFile
	}
	if p.Files != nil {
		cfg.Files = *p.Files
	}
	if p.Inbox != nil {
		cfg.Inbox = *p.Inbox
	}
}

func (p *patch) summary() string {
	names := []string{}
	if p.WorkHoursStart != nil || p.WorkHoursEnd != nil {
		names = append(names, "工作时间")
	}
	if p.DeadlineWarnDays != nil {
		names = append(names, "提醒天数")
	}
	if p.AgendaDays != nil {
		names = append(names, "agenda 天数")
	}
	if p.LogStateChanges != nil {
		names = append(names, "状态日志")
	}
	if p.AutoConfirm != nil {
		names = append(names, "自动档")
	}
	if p.Interests != nil {
		names = append(names, "用户自述")
	}
	if p.Provider != nil || p.Model != nil || p.BaseURL != nil || p.Temperature != nil ||
		p.MaxSteps != nil || p.AutoCompact != nil || p.ContextLimit != nil || p.Proxy != nil {
		names = append(names, "模型设置")
	}
	if p.LedgerFile != nil {
		names = append(names, "账本文件")
	}
	if p.Files != nil || p.Inbox != nil {
		names = append(names, "org 文件")
	}
	if len(names) == 0 {
		return "（没有字段被修改）"
	}
	return strings.Join(names, "、")
}

func mask(s string) string {
	r := []rune(s)
	if len(r) <= 8 {
		return strings.Repeat("*", len(r))
	}
	return string(r[:4]) + "…" + string(r[len(r)-4:])
}
