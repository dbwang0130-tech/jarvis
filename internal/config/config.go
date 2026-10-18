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
	// ContextLimit 是**一次请求**的估算 token 预算：系统提示 + 工具声明 +
	// 对话历史，三样加起来。零值用内置默认。
	//
	// 这个口径是改过的——早先它只管对话历史，固定开销一分不算。老配置里
	// 那个量级的数放到新口径下装不下，agent 那边会当没设过（见 contextLimit）。
	ContextLimit int `json:"context_limit,omitempty"`
	// TimeoutSec 是模型请求超时秒数，零值用默认 120。
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// Proxy 控制怎么连模型接口：留空跟随环境变量，"none" 强制直连，
	// 其他值当作代理地址（如 http://127.0.0.1:7890）。
	Proxy string `json:"proxy,omitempty"`
	// Profiles 是存着的几套模型，`/model` 在它们之间切。
	//
	// **当前在用的那套仍然摊在上面 Provider/Model/BaseURL/APIKey 几个字段
	// 里**，切换就是把某个 profile 的内容拷上来。这样 agent.Build 一行都
	// 不用改，老配置（没有 profiles 那一段）也照常能跑——多模型是加在
	// 配置层的一份备选清单，不是把「当前用哪个」这件事重新发明一遍。
	Profiles []ModelProfile `json:"profiles,omitempty"`
}

// ModelProfile 是存下来的一套模型接入参数。
//
// 名字是用户自己起的（「豆包看图」「本地小模型」），因为同一个服务商
// 底下可能存好几套——同是 doubao，一套走 turbo 干活、一套走 lite 省钱。
// 用服务商名当主键的话这两套就只能留一套。
type ModelProfile struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
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
	Files []string `json:"files"`
	Inbox string   `json:"inbox,omitempty"`
	// MemoryFile 是长期记忆放哪儿。空的话放配置目录，见 MemoryPath。
	MemoryFile string `json:"memory_file,omitempty"`
	// PersonaFile 是 Jarvis 自己的人设放哪儿。空的话跟着记忆走，
	// 见 PersonaPath。
	PersonaFile string `json:"persona_file,omitempty"`
	// SkillsDir 是用户扩展技能的目录（一个子目录一份 SKILL.md）。
	// 空的话跟着人设走，见 SkillsPath。
	SkillsDir        string    `json:"skills_dir,omitempty"`
	DeadlineWarnDays int       `json:"deadline_warn_days"`
	AgendaDays       int       `json:"agenda_days"`
	LogStateChanges  bool      `json:"log_state_changes"`
	WorkHours        WorkHours `json:"work_hours"`

	// 模型接入
	Agent Agent `json:"agent"`

	// 记账（ledger-cli 纯文本格式）
	LedgerFile string `json:"ledger_file,omitempty"`

	// 健康记录（HL7 FHIR，一行一条资源的 NDJSON）
	HealthFile string `json:"health_file,omitempty"`

	// Theme 是界面主题：dark / light / auto。
	Theme string `json:"theme,omitempty"`

	// Mouse 决定要不要收鼠标上报。开着滚轮才能翻历史，代价是终端
	// 原生的拖选复制会被拦掉（按住 option/shift 拖仍然能选）。
	// 关掉就换回拖选，翻历史改用 PageUp/PageDown。零值视为开。
	Mouse *bool `json:"mouse,omitempty"`

	// 邮件
	Profile  Profile       `json:"profile"`
	Behavior Behavior      `json:"behavior"`
	Accounts []MailAccount `json:"accounts"`
	// MailCheckTimes 是每天什么时候自动收一次信，形如 ["08:30", "12:00"]。
	//
	// 空着就不自动收。这件事只有装了后台提醒（launchd 每五分钟跑一次
	// `jarvis remind`）才会发生——手机上办不到，其他客户端 不让 app 在后台按点
	// 醒来连网。
	//
	// 为什么要能设好几个：收信这事的节奏因人而异，有人早上看一次就够，
	// 有人上午下午各一次。定成固定间隔（比如「每两小时」）的话，半夜
	// 也在收，白白吵醒你的电脑。
	MailCheckTimes []string `json:"mail_check_times,omitempty"`
}

