package agent

import (
	"fmt"
	"time"

	"github.com/dabao/jarvis/internal/config"
)

// Build 按配置构造秘书。未配置 provider 或缺少密钥时返回 nil 与原因，
// 调用方据此把界面降级为纯浏览模式。
func Build(cfg config.Config, skills ...Skill) (*Agent, error) {
	if cfg.Agent.Provider == "" {
		return nil, fmt.Errorf("尚未设置模型服务商，在秘书面板输入 /login 即可配置")
	}
	client, err := New(Options{
		Provider:    cfg.Agent.Provider,
		Model:       cfg.Agent.Model,
		BaseURL:     cfg.Agent.BaseURL,
		APIKey:      cfg.Agent.APIKey,
		APIKeyEnv:   cfg.Agent.APIKeyEnv,
		APIKeyCmd:   cfg.Agent.APIKeyCmd,
		Temperature: cfg.Agent.Temperature,
		Timeout:     time.Duration(cfg.Agent.TimeoutSec) * time.Second,
		Proxy:       cfg.Agent.Proxy,
	})
	if err != nil {
		return nil, err
	}
	agent := NewAgent(client, skills, cfg.Agent.MaxSteps, ParseAutoLevel(cfg.Behavior.AutoConfirm))
	agent.AutoCompact = cfg.Agent.AutoCompact
	agent.ContextLimit = cfg.Agent.ContextLimit
	agent.Profile = cfg.Profile.Interests
	return agent, nil
}
