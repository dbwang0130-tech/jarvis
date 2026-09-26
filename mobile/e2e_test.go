package mobile

import (
	"os"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
)

// 拿**真模型**跑一遍手机那条路：真 JSON 进、真 JSON 出。
//
// 跟 internal/eval 那套的分工：那边验的是 agent 循环和提示（桌面和手机共用的部分），
// 这儿验的是**只有这条路才会坏的东西**——
//
//   - 手机上带哪几项能力（skillset.Build 的 Deps 是这儿传的，传错了某项能力就
//     凭空消失，而编译和契约检查都不会响）
//   - 只回传真变过的文件（多回传一个就多一次 iCloud 写，多一次跟电脑那头撞车）
//   - 人设出参是**指针**：没改是 nil，改成空串是「恢复内置」——收成 string 的话
//     这两件事挤在同一个零值上
//   - 扩展技能的原文过桥之后在 Go 侧解析并真被用上
//   - 对话历史原样往返，下一轮接得上
//
// 别的测试（ask_test.go）拿假 Transport 按脚本作答，验的是接线；这儿验的是
// 真模型在这条路上的实际行为。**要钱要网，所以跟 make eval 同一个开关。**
func mobileModel(t *testing.T) ModelConfig {
	t.Helper()
	if os.Getenv("JARVIS_EVAL") != "1" {
		t.Skip("要真模型、要花钱：JARVIS_EVAL=1 才跑（或 make eval）")
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatalf("读不到配置：%v", err)
	}
	if cfg.Agent.Provider == "" || cfg.Agent.APIKey == "" {
		t.Skip("还没配模型（或密钥不在配置里），先跑一遍 jarvis 的 /setup")
	}
	return ModelConfig{
		Provider: cfg.Agent.Provider,
		Model:    cfg.Agent.Model,
		BaseURL:  cfg.Agent.BaseURL,
		APIKey:   cfg.Agent.APIKey,
		MaxSteps: 12,
	}
}

// mustOK 断言这一轮真办成了。
//
// 走的是 ask_test.go 里那个 ask 辅助函数——marshal → Ask → unmarshal 那一圈
// （以及「不许有 null」那道检查）只该有一份。这儿只多加一条：真模型这条路上
// out.Error 非空就是没办成，继续断言下去只会得到一堆看不懂的失败。
func mustOK(t *testing.T, in AskInput) AskOutput {
	t.Helper()
	out := ask(t, in)
	if out.Error != "" {
		t.Fatalf("这一轮没办成：%s", out.Error)
	}
	return out
}

func toolsUsed(out AskOutput) []string {
	var names []string
	for _, s := range out.Steps {
		if s.Kind == "tool" && s.Name != "" {
			names = append(names, s.Name)
		}
	}
	return names
}

const e2eOrg = "#+TITLE: 工作\n#+TODO: TODO NEXT WAITING | DONE CANCELLED\n\n* TODO 体检预约\n"

// 手机上带哪几项能力——**尤其是不带哪一项**。
//
// 浏览器是桌面端独有的（CDP 要连本机 Chrome，iOS 上没这东西）。这件事现在是
// mobile/ask.go 里 `Browser: nil` 一行声明，传错了就是手机上凭空多出一项它办不到
// 的能力，而模型会照着应承。
func TestMobileHasNoBrowser(t *testing.T) {
	m := mobileModel(t)
	out := mustOK(t, AskInput{
		Files:    map[string]string{"work.org": e2eOrg},
		Question: "帮我打开百度首页搜一下今天天气",
		Model:    m,
	})
	for _, name := range toolsUsed(out) {
		if strings.HasPrefix(name, "browser_") {
			t.Errorf("手机上调了浏览器工具 %s——Browser 那一项应该是 nil", name)
		}
	}
	// 它该直说做不到，而不是绕个弯假装办了。
	if len(out.Files) != 0 || out.Ledger != "" {
		t.Errorf("什么都不该改，却回传了改动：files=%v ledger=%q", out.Files, out.Ledger)
	}
	t.Logf("答复：%s", strings.TrimSpace(out.Reply))
}

// 四项该有的能力都在场：日程、记账、健康、记忆。
func TestMobileSkillsAllReachable(t *testing.T) {
	m := mobileModel(t)
	cases := []struct {
		name, ask, wantTool string
	}{
		{"日程", "加一条待办：买牛奶", "create_item"},
		{"记账", "打车花了 26 块，微信付的", "add_transaction"},
		{"健康", "今天体重 77.5 公斤", "record_vital"},
		{"记忆", "记住：我不喝咖啡", "remember"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := mustOK(t, AskInput{
				Files:    map[string]string{"work.org": e2eOrg},
				Ledger:   "2026/09/01 * 建账\n    Assets:WeChat:Balance  100.00 CNY\n    Equity:Opening\n",
				Question: c.ask,
				Model:    m,
			})
			used := toolsUsed(out)
			for _, n := range used {
				if n == c.wantTool {
					return
				}
			}
			t.Errorf("%s 那项没生效：调的是 %v，该有 %s", c.name, used, c.wantTool)
		})
	}
}

