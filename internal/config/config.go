// Package config 负责 jarvis 的配置文件读写。
//
// jarvis 把日程（org-mode）与邮件（IMAP/SMTP）两个秘书合并成一个，
// 因此配置也合并成一份 JSON：org 文件、模型接入、邮箱账号都在里面。
// 密码与 API key 不进明文，优先走 macOS 钥匙串。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Agent 是秘书使用的模型配置。
type Agent struct {
	// Provider 是预设名，见 ai.Presets；留空表示未启用秘书。
	Provider string `json:"provider"`
	// Model 覆盖预设中的模型名。
	Model string `json:"model,omitempty"`
	// BaseURL 覆盖预设中的接口地址。
	BaseURL string `json:"base_url,omitempty"`
	// APIKeyEnv 覆盖读取密钥的环境变量名。
	APIKeyEnv string `json:"api_key_env,omitempty"`
	// APIKey 是直接保存在配置里的密钥（/login 写入），优先于环境变量。
	APIKey string `json:"api_key,omitempty"`
	// APIKeyCmd 从命令输出取密钥（1Password CLI、pass 等），优先于上面两项。
	APIKeyCmd string `json:"api_key_cmd,omitempty"`
	// Temperature 为 0 时使用预设默认值。
	Temperature float64 `json:"temperature,omitempty"`
	// MaxSteps 限制一次对话中最多执行多少轮工具调用。
	MaxSteps int `json:"max_steps,omitempty"`
	// AutoApprove 为真时秘书对日程的修改直接落盘（邮件动作仍受 Behavior 约束）。
	AutoApprove bool `json:"auto_approve"`
	// AutoCompact 为真时上下文接近上限会自动压缩成摘要，默认开启。
	AutoCompact bool `json:"auto_compact"`
	// ContextLimit 是触发压缩的估算 token 数，零值用内置默认。
	ContextLimit int `json:"context_limit,omitempty"`
	// TimeoutSec 是模型请求超时秒数，零值用默认 120。
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// Proxy 控制怎么连模型接口：留空跟随环境变量，"none" 强制直连，
	// 其他值当作代理地址（如 http://127.0.0.1:7890）。
	Proxy string `json:"proxy,omitempty"`
}

// WorkHours 是一天中可安排事务的时段。
type WorkHours struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Profile 描述用户本人，交给模型判断什么算「要紧」。
type Profile struct {
	Interests string `json:"interests"`
}

// Behavior 控制秘书对邮件能自己做多少主。
type Behavior struct {
	// AutoConfirm: "off" 逐项确认；"clean" 整理自动、发信仍确认；"all" 全自动。
	AutoConfirm string `json:"auto_confirm"`
}

// MailAccount 是一个 IMAP 邮箱账号，同时包含发信所需的 SMTP 信息。
type MailAccount struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	PasswordCmd string `json:"password_cmd,omitempty"`
	Junk        string `json:"junk,omitempty"`  // 垃圾箱路径；留空则按 \Junk 标记自动探测
	Limit       int    `json:"limit,omitempty"` // 单次最多拉取多少封
	Days        int    `json:"days,omitempty"`  // 只看最近几天
	// FirstSyncLimit 是首次同步（没有进度记录时）最多拉多少封，0 表示不限。
	FirstSyncLimit *int `json:"first_sync_limit,omitempty"`
	Disabled       bool `json:"disabled,omitempty"`
	// MarkRead 决定秘书判断过的邮件要不要在服务器上标成已读。
	MarkRead *bool `json:"mark_read,omitempty"`

	// 发信。留空则从 IMAP 主机自动推导（imap.x.com -> smtp.x.com:465）。
	SMTPHost  string `json:"smtp_host,omitempty"`
	SMTPPort  int    `json:"smtp_port,omitempty"`
	SMTPUser  string `json:"smtp_user,omitempty"` // 留空则复用 user
	From      string `json:"from,omitempty"`      // 发信显示名，如 "王大宝"
	Signature string `json:"signature,omitempty"` // 追加到草稿末尾的签名
}

// Config 是 jarvis 的全部配置。
type Config struct {
	// 日程（org-mode）
	Files            []string  `json:"files"`
	Inbox            string    `json:"inbox,omitempty"`
	DeadlineWarnDays int       `json:"deadline_warn_days"`
	AgendaDays       int       `json:"agenda_days"`
	LogStateChanges  bool      `json:"log_state_changes"`
	WorkHours        WorkHours `json:"work_hours"`

	// 模型接入
	Agent Agent `json:"agent"`

	// 记账（ledger-cli 纯文本格式）
	LedgerFile string `json:"ledger_file,omitempty"`

	// 邮件
	Profile  Profile       `json:"profile"`
	Behavior Behavior      `json:"behavior"`
	Accounts []MailAccount `json:"accounts"`
}

// Default 返回默认配置。
func Default() Config {
	return Config{
		Files:            []string{"~/org"},
		DeadlineWarnDays: 14,
		AgendaDays:       7,
		LogStateChanges:  true,
		WorkHours:        WorkHours{Start: 9, End: 22},
		Agent: Agent{
			Provider:    "deepseek",
			MaxSteps:    20,
			Temperature: 0.3,
			AutoCompact: true,
			TimeoutSec:  120,
		},
		LedgerFile: "~/finance/ledger.dat",
		Behavior:   Behavior{AutoConfirm: "clean"},
	}
}

// Path 返回配置文件路径：$JARVIS_CONFIG，否则 ~/.config/jarvis/config.json。
func Path() string {
	if p := os.Getenv("JARVIS_CONFIG"); p != "" {
		return Expand(p)
	}
	return filepath.Join(ConfigDir(), "jarvis", "config.json")
}

