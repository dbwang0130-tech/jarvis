package chat

import (
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/mattn/go-runewidth"
)

// 存着的几套模型，以及在它们之间切换。
//
// 全挂在 `/model` 一个命令下面，不另开 `/models` `/modeladd` 那一串：
// 命令表是补全和 /help 的单一数据源，一个动作一条命令地加，表很快就没法
// 一眼看完了。名字本身就够分辨要干什么——`/model` 看清单、`/model 豆包`
// 切过去、`/model save 豆包` 存一套、`/model rm 豆包` 删掉。

// currentProfile 把当前摊在 Agent 上的那几个字段收成一套。
func (s *Session) currentProfile() config.ModelProfile {
	a := s.cfg.Agent
	return config.ModelProfile{
		Provider: a.Provider,
		Model:    a.Model,
		BaseURL:  a.BaseURL,
		APIKey:   a.APIKey,
	}
}

// sameAsCurrent 判断某套是不是正在用的那套。比的是真正决定「连到哪、
// 用哪个模型」的三项，名字和密钥不算——同一套配置存两个名字，两个都该
// 显示成当前在用。
func (s *Session) sameAsCurrent(p config.ModelProfile) bool {
	cur := s.currentProfile()
	return p.Provider == cur.Provider && p.Model == cur.Model && p.BaseURL == cur.BaseURL
}

// modelDesc 描述一套模型：用哪个服务商的哪个模型，看不看得见图片。
func modelDesc(p config.ModelProfile) string {
	model := p.Model
	if model == "" {
		model = agent.Presets[p.Provider].Model + "（预设）"
	}
	desc := p.Provider + " · " + model
	if vision, known := agent.SupportsVision(p.Provider, p.Model); known && vision {
		desc += " · 认得图片"
	} else if known {
		desc += " · " + warn + "看不见图片" + reset + dim
	}
	return desc
}

// listModels 列出存着的几套，标出当前在用的那套。
func (s *Session) listModels() {
	cur := s.currentProfile()
	if cur.Provider == "" && len(s.cfg.Agent.Profiles) == 0 {
		note("还没配模型。/setup 走一遍，或者 /provider <服务商> + /login <密钥>。")
		return
	}

	outln("")
	outln(dim + "  当前：" + reset + bold + modelDesc(cur) + reset)
	if len(s.cfg.Agent.Profiles) == 0 {
		outln(dim + "  （还没存过别的。/model save <名字> 把现在这套存下来，以后一句话切回来）" + reset)
		outln("")
		return
	}

	outln("")
	outln(dim + "  存着的：" + reset)
	// 名字列按**显示宽度**对齐，不是字节数——`%-12s` 数的是字节，
	// 一个中文名字占三倍，整列就散了。
	w := 0
	for _, p := range s.cfg.Agent.Profiles {
		if n := runewidth.StringWidth(p.Name); n > w {
			w = n
		}
	}
	for _, p := range s.cfg.Agent.Profiles {
		mark := "  "
		if s.sameAsCurrent(p) {
			mark = coral + "❯ " + reset
		}
		key := ""
		if p.APIKey == "" && agent.Presets[p.Provider].KeyEnv != "" {
			key = warn + "（没密钥）" + reset + dim
		}
		outln(fmt.Sprintf("  %s%s%s%s  %s%s%s",
			mark, bold, pad(p.Name, w), reset, dim, modelDesc(p)+key, reset))
	}
	outln("")
	outln(dim + "  /model <名字> 切过去 · /model save <名字> 存下当前这套 · /model rm <名字> 删掉" + reset)
	outln("")
}

// saveModel 把当前这套存成一个名字。同名就覆盖——重复存一遍是「更新」，
// 不该悄悄留下两条一模一样的。
func (s *Session) saveModel(name string) {
	if s.cfg.Agent.Provider == "" {
		fail("还没配模型，没什么可存的。")
		return
	}
	p := s.currentProfile()
	p.Name = name
	for i, old := range s.cfg.Agent.Profiles {
		if old.Name == name {
			s.cfg.Agent.Profiles[i] = p
			s.saveCfg()
			note("已更新「%s」：%s", name, modelDesc(p))
			return
		}
	}
	s.cfg.Agent.Profiles = append(s.cfg.Agent.Profiles, p)
	s.saveCfg()
	note("已存下「%s」：%s", name, modelDesc(p))
}

// removeModel 删掉一套。删的是清单里那一条，当前在用的配置不动——
// 正用着的那套被删掉之后秘书突然没模型了，谁也想不到是这条命令干的。
func (s *Session) removeModel(name string) {
	for i, p := range s.cfg.Agent.Profiles {
		if p.Name != name {
			continue
		}
		s.cfg.Agent.Profiles = append(s.cfg.Agent.Profiles[:i], s.cfg.Agent.Profiles[i+1:]...)
		s.saveCfg()
		if s.sameAsCurrent(p) {
			note("已从清单里删掉「%s」。当前还用着这套，没有断。", name)
			return
		}
		note("已删掉「%s」。", name)
		return
	}
	fail("没有叫「%s」的，/model 看看存了哪些。", name)
}

// useModel 切到某一套。
func (s *Session) useModel(p config.ModelProfile) {
	s.cfg.Agent.Provider = p.Provider
	s.cfg.Agent.Model = p.Model
	s.cfg.Agent.BaseURL = p.BaseURL
	s.cfg.Agent.APIKey = p.APIKey
	s.saveCfg()

	msg := "已切到「" + p.Name + "」：" + modelDesc(p)
	if p.APIKey == "" && agent.Presets[p.Provider].KeyEnv != "" {
		msg += "。这套还没密钥，/login <密钥> 填上"
	}
	s.reloadAgent(msg)
}

// switchModel 是 `/model` 的全部行为。
func (s *Session) switchModel(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		s.listModels()
		return
	}

	// save / rm 带一个名字。
	if verb, name, ok := strings.Cut(arg, " "); ok {
		name = strings.TrimSpace(name)
		switch verb {
		case "save":
			if name == "" {
				fail("用法：/model save <名字>")
				return
			}
			s.saveModel(name)
			return
		case "rm", "del", "delete":
			if name == "" {
				fail("用法：/model rm <名字>")
				return
			}
			s.removeModel(name)
			return
		}
	}

	// 先当成存着的那套的名字。对得上就整套切过去——服务商、地址、密钥
	// 一起换，而不是只换一个模型名字。
	for _, p := range s.cfg.Agent.Profiles {
		if p.Name == arg {
			s.useModel(p)
			return
		}
	}

	// 没对上就是直接给了个模型名，沿用老行为：在当前服务商底下换模型。
	if s.cfg.Agent.Provider == "" {
		fail("还没选服务商。/provider <%s>", strings.Join(agent.PresetNames(), "、"))
		return
	}
	s.cfg.Agent.Model = arg
	s.saveCfg()
	s.reloadAgent("已切换模型 " + arg)
}
