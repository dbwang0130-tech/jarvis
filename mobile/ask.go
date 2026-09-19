package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// iOS 端只带日程和记账两项技能：它们改的都是本地文件、都可逆，
// 全部标着 RiskNone，所以不会冒出「要不要执行」的确认框——手机上
// 弹那个东西很别扭，而且要把半途的状态存下来等用户回话。
// 邮件和浏览器留在桌面端。

// AskInput 是 Ask 的入参。
type AskInput struct {
	// Files 是 org 文件，键是文件名，值是全文。
	Files map[string]string `json:"files"`
	// Ledger 是账本全文。
	Ledger string `json:"ledger"`
	// Question 是用户这次说的话。
	Question string `json:"question"`
	// Images 是这一轮随话一起发的图片（data URL）。
	//
	// **只跟这一轮走，不进历史。** 图片按 token 算贵得多，留在历史里
	// 每轮都重发一遍，几轮下来就把上下文撑爆了、钱也白花。人要是还想
	// 问这张图，再发一次就是。
	Images []string `json:"images"`
	// History 是之前几轮的对话，原样从上次的出参里拿回来。
	History []agent.Message `json:"history"`
	// TZ 是设备的时区名，见 SetTimeZone。
	TZ string `json:"tz"`
	// Memory 是长期记忆全文（iCloud 里的 memory.md），没有就留空。
	Memory string `json:"memory"`
	// Health 是健康记录全文（iCloud 里的 health.ndjson，一行一条 FHIR 资源）。
	Health string `json:"health"`
	// Accounts 是邮箱账号，空的话这一轮就没有邮件能力。
	Accounts []MailAccount `json:"accounts"`
	// Model 是模型接入参数。
	Model ModelConfig `json:"model"`
	// Profile 是用户自述，交给模型判断什么算要紧。
	Profile string `json:"profile"`
	// Now 可以指定「现在」，留空就是真的现在（测试用）。
	Now string `json:"now"`
}

// ModelConfig 是模型接入参数。密钥由 Swift 那边从钥匙串取出来传进来，
// Go 这头不落盘。
type ModelConfig struct {
	Provider    string  `json:"provider"`
	Model       string  `json:"model"`
	BaseURL     string  `json:"base_url"`
	APIKey      string  `json:"api_key"`
	Temperature float64 `json:"temperature"`
	MaxSteps    int     `json:"max_steps"`
	TimeoutSec  int     `json:"timeout_sec"`
}

// AskOutput 是 Ask 的出参。
type AskOutput struct {
	// Reply 是秘书的答复。
	Reply string `json:"reply"`
	// Files 只装改过的那几个文件，键是文件名，值是新的全文。
	// Swift 那边照着这个写回 iCloud。
	Files map[string]string `json:"files,omitempty"`
	// Ledger 只在账本被改过时才有。
	Ledger string `json:"ledger,omitempty"`
	// History 是这一轮之后的完整对话，下次原样传回来。
	History []agent.Message `json:"history"`
	// Steps 是干活过程（调了什么工具、结果如何），界面可以显示出来。
	Steps []Step `json:"steps,omitempty"`
	// Memory 只在秘书改过记忆时才有，界面拿去写回 iCloud。
	Memory string `json:"memory,omitempty"`
	// Health 只在健康记录被追加过时才有。
	Health string `json:"health,omitempty"`
	// Stopped 表示这一轮是被叫停的，不是自己跑完的。
	Stopped bool `json:"stopped,omitempty"`
	// Error 非空表示这一轮没办成。
	Error string `json:"error,omitempty"`
}

// Step 是干活过程里的一条。
type Step struct {
	Kind string `json:"kind"` // tool / result / error
	Name string `json:"name"` // 工具名
	// Label 是工具名的中文说法，界面直接显示这个。
	//
	// 在这边查好再过桥，不让 Swift 自己维护一张对照表——那张表原先确实
	// 存在，而且已经跟现实脱节了：留着早就删掉的工具、缺着新加的工具，
	// 两边谁都不知道对方改了什么。
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Ask 把一句话交给秘书处理，返回答复和要写回的文件。
//
// 文件仍然不由 Go 直接碰 iCloud：内容从入参进来，落到一个临时目录里
// 让现成的工具去改，改完读回来对比，只把动过的那几个交出去。临时目录
// 在 app 自己的沙箱里，没有权限问题。
// Ask 不带回调：进度攒到最后一起给，要确认的一律不批。
func Ask(inputJSON string) string { return askWith(inputJSON, nil) }

// AskLive 带界面回调：干活过程实时报上去，要拿主意的事回头问。
// host 那边 Confirm 会一直阻塞到用户点了为止，所以别在主线程上调这个。
func AskLive(inputJSON string, host Host) string { return askWith(inputJSON, host) }

// 正在跑的那一轮的取消开关。手机上一轮可能要跑一分钟，中途改主意了
// 总得有地方喊停——桌面端按 esc，这边只能从界面喊进来。
var (
	cancelMu sync.Mutex
	cancelFn context.CancelFunc
)

// Cancel 叫停正在跑的那一轮。已经做完的改动会照常交回来，不会白干。
func Cancel() {
	cancelMu.Lock()
	fn := cancelFn
	cancelMu.Unlock()
	if fn != nil {
		fn()
	}
}

func startCancelable() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancelMu.Lock()
	if cancelFn != nil {
		cancelFn() // 上一轮还挂着就先收掉，免得越积越多
	}
	cancelFn = cancel
	cancelMu.Unlock()
	return ctx
}