// Default 返回默认配置。
// ICloudDir 返回 iCloud Drive 里的 Jarvis 目录，没有 iCloud Drive 就返回空串。
//
// 这是 macOS 上 iCloud Drive 的固定位置。其他客户端那份由用户亲手选目录拿到
// （沙箱不让猜），两边指的是同一个文件夹。
func ICloudDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, "Library", "Mobile Documents", "com~apple~CloudDocs")
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return "" // 没开 iCloud Drive
	}
	return filepath.Join(root, "Jarvis")
}

// Default 是没有配置文件时的那份配置。
//
// 有 iCloud Drive 就默认放在里面——日程、账本、记忆都该跟手机上是同一份，
// 这是默认值该有的样子，不该逼人先去设置一遍。没有就退回本地目录。
func Default() Config {
	c := defaultLocal()
	if icloud := ICloudDir(); icloud != "" {
		c.Files = []string{filepath.Join(icloud, "org")}
		c.LedgerFile = filepath.Join(icloud, "ledger.dat")
		c.MemoryFile = filepath.Join(icloud, "memory.md")
		c.PersonaFile = filepath.Join(icloud, "persona.md")
		c.SkillsDir = filepath.Join(icloud, "skills")
		c.HealthFile = filepath.Join(icloud, "health.ndjson")
	}
	return c
}

func defaultLocal() Config {
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
		LedgerFile:  "~/finance/ledger.dat",
		HealthFile:  "~/health/health.ndjson",
		MemoryFile:  "", // 空表示放配置目录，见 MemoryPath
		PersonaFile: "", // 同上，见 PersonaPath
		SkillsDir:   "", // 同上，见 SkillsPath
		Theme:       "auto",
		Behavior:    Behavior{AutoConfirm: "clean"},
	}
}

// Path 返回配置文件路径：$JARVIS_CONFIG > ~/.config/jarvis/config.json。
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
func Load(path string) (cfg Config, err error) {
	cfg = Default()
	// 不管成不成，返回的都是一份能直接用的配置：读不出来就是默认值。
	// 配置坏了不该让人连界面都进不去——进去了才好修。
	defer cfg.normalize()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		cfg = Default() // 半解析的内容不要，退回默认
		return cfg, fmt.Errorf("解析配置 %s 失败: %w", path, err)
	}
	cfg.fill()
	return cfg, nil
}

