package chat

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dabao/jarvis/internal/acp"
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/session"
)

// RunACP 用 stdio 跑 ACP（`jarvis acp`），供 UULink、Zed 这类客户端驱动。
//
// 装配和终端界面走同一个 setup：技能清单、人设、扩展技能一样不少，只是不碰
// 终端（不进 raw 模式、不画屏幕）。stdout 是协议通道，**这条路上任何东西都
// 不许往 stdout 打字**，提示一律走 stderr。
func RunACP(version string) error {
	h := &acpHost{cfgPath: config.Path()}
	store := session.New(filepath.Join(config.Dir(), "sessions"))
	return acp.New(h, store, version).Serve(os.Stdin, os.Stdout)
}

type acpHost struct {
	cfgPath string
}

// NewAgent 每次都重读配置：终端里刚 /setup 改过的东西，新开的会话就能用上。
func (h *acpHost) NewAgent() (*agent.Agent, error) {
	cfg, err := config.Load(h.cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "jarvis acp:", err)
	}
	s := &Session{cfgPath: h.cfgPath, cfg: cfg}
	if err := s.setup(); err != nil {
		return nil, err
	}
	for _, w := range s.startupWarn {
		fmt.Fprintln(os.Stderr, "jarvis acp:", w)
	}
	return s.ag, s.agentErr
}

func (h *acpHost) Models() ([]string, string) {
	cfg, _ := config.Load(h.cfgPath)
	var names []string
	current := ""
	for _, p := range cfg.Agent.Profiles {
		names = append(names, p.Name)
		if p.Provider == cfg.Agent.Provider && p.Model == cfg.Agent.Model && p.BaseURL == cfg.Agent.BaseURL {
			current = p.Name
		}
	}
	return names, current
}

// UseModel 只换这个秘书的模型客户端，历史留着；配置文件不动——ACP 客户端里给
// 一个会话换模型，不该连带改掉终端那边的默认。
func (h *acpHost) UseModel(a *agent.Agent, name string) error {
	cfg, _ := config.Load(h.cfgPath)
	for _, p := range cfg.Agent.Profiles {
		if p.Name != name {
			continue
		}
		cfg.Agent.Provider, cfg.Agent.Model, cfg.Agent.BaseURL, cfg.Agent.APIKey = p.Provider, p.Model, p.BaseURL, p.APIKey
		next, err := agent.Build(cfg)
		if err != nil {
			return err
		}
		a.Client = next.Client
		return nil
	}
	return fmt.Errorf("没有这套模型：%s", name)
}