func endCancelable() {
	cancelMu.Lock()
	cancelFn = nil
	cancelMu.Unlock()
}

func askWith(inputJSON string, host Host) string {
	var in AskInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return askError("入参不是合法 JSON：%v", err)
	}
	SetTimeZone(in.TZ)

	if strings.TrimSpace(in.Question) == "" {
		return askError("没说要干什么")
	}
	if in.Model.Provider == "" {
		return askError("还没设模型服务商")
	}

	wd, err := newWorkdir(in.Files, "jarvis-ask-")
	if err != nil {
		return askError("%v", err)
	}
	defer wd.close()

	orgDir := wd.orgDir
	ledgerPath := wd.path("ledger.dat")
	if err := wd.write("ledger.dat", in.Ledger); err != nil {
		return askError("%v", err)
	}
	memoryPath := wd.path("memory.md")
	if err := wd.write("memory.md", in.Memory); err != nil {
		return askError("%v", err)
	}
	healthPath := wd.path("health.ndjson")
	if err := wd.write("health.ndjson", in.Health); err != nil {
		return askError("%v", err)
	}

	cfg := mobileAgentConfig(in.Model, in.Profile)
	cfg.Files = []string{orgDir}
	cfg.Inbox = wd.inboxPath()
	cfg.LedgerFile = ledgerPath
	cfg.HealthFile = healthPath

	coll, err := org.Load(orgDir)
	if err != nil {
		return askError("日程读不出来：%v", err)
	}
	ws := orgtools.NewWorkspace(coll, cfg)
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			return askError("now 得是 RFC3339 格式：%v", err)
		}
		ws.Now = func() time.Time { return t }
	}

	// 记忆跟着一起带上——两端共用 iCloud 里那一份，电脑上记住的事手机上
	// 也该记得，反过来也是。
	skills := []agent.Skill{
		orgtools.NewSkill(ws),
		ledger.NewSkill(ledgerPath),
		health.New(healthPath),
		memory.New(memoryPath),
	}
	// 配了邮箱才带上邮件能力。会话跨轮次留着，不然上一轮的编号和草稿全没了。
	// 发信这类收不回来的动作靠 Host.Confirm 问用户，不用把工具摘掉。
	if ms := mailSessionFor(in.Accounts); ms != nil {
		skills = append(skills, mail.NewSkill(ms))
	}
	ag, err := agent.Build(cfg, skills...)
	if err != nil {
		return askError("%v", err)
	}
	ag.Stream = false // 手机上一次性拿结果，不做流式
	ag.AutoCompact = true
	ag.Auto = agent.AutoClean // 本地文件改动都可逆，不用逐项点头
	ag.History = in.History

	var steps []Step
	ctx := startCancelable()
	reply, err := ag.AskWith(ctx, in.Question, in.Images, hostEmit(host, &steps))
	endCancelable()

	// 被叫停时**不能当成失败**：工具已经改过的文件就在临时目录里，
	// 直接返回错误的话那些改动就丢了，用户看到的是「什么都没发生」，
	// 而实际上一半的事已经做完了。
	stopped := errors.Is(err, context.Canceled)
	if err != nil && !stopped {
		return askError("%v", err)
	}
	if stopped && strings.TrimSpace(reply) == "" {
		reply = "停下了。已经做完的那部分保留着。"
	}

	out := AskOutput{Reply: reply, History: ag.History, Steps: steps, Stopped: stopped}
	out.Files, err = wd.changed()
	if err != nil {
		return askError("读回改动失败：%v", err)
	}
	if raw, err := os.ReadFile(ledgerPath); err == nil && string(raw) != in.Ledger {
		out.Ledger = string(raw)
	}
	if raw, err := os.ReadFile(memoryPath); err == nil && string(raw) != in.Memory {
		out.Memory = string(raw)
	}
	if raw, err := os.ReadFile(healthPath); err == nil && string(raw) != in.Health {
		out.Health = string(raw)
	}
	return marshal(&out)
}

func (o *AskOutput) normalize() {
	if o.History == nil {
		o.History = []agent.Message{}
	}
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func askError(format string, args ...any) string {
	return marshal(&AskOutput{Error: fmt.Sprintf(format, args...)})
}

// mobileAgentConfig 拼出手机上跑 agent 要的那份配置。
//
// 提出来是因为对话、邮件分拣、模型自检都要同一份——各写一遍的话，
// 「手机上别读环境变量代理」这类决定迟早只在其中一处生效。
func mobileAgentConfig(m ModelConfig, profile string) config.Config {
	return config.Config{
		WorkHours:  config.WorkHours{Start: 9, End: 22},
		AgendaDays: 7,
		Profile:    config.Profile{Interests: profile},
		Agent: config.Agent{
			Provider:    m.Provider,
			Model:       m.Model,
			BaseURL:     m.BaseURL,
			APIKey:      m.APIKey,
			Temperature: m.Temperature,
			MaxSteps:    orDefault(m.MaxSteps, 12),
			TimeoutSec:  orDefault(m.TimeoutSec, 60),
			Proxy:       "none", // 手机上别去读环境变量里的代理
		},
	}
}
