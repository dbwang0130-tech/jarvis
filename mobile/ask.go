package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
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
	// History 是之前几轮的对话，原样从上次的出参里拿回来。
	History []agent.Message `json:"history"`
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
	// Error 非空表示这一轮没办成。
	Error string `json:"error,omitempty"`
}

// Step 是干活过程里的一条。
type Step struct {
	Kind string `json:"kind"` // tool / result / error
	Name string `json:"name"` // 工具名
	Text string `json:"text"`
}

// Ask 把一句话交给秘书处理，返回答复和要写回的文件。
//
// 文件仍然不由 Go 直接碰 iCloud：内容从入参进来，落到一个临时目录里
// 让现成的工具去改，改完读回来对比，只把动过的那几个交出去。临时目录
// 在 app 自己的沙箱里，没有权限问题。
func Ask(inputJSON string) string {
	var in AskInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return askError("入参不是合法 JSON：%v", err)
	}
	if strings.TrimSpace(in.Question) == "" {
		return askError("没说要干什么")
	}
	if in.Model.Provider == "" {
		return askError("还没设模型服务商")
	}

	dir, err := os.MkdirTemp("", "jarvis-mobile-")
	if err != nil {
		return askError("建不了临时目录：%v", err)
	}
	defer os.RemoveAll(dir)

	orgDir := filepath.Join(dir, "org")
	if err := os.MkdirAll(orgDir, 0o700); err != nil {
		return askError("建不了临时目录：%v", err)
	}
	for name, content := range in.Files {
		// 只取文件名，不让 ../ 这类东西跑出临时目录。
		p := filepath.Join(orgDir, filepath.Base(name))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return askError("写临时文件失败：%v", err)
		}
	}
	ledgerPath := filepath.Join(dir, "ledger.dat")
	if err := os.WriteFile(ledgerPath, []byte(in.Ledger), 0o600); err != nil {
		return askError("写临时账本失败：%v", err)
	}

	cfg := config.Config{
		Files:      []string{orgDir},
		Inbox:      filepath.Join(orgDir, defaultInbox(in.Files)),
		LedgerFile: ledgerPath,
		WorkHours:  config.WorkHours{Start: 9, End: 22},
		AgendaDays: 7,
		Profile:    config.Profile{Interests: in.Profile},
		Agent: config.Agent{
			Provider:    in.Model.Provider,
			Model:       in.Model.Model,
			BaseURL:     in.Model.BaseURL,
			APIKey:      in.Model.APIKey,
			Temperature: in.Model.Temperature,
			MaxSteps:    orDefault(in.Model.MaxSteps, 12),
			TimeoutSec:  orDefault(in.Model.TimeoutSec, 60),
			Proxy:       "none", // 手机上别去读环境变量里的代理
		},
	}

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

	ag, err := agent.Build(cfg, orgtools.NewSkill(ws), ledger.NewSkill(ledgerPath))
	if err != nil {
		return askError("%v", err)
	}
	ag.Stream = false // 手机上一次性拿结果，不做流式
	ag.AutoCompact = true
	ag.Auto = agent.AutoClean // 本地文件改动都可逆，不用逐项点头
	ag.History = in.History

	var steps []Step
	reply, err := ag.Ask(context.Background(), in.Question, func(e agent.Event) {
		switch e.Kind {
		case agent.EventToolCall:
			steps = append(steps, Step{Kind: "tool", Name: e.Tool, Text: e.Text})
		case agent.EventToolResult:
			steps = append(steps, Step{Kind: "result", Name: e.Tool, Text: e.Text})
		case agent.EventError:
			steps = append(steps, Step{Kind: "error", Name: e.Tool, Text: e.Text})
		}
	})
	if err != nil {
		return askError("%v", err)
	}

	out := AskOutput{Reply: reply, History: ag.History, Steps: steps}
	out.Files, err = changedFiles(orgDir, in.Files)
	if err != nil {
		return askError("读回改动失败：%v", err)
	}
	if raw, err := os.ReadFile(ledgerPath); err == nil && string(raw) != in.Ledger {
		out.Ledger = string(raw)
	}
	return marshal(&out)
}

func (o *AskOutput) normalize() {
	if o.History == nil {
		o.History = []agent.Message{}
	}
}

// changedFiles 把临时目录读回来，只挑出内容变过的和新建的。
func changedFiles(dir string, before map[string]string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var changed map[string]string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".org") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if string(raw) == before[e.Name()] {
			continue // 一个字都没动
		}
		if changed == nil {
			changed = map[string]string{}
		}
		changed[e.Name()] = string(raw)
	}
	return changed, nil
}

// defaultInbox 挑一个收件箱：优先叫 inbox.org 的，没有就按名字取第一个。
// 新建条目没指定文件时会落在这儿。
func defaultInbox(files map[string]string) string {
	if _, ok := files["inbox.org"]; ok {
		return "inbox.org"
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	if len(names) == 0 {
		return "inbox.org"
	}
	sort.Strings(names)
	return names[0]
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