// normalize 把配置里的路径一次性展开成绝对路径。
//
// 这些路径会被直接拿去建文件建目录，`~` 没展开就会在当前工作目录下
// 造出一个名叫 ~ 的目录。展开责任放在加载这一处，调用方一律假定
// 拿到的就是绝对路径，不必各自记得调 Expand。
func (c *Config) normalize() {
	for i, f := range c.Files {
		c.Files[i] = Expand(f)
	}
	c.Inbox = Expand(c.Inbox)
	c.MemoryFile = Expand(c.MemoryFile)
	c.PersonaFile = Expand(c.PersonaFile)
	c.SkillsDir = Expand(c.SkillsDir)
	c.LedgerFile = Expand(c.LedgerFile)
	c.HealthFile = Expand(c.HealthFile)
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
	if c.HealthFile == "" {
		c.HealthFile = d.HealthFile
	}
	if c.Theme == "" {
		c.Theme = d.Theme
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
	return WriteAtomic(path, append(data, '\n'), 0o600)
}

// MouseOn 报告要不要收鼠标上报，没配过就是开着。
func (c Config) MouseOn() bool { return c.Mouse == nil || *c.Mouse }

// icloudOr 是那几份共用文件的落点规则：配置里指了就用指的，否则跟着 iCloud 走，
// iCloud 也没有才退回配置目录。
//
// 记忆、人设、扩展技能三处原先各写一遍这个三段式。加第四份共用文件时照着抄，
// 抄漏一个分支就会在当前工作目录下造出东西来——跟 normalize() 那条教训同一类。
func (c Config) icloudOr(override, name string) string {
	if override != "" {
		return override
	}
	if icloud := ICloudDir(); icloud != "" {
		return filepath.Join(icloud, name)
	}
	return filepath.Join(Dir(), name)
}

// MemoryPath 返回长期记忆文件的路径。
//
// 没配就跟着日程走——日程在 iCloud 里，记忆也该在，否则手机上那位秘书
// 记不得电脑上这位刚记住的事。日程也没配才退回配置目录。
func (c Config) MemoryPath() string { return c.icloudOr(c.MemoryFile, "memory.md") }

// PersonaPath 返回人设文件的路径。
//
// 跟记忆同一个道理，也放 iCloud：人设是「秘书是谁」，手机上那位和电脑上
// 这位必须是同一个人——两边各存一份的话，用户在手机上说「以后别叫我先生」，
// 回到电脑上它照旧叫，而这种不一致看起来像模型记性差，不像配置问题。
func (c Config) PersonaPath() string { return c.icloudOr(c.PersonaFile, "persona.md") }

// SkillsPath 返回扩展技能目录。
//
// 跟人设、记忆同一个地方：用户在手机上放进去的技能，电脑上这位也该会。
func (c Config) SkillsPath() string { return c.icloudOr(c.SkillsDir, "skills") }

// MigrateMemory 把老位置（配置目录）的记忆挪到现在该在的地方。
//
// 记忆本来只在电脑上，放配置目录没问题；现在两端共用，得放 iCloud。
// 老用户升上来时那份记忆还在旧地方，不搬的话秘书就把之前记住的全忘了。
//
// **复制，不删原件**：原件留着当备份，出岔子还能翻回去。
// 目标已经有了就什么都不做——这条要能重复跑而不出事。
// 返回搬没搬、出没出错。
func MigrateMemory(c Config) (moved bool, err error) {
	dst := c.MemoryPath()
	legacy := filepath.Join(Dir(), "memory.md")
	if dst == legacy {
		return false, nil
	}
	if _, err := os.Stat(dst); err == nil {
		return false, nil // 已经有了，别覆盖
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		return false, nil // 老位置没有，那就是新用户
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	if err := os.WriteFile(dst, raw, 0o600); err != nil {
		return false, err
	}
	return true, nil
}

// InboxFile 返回新建条目应写入的文件路径。
func (c Config) InboxFile() string {
	if c.Inbox != "" {
		return c.Inbox
	}
	if len(c.Files) > 0 {
		return c.Files[0]
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

// WriteAtomic 把内容写进文件：先写同目录的临时文件再改名。
//
// 直接 WriteFile 的话，写到一半断电或者被系统杀掉，留下的是半截内容——
// 对 JSON 来说就是下次读不出来。改名在同一个文件系统上是原子的，
// 要么是旧的完整内容，要么是新的完整内容。
// WriteText 整份覆盖一个文本文件：建目录、去掉首尾空白、补一个结尾换行、原子写。
//
// **iCloud 里那几份共用文件都走这儿**（记忆、人设）。原先各写一遍，连「原子写：
// 整份覆盖，写到一半被杀掉就等于清空」那句注释都是同一句——于是权限位和「要不要
// 补换行」这类判据在两份文件上各记一份，往后只会改到一边。
//
// text 是空串就写一个空文件，不是删掉：调用方那边「空」有自己的含义
// （人设为空表示回到内置那套），删文件会让那个意思丢掉。
func WriteText(path, text string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text != "" {
		text += "\n"
	}
	return WriteAtomic(path, []byte(text), 0o600)
}

func WriteAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
