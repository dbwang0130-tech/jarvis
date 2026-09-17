package chat

import (
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
)

// 配置向导：把散在七八个 / 命令里的设置串成一条路走完。
// 每一步都能直接回车跳过（保持原样），esc 随时中止，已经存下的不回滚。

// step 是向导里的一步。done 为真表示这一步真的改了东西。
type step struct {
	title string
	run   func(*Session) (done bool, abort bool)
}

var setupSteps = []step{
	{"模型", (*Session).setupModel},
	{"日程文件", (*Session).setupFiles},
	{"邮箱", (*Session).setupMail},
	{"账本", (*Session).setupLedger},
	{"自动档", (*Session).setupAuto},
	{"后台提醒", (*Session).setupRemind},
}

// runSetup 走一遍配置向导。
func (s *Session) runSetup() {
	out("\n" + sectionTitle("配置向导") + "\n")
	note("一共 %d 步，每步直接回车就跳过，esc 随时退出。", len(setupSteps))

	var changed int
	for i, st := range setupSteps {
		out("\n")
		msg(lvSystem, "%s第 %d/%d 步 · %s%s", bold, i+1, len(setupSteps), st.title, reset)
		done, abort := st.run(s)
		if abort {
			note("向导中止了，前面几步已经存下的仍然有效。")
			return
		}
		if done {
			changed++
		}
	}

	out("\n")
	msg(lvSystem, "%s配置好了%s", bold, reset)
	s.showConfigSummary()
	if changed == 0 {
		note("这次什么都没改，原样保持。")
		return
	}
	s.reloadAgent(fmt.Sprintf("这次改了 %d 处设置", changed))
}

// prompt 问一句话。返回的 abort 为真表示用户按了 esc 想退出。
func (s *Session) prompt(hint string) (answer string, abort bool) {
	line, kind := s.readLine(hint)
	return strings.TrimSpace(line), kind != evLine
}

// askYes 问一个是非题，def 是直接回车时的答案。
func (s *Session) askYes(hint string, def bool) (yes bool, abort bool) {
	tail := "（y/N）"
	if def {
		tail = "（Y/n）"
	}
	line, abort := s.prompt(hint + tail + "：")
	if abort {
		return false, true
	}
	switch strings.ToLower(line) {
	case "y", "yes", "是":
		return true, false
	case "n", "no", "否":
		return false, false
	}
	return def, false
}

func (s *Session) setupModel() (bool, bool) {
	cur := s.cfg.Agent.Provider
	if cur == "" {
		cur = "还没设"
	}
	note("现在是：%s", cur)
	note("可选：%s", strings.Join(agent.PresetNames(), " / "))

	name, abort := s.prompt("服务商（回车保持不变）：")
	if abort {
		return false, true
	}
	var changed bool
	if name != "" && name != s.cfg.Agent.Provider {
		if _, ok := agent.Presets[name]; !ok {
			fail("没有 %s 这个服务商，这一步跳过了", name)
		} else {
			s.cfg.Agent.Provider = name
			s.cfg.Agent.Model = "" // 真换了服务商，模型名才跟着回到预设
			changed = true
		}
	}
	if s.cfg.Agent.Provider == "" {
		return changed, false // 没定服务商就不用问密钥了
	}

	hint := "密钥（回车保持不变）："
	if s.cfg.Agent.APIKey == "" {
		hint = fmt.Sprintf("密钥（形如 sk-…，也可以留空改用环境变量 %s）：",
			agent.Presets[s.cfg.Agent.Provider].KeyEnv)
	}
	key, abort := s.readSecretLine(hint)
	if abort {
		return changed, true
	}
	if key != "" && key != s.cfg.Agent.APIKey {
		s.cfg.Agent.APIKey = key
		changed = true
	}
	if changed {
		_ = s.cfg.Save(s.cfgPath)
	}
	return changed, false
}

// readSecretLine 是 readSecret 的 trim 版。
func (s *Session) readSecretLine(hint string) (string, bool) {
	line, kind := s.readSecret(hint)
	return strings.TrimSpace(line), kind != evLine
}