// ConfigDir 返回配置根目录：$XDG_CONFIG_HOME，否则 ~/.config。
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// Dir 返回 jarvis 的配置目录。
func Dir() string { return filepath.Dir(Path()) }

// Expand 展开路径里的 ~ 前缀。
func Expand(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// Load 读取配置文件；文件不存在时返回默认配置。
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	cfg.fill()
	return cfg, nil
}

// fill 用默认值补齐缺失的字段。
func (c *Config) fill() {
	d := Default()
	if len(c.Files) == 0 {
		c.Files = d.Files
	}
	if c.DeadlineWarnDays == 0 {
		c.DeadlineWarnDays = d.DeadlineWarnDays
	}
	if c.AgendaDays == 0 {
		c.AgendaDays = d.AgendaDays
	}
	if c.WorkHours.End == 0 {
		c.WorkHours = d.WorkHours
	}
	if c.Agent.MaxSteps == 0 {
		c.Agent.MaxSteps = d.Agent.MaxSteps
	}
	if c.Agent.TimeoutSec == 0 {
		c.Agent.TimeoutSec = d.Agent.TimeoutSec
	}
	if c.Behavior.AutoConfirm == "" {
		c.Behavior.AutoConfirm = d.Behavior.AutoConfirm
	}
	if c.LedgerFile == "" {
		c.LedgerFile = d.LedgerFile
	}
	for i := range c.Accounts {
		c.Accounts[i].fill()
	}
}

// Normalize 补齐缺省字段，供界面里手动添加账号后调用。
func (a *MailAccount) Normalize() { a.fill() }

func (a *MailAccount) fill() {
	if a.Port == 0 {
		a.Port = 993
	}
	if a.Limit == 0 {
		a.Limit = 50
	}
	if a.Days == 0 {
		a.Days = 7
	}
	if a.SMTPHost == "" {
		a.SMTPHost = deriveSMTPHost(a.Host)
	}
	if a.SMTPPort == 0 {
		a.SMTPPort = 465
	}
	if a.SMTPUser == "" {
		a.SMTPUser = a.User
	}
	if a.MarkRead == nil {
		on := true
		a.MarkRead = &on
	}
	if a.FirstSyncLimit == nil {
		n := 200
		a.FirstSyncLimit = &n
	}
}

// Save 把配置写入 path，必要时创建目录。
func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// 配置里可能存有 API 密钥，只允许本人读写。
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// InboxFile 返回新建条目应写入的文件路径。
func (c Config) InboxFile() string {
	if c.Inbox != "" {
		return Expand(c.Inbox)
	}
	if len(c.Files) > 0 {
		return Expand(c.Files[0])
	}
	return ""
}

// Enabled 返回未被禁用的邮箱账号。
func (c Config) Enabled() []MailAccount {
	var out []MailAccount
	for _, a := range c.Accounts {
		if !a.Disabled {
			out = append(out, a)
		}
	}
	return out
}

// Account 按名字取账号配置。
func (c Config) Account(name string) (MailAccount, error) {
	for _, a := range c.Enabled() {
		if a.Name == name {
			return a, nil
		}
	}
	return MailAccount{}, fmt.Errorf("没有名为 %q 的邮箱账号", name)
}

// Addr 是 IMAP 的连接地址。
func (a MailAccount) Addr() string { return fmt.Sprintf("%s:%d", a.Host, a.Port) }

// SMTPAddr 是发信服务器地址。
func (a MailAccount) SMTPAddr() string { return fmt.Sprintf("%s:%d", a.SMTPHost, a.SMTPPort) }

// MarksRead 说明这个账号要不要把判断过的邮件标成已读。
func (a MailAccount) MarksRead() bool { return a.MarkRead == nil || *a.MarkRead }

// FirstSync 返回首次同步的条数上限，0 表示不限。
func (a MailAccount) FirstSync() int {
	if a.FirstSyncLimit == nil {
		return 200
	}
	return *a.FirstSyncLimit
}

// Secret 按优先级取邮箱密码：password_cmd > macOS 钥匙串。
func (a MailAccount) Secret() (string, error) {
	if a.PasswordCmd != "" {
		return RunCmd(a.PasswordCmd)
	}
	pw, err := KeychainGet("mail:"+a.Name, a.User)
	if err != nil {
		return "", fmt.Errorf("账号 %s 没有可用密码：%w", a.Name, err)
	}
	return pw, nil
}

// deriveSMTPHost 把 imap.example.com 推成 smtp.example.com。
func deriveSMTPHost(imapHost string) string {
	if strings.HasPrefix(imapHost, "imap.") {
		return "smtp." + strings.TrimPrefix(imapHost, "imap.")
	}
	return imapHost
}

// RunCmd 执行一行 shell 命令并返回去掉首尾换行的输出。
func RunCmd(line string) (string, error) {
	out, err := exec.Command("sh", "-c", line).Output()
	if err != nil {
		return "", fmt.Errorf("执行 %q 失败: %w", line, err)
	}
	s := strings.TrimRight(string(out), "\r\n")
	if s == "" {
		return "", fmt.Errorf("命令 %q 没有输出", line)
	}
	return s, nil
}

// KeychainGet 从 macOS 登录钥匙串读取一条通用密码。
func KeychainGet(service, account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", "jarvis:"+service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("钥匙串中没有 %s/%s", service, account)
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}

// KeychainSet 写入（或覆盖）登录钥匙串里的一条通用密码。
func KeychainSet(service, account, secret string) error {
	cmd := exec.Command("security", "add-generic-password",
		"-s", "jarvis:"+service, "-a", account, "-w", secret, "-U",
		"-l", "jarvis:"+service, "-D", "jarvis credential")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("写入钥匙串失败: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
