package chat

import (
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
)

func modelSession(t *testing.T) *Session {
	t.Helper()
	captureTerm(t)
	s := &Session{cfg: config.Default(), cfgPath: t.TempDir() + "/config.json"}
	s.cfg.Agent.Provider = "deepseek"
	return s
}

// 存一套、切走、再切回来：切换换的是**整套**接入参数，不是只换个模型名。
// 只换模型名的话，从豆包切回 deepseek 会留着豆包的地址和密钥，连出去必错。
func TestSaveAndSwitchCarriesWholeProfile(t *testing.T) {
	s := modelSession(t)
	s.cfg.Agent.Model = "deepseek-flash"
	s.cfg.Agent.APIKey = "sk-ds"
	s.saveModel("深度")

	// 换到豆包并存下来。
	s.cfg.Agent.Provider = "doubao"
	s.cfg.Agent.Model = ""
	s.cfg.Agent.BaseURL = "https://ark.example/api/v3"
	s.cfg.Agent.APIKey = "ark-key"
	s.saveModel("豆包")

	// 切回深度：服务商、模型、地址、密钥都得跟着回去。
	s.switchModel("深度")
	a := s.cfg.Agent
	if a.Provider != "deepseek" || a.Model != "deepseek-flash" || a.APIKey != "sk-ds" {
		t.Fatalf("没整套切回来：%+v", a)
	}
	if a.BaseURL != "" {
		t.Fatalf("豆包那套的地址跟过来了：%q", a.BaseURL)
	}
}

// 同名再存一次是「更新」，不该在清单里留下两条。
func TestSaveSameNameUpdates(t *testing.T) {
	s := modelSession(t)
	s.cfg.Agent.Model = "a"
	s.saveModel("常用")
	s.cfg.Agent.Model = "b"
	s.saveModel("常用")

	if n := len(s.cfg.Agent.Profiles); n != 1 {
		t.Fatalf("同名该覆盖，现在有 %d 条", n)
	}
	if got := s.cfg.Agent.Profiles[0].Model; got != "b" {
		t.Fatalf("没更新成新的那套，还是 %q", got)
	}
}

// 删掉正用着的那套，只是从清单里去掉——当前连接不能跟着断。
// 断了的话秘书突然不工作，没人会想到是刚才那条 /model rm。
func TestRemoveActiveKeepsItRunning(t *testing.T) {
	s := modelSession(t)
	s.cfg.Agent.Model = "deepseek-flash"
	s.saveModel("手头这个")
	s.removeModel("手头这个")

	if len(s.cfg.Agent.Profiles) != 0 {
		t.Fatal("没从清单里删掉")
	}
	if s.cfg.Agent.Provider != "deepseek" || s.cfg.Agent.Model != "deepseek-flash" {
		t.Fatalf("当前配置被删掉了：%+v", s.cfg.Agent)
	}
}

// 名字对不上任何一套时，沿用老行为：当成模型名，在当前服务商底下换。
// 这条是为了不打断肌肉记忆——`/model deepseek-flash` 一直是这么用的。
func TestUnknownNameFallsBackToPlainModel(t *testing.T) {
	s := modelSession(t)
	s.switchModel("some-new-model")

	if s.cfg.Agent.Model != "some-new-model" {
		t.Fatalf("该当成模型名，现在是 %q", s.cfg.Agent.Model)
	}
	if s.cfg.Agent.Provider != "deepseek" {
		t.Fatalf("服务商不该动，现在是 %q", s.cfg.Agent.Provider)
	}
}

// 「看不看得见图片」只在用着预设默认模型时才敢说死。用户自己填了模型名，
// 我们无从判断，这时候什么都不说——拿一张会过期的表去误标，比不标更糟。
func TestVisionOnlyClaimedForPresetDefault(t *testing.T) {
	preset := modelDesc(config.ModelProfile{Provider: "doubao"})
	if !strings.Contains(preset, "视觉") {
		t.Fatalf("预设默认模型该标出视觉能力：%s", preset)
	}
	custom := modelDesc(config.ModelProfile{Provider: "doubao", Model: "某个没见过的"})
	if strings.Contains(custom, "视觉") || strings.Contains(custom, "不支持图像输入") {
		t.Fatalf("自定义模型不该妄下判断：%s", custom)
	}
	noVision := modelDesc(config.ModelProfile{Provider: "spark"})
	if !strings.Contains(noVision, "不支持图像输入") {
		t.Fatalf("纯文本的预设该说明白：%s", noVision)
	}
}

// 上下文窗口按 1024 进位。服务商报的 1048576 正是 1M，除以 1000 得出的
// 「1048k」既不是它说的数，也不是任何人会说的话。
func TestWindowSize(t *testing.T) {
	for n, want := range map[int]string{
		1048576: "1M",
		131072:  "128k",
		8192:    "8k",
		0:       "", // 没报就什么都不显示，别编一个出来
		512:     "",
	} {
		if got := windowSize(n); got != want {
			t.Errorf("windowSize(%d) = %q，要 %q", n, got, want)
		}
	}
}

// 命令表是补全和 /help 的单一数据源——`/model list` 加在实现里却没进表，
// 补全和帮助就都不知道有这回事（`/inbox` 当年是反着来的：表里挂着一个
// 根本不存在的命令）。
func TestModelUsageMentionsList(t *testing.T) {
	for _, c := range commands {
		if c.name == "/model" {
			if !strings.Contains(c.args, "list") {
				t.Errorf("/model 的用法里没有 list：%q", c.args)
			}
			return
		}
	}
	t.Fatal("命令表里没有 /model")
}