func (s *Session) setupFiles() (bool, bool) {
	note("现在是：%s", strings.Join(s.cfg.Files, "、"))
	note("可以是目录（里面的 .org 全算），也可以是单个文件。")

	line, abort := s.prompt("日程文件或目录（回车保持不变）：")
	if abort || line == "" {
		return false, abort
	}
	if want := strings.Fields(line); sameStrings(want, s.cfg.Files) {
		return false, false // 填的还是这些，不折腾
	}
	old := s.cfg.Files
	s.cfg.Files = strings.Fields(line)
	coll, err := LoadOrg(s.cfg)
	if err != nil {
		s.cfg.Files = old
		fail("%s（这一步没改）", err)
		return false, false
	}
	coll.SetLogStates(s.cfg.LogStateChanges)
	s.coll, s.orgWS.Coll, s.orgWS.Cfg = coll, coll, s.cfg
	_ = s.cfg.Save(s.cfgPath)
	note("读到 %d 个文件。", len(coll.Docs))
	return true, false
}

func (s *Session) setupMail() (bool, bool) {
	if n := len(s.cfg.Accounts); n > 0 {
		note("已经有 %d 个账号：%s", n, strings.Join(accountNames(s.cfg), "、"))
		more, abort := s.askYes("再加一个", false)
		if abort || !more {
			return false, abort
		}
	} else {
		note("Jarvis 要收发邮件的话得先加个账号，不用就跳过。")
		want, abort := s.askYes("现在加一个", false)
		if abort || !want {
			return false, abort
		}
	}
	before := len(s.cfg.Accounts)
	s.addMail()
	return len(s.cfg.Accounts) > before, false
}

func accountNames(cfg config.Config) []string {
	out := make([]string, 0, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		out = append(out, a.Name)
	}
	return out
}

func (s *Session) setupLedger() (bool, bool) {
	note("现在是：%s", s.cfg.LedgerFile)
	note("ledger-cli 的纯文本账本，不记账就跳过。")

	line, abort := s.prompt("账本文件（回车保持不变）：")
	if abort || line == "" {
		return false, abort
	}
	if want := config.Expand(line); want == s.cfg.LedgerFile {
		return false, false
	} else {
		s.cfg.LedgerFile = want
	}
	s.ledgerSkill = ledger.NewSkill(s.cfg.LedgerFile)
	_ = s.cfg.Save(s.cfgPath)
	return true, false
}

func (s *Session) setupAuto() (bool, bool) {
	cur := agent.ParseAutoLevel(s.cfg.Behavior.AutoConfirm)
	note("现在是：%s", cur.Label())
	note("off 逐项确认 · clean 清理类自动做、发信仍问 · all 全自动")
	note("发出去的信收不回来，拿不准就选 off。")

	line, abort := s.prompt("档位（回车保持不变）：")
	if abort || line == "" {
		return false, abort
	}
	lv := agent.ParseAutoLevel(line)
	if lv == agent.AutoOff && line != "off" {
		fail("只认 off / clean / all，这一步跳过了")
		return false, false
	}
	if lv == cur {
		return false, false // 还是这一档
	}
	s.cfg.Behavior.AutoConfirm = line
	if s.ag != nil {
		s.ag.SetAuto(lv)
	}
	_ = s.cfg.Save(s.cfgPath)
	return true, false
}

func (s *Session) setupRemind() (bool, bool) {
	if remindInstalled() {
		note("已经装好了，每 5 分钟查一次；不想要了用 /remindrm 移除。")
		return false, false
	}
	note("装上之后每 5 分钟查一次日程，到点前 5 分钟弹通知。")
	note("只对带具体时刻的条目有效（整天的不弹）。")

	want, abort := s.askYes("现在装上", false)
	if abort || !want {
		return false, abort
	}
	s.installRemind()
	return true, false
}

// sameStrings 判断两批路径是不是一回事。
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if config.Expand(a[i]) != config.Expand(b[i]) {
			return false
		}
	}
	return true
}

// showConfigSummary 把配好的东西列一遍。
func (s *Session) showConfigSummary() {
	model := s.modelName()
	if model == "" {
		model = "还没设"
	}
	key := "没设"
	if s.cfg.Agent.APIKey != "" {
		key = "已设"
	}
	mail := "没有"
	if n := len(s.cfg.Accounts); n > 0 {
		mail = fmt.Sprintf("%d 个（%s）", n, strings.Join(accountNames(s.cfg), "、"))
	}

	for _, l := range []struct{ k, v string }{
		{"模型", fmt.Sprintf("%s · %s · 密钥%s", s.cfg.Agent.Provider, model, key)},
		{"日程", strings.Join(s.cfg.Files, "、")},
		{"邮箱", mail},
		{"账本", s.cfg.LedgerFile},
		{"档位", agent.ParseAutoLevel(s.cfg.Behavior.AutoConfirm).Label()},
		{"配置", s.cfgPath},
	} {
		note("%s  %s", pad(l.k, 6), l.v)
	}
}