// **只回传真变过的文件。** 多回传一个就多一次 iCloud 写，也多一次跟电脑那头撞车。
func TestMobileOnlyReturnsChangedFiles(t *testing.T) {
	m := mobileModel(t)
	out := mustOK(t, AskInput{
		Files: map[string]string{
			"work.org": e2eOrg,
			"life.org": "#+TITLE: 生活\n\n* TODO 换灯泡\n",
		},
		Question: "把「换灯泡」标成做完了",
		Model:    m,
	})
	if _, ok := out.Files["life.org"]; !ok {
		t.Fatalf("改的那个文件没回传：%v", keysOf(out.Files))
	}
	if _, ok := out.Files["work.org"]; ok {
		t.Errorf("没动过的 work.org 也回传了：%v", keysOf(out.Files))
	}
}

// 人设出参是**指针**：没改是 nil，改了才有。
func TestMobilePersonaRoundTrip(t *testing.T) {
	m := mobileModel(t)
	const before = "你是 Jarvis。称呼用户为「先生」。说话简洁。"

	// 没让它改人设，就不该回传。
	idle := mustOK(t, AskInput{
		Files:    map[string]string{"work.org": e2eOrg},
		Persona:  before,
		Question: "今天有什么待办？",
		Model:    m,
	})
	if idle.Persona != nil {
		t.Errorf("没改人设却回传了（Swift 那边会白写一次 iCloud）：%q", *idle.Persona)
	}

	// 让它改，就必须回传，而且内容真的换了。
	changed := mustOK(t, AskInput{
		Files:    map[string]string{"work.org": e2eOrg},
		Persona:  before,
		Question: "以后别叫我先生了，叫我老板",
		Model:    m,
	})
	if changed.Persona == nil {
		t.Fatal("改了人设却没回传——手机上重启一次就白改了")
	}
	if strings.Contains(*changed.Persona, "先生") || !strings.Contains(*changed.Persona, "老板") {
		t.Errorf("回传的人设不对：%q", *changed.Persona)
	}
}

// 扩展技能的**原文**过桥，在 Go 侧解析，然后真被用上。
//
// 解析只有 skills.Parse 一份（Swift 那边只管读文件、不解析），这条就是验那一份
// 在桥这条路上也走得通。
func TestMobileExtraSkillOverBridge(t *testing.T) {
	m := mobileModel(t)
	out := mustOK(t, AskInput{
		Ledger: "2026/09/01 * 建账\n    Assets:WeChat:Balance  500.00 CNY\n    Equity:Opening\n",
		Skills: []RawSkill{{
			Dir: "云服务记账",
			Content: "---\nname: 云服务记账\n" +
				"description: 记云服务、服务器、API 的花费时用。这类开销有专门的归账规矩。\n" +
				"---\n\n云服务的开销**一律归到 Expenses:Cloud 下面**，按厂商分子账户" +
				"（火山引擎 → Expenses:Cloud:Volcano）。别归到 Expenses:Other。\n",
		}},
		Question: "火山引擎扣了 68 块，微信付的",
		Model:    m,
	})
	if !strings.Contains(out.Ledger, "Expenses:Cloud:Volcano") {
		t.Errorf("扩展技能没在桥这条路上生效：\n调用：%v\n账本：\n%s",
			toolsUsed(out), out.Ledger)
	}
}

// 对话历史原样往返，下一轮接得上。
func TestMobileHistoryRoundTrip(t *testing.T) {
	m := mobileModel(t)
	first := mustOK(t, AskInput{
		Ledger:   "2026/09/01 * 建账\n    Assets:WeChat:Balance  500.00 CNY\n    Equity:Opening\n",
		Question: "打车花了 37 块，微信付的",
		Model:    m,
	})
	if len(first.History) == 0 {
		t.Fatal("没回传历史")
	}
	// 第二轮只给「刚才那笔」这种指代——接不上就说明历史没往返成功。
	second := mustOK(t, AskInput{
		Ledger:   first.Ledger,
		History:  first.History,
		Question: "刚才那笔多少钱？",
		Model:    m,
	})
	if !strings.Contains(second.Reply, "37") {
		t.Errorf("历史没接上，它不知道「刚才那笔」是什么：%q", strings.TrimSpace(second.Reply))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
