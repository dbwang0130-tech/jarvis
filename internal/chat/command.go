package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
)

// command 是一条 / 命令。补全和 /help 都从这张表来——
// 之前两处各写各的，usage 里一度挂着个根本不存在的 /inbox。
type command struct {
	name string
	args string
	desc string
}

var commands = []command{
	{"/setup", "", "从头走一遍配置（模型、日程、邮箱、账本…）"},
	{"/login", "<key>", "设置模型密钥"},
	{"/provider", "<名字>", "切换服务商（deepseek/qwen/kimi/minimax…）"},
	{"/model", "<模型名>", "切换模型"},
	{"/auto", "<off|clean|all>", "有风险动作的自动档"},
	{"/mail", "", "添加邮箱账号"},
	{"/accounts", "", "看已有的邮箱账号"},
	{"/mailrm", "<账号名>", "删掉一个邮箱账号"},
	{"/ledger", "[路径]", "账本文件"},
	{"/files", "<路径...>", "org 文件"},
	{"/theme", "<dark|light|auto>", "切换主题"},
	{"/mouse", "[on|off]", "鼠标滚轮 / 拖选复制，二选一"},
	{"/dream", "", "整理记忆"},
	{"/remind", "", "装上后台提醒"},
	{"/remindrm", "", "移除后台提醒"},
	{"/clear", "", "清空对话上下文"},
	{"/new", "", "另起一个会话"},
	{"/help", "", "看这份帮助"},
	{"/quit", "", "退出"},
}

// matchCommands 找出以 prefix 打头的命令。prefix 不像在打命令就返回 nil。
func matchCommands(prefix string) []command {
	if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, " \n") {
		return nil
	}
	var out []command
	for _, c := range commands {
		if strings.HasPrefix(c.name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// commonPrefix 返回这些命令名的最长公共前缀，用来做 tab 补全。
func commonPrefix(cs []command) string {
	if len(cs) == 0 {
		return ""
	}
	p := cs[0].name
	for _, c := range cs[1:] {
		for !strings.HasPrefix(c.name, p) {
			p = p[:len(p)-1]
		}
	}
	return p
}

// runCommand 执行一条 / 命令，返回是否该退出程序。
func (s *Session) runCommand(input string) bool {
	fields := strings.Fields(strings.TrimSpace(input))
	name := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), fields[0]))

	switch name {
	case "/quit", "/exit":
		note("再见。")
		return true
	case "/help":
		s.help()
	case "/setup":
		s.runSetup()
	case "/login":
		if arg == "" {
			fail("用法：/login sk-...（密钥）")
			return false
		}
		if s.cfg.Agent.Provider == "" {
			s.cfg.Agent.Provider = "deepseek"
		}
		s.cfg.Agent.APIKey = arg
		s.saveCfg()
		s.reloadAgent("已保存密钥")
	case "/provider":
		if arg == "" {
			fail("用法：/provider <%s>", strings.Join(agent.PresetNames(), "、"))
			return false
		}
		s.cfg.Agent.Provider = arg
		s.cfg.Agent.Model = ""
		s.cfg.Agent.APIKey = ""
		s.saveCfg()
		s.reloadAgent("已切换服务商 " + arg)
	case "/model":
		if arg == "" {
			fail("用法：/model <模型名>")
			return false
		}
		s.cfg.Agent.Model = arg
		s.saveCfg()
		s.reloadAgent("已切换模型 " + arg)
	case "/theme":
		if arg == "" {
			note("当前主题：%s", s.cfg.Theme)
			return false
		}
		if arg != "dark" && arg != "light" && arg != "auto" {
			fail("主题可选：dark / light / auto")
			return false
		}
		s.cfg.Theme = arg
		s.saveCfg()
		note("已切换主题 %s", arg)
	case "/clear":
		if s.ag != nil {
			s.ag.Reset()
		}
		s.saveSession()
		note("已清空对话上下文。")
	case "/new":
		if s.ag != nil {
			s.ag.Reset()
		}
		s.sessionID = s.store.NewID()
		s.saveSession()
		note("已开新会话。")
	case "/auto":
		s.setAuto(arg)
	case "/mouse":
		s.setMouse(arg)
	case "/dream":
		s.dream()
	case "/ledger":
		if arg == "" {
			note("账本：%s", s.cfg.LedgerFile)
			return false
		}
		s.cfg.LedgerFile = config.Expand(arg)
		s.ledgerSkill = ledger.NewSkill(s.cfg.LedgerFile)
		s.saveCfg()
		s.reloadAgent("账本已设为 " + s.cfg.LedgerFile)
	case "/accounts":
		if len(s.cfg.Enabled()) == 0 {
			note("还没有邮箱账号，用 /mail 添加。")
			return false
		}
		for _, a := range s.cfg.Enabled() {
			outf("  %s%s%s  %s\n", dim, a.Name, reset, a.User)
		}
	case "/mailrm":
		if arg == "" {
			fail("用法：/mailrm <账号名>")
			return false
		}
		kept := s.cfg.Accounts[:0]
		found := false
		for _, a := range s.cfg.Accounts {
			if a.Name == arg {
				found = true
				continue
			}
			kept = append(kept, a)
		}
		if !found {
			fail("没有名为 %q 的账号", arg)
			return false
		}
		s.cfg.Accounts = kept
		s.mailS.SetConfig(s.cfg)
		s.saveCfg()
		s.reloadAgent("已删除邮箱 " + arg)
	case "/mail":
		s.addMail()
	case "/files":
		if arg == "" {
			fail("用法：/files <路径...>")
			return false
		}
		s.cfg.Files = fields[1:]
		s.saveCfg()
		coll, err := LoadOrg(s.cfg)
		if err != nil {
			fail("%s", err.Error())
			return false
		}
		coll.SetLogStates(s.cfg.LogStateChanges)
		s.coll = coll
		s.orgWS.Coll = coll
		s.orgWS.Cfg = s.cfg
		note("已更新 org 文件")
	case "/remind":
		s.installRemind()
	case "/remindrm":
		s.uninstallRemind()
	default:
		fail("没有 %s 命令，/help 看全部。", name)
	}
	return false
}

// setMouse 开关鼠标上报。这俩只能要一个：开着滚轮能翻历史，
// 但终端原生的拖选被拦掉；关掉就能拖选，翻历史改用 PageUp/PageDown。
func (s *Session) setMouse(arg string) {
	on := scr.MouseOn() // 不带参数就是问问，照当前实际状态答
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "on", "yes", "true":
		on = true
	case "off", "no", "false":
		on = false
	case "":
		if on {
			note("滚轮翻历史（拖选被拦，按住 option 拖可以绕过）。/mouse off 换成拖选优先")
		} else {
			note("拖选复制（滚轮不翻页，用 PageUp/PageDown）。/mouse on 换回滚轮")
		}
		return
	default:
		fail("用法：/mouse [on|off]")
		return
	}

	scr.SetMouse(on)
	s.cfg.Mouse = &on
	s.saveCfg()
	if on {
		note("滚轮可以翻历史了；要拖选的话按住 option 拖，或者 /mouse off")
		return
	}
	note("可以拖选复制了；翻历史用 PageUp/PageDown，或者 /mouse on")
}

// setAuto 切换有风险动作的自动档位。
func (s *Session) setAuto(arg string) {
	if arg == "" {
		note("当前档位：%s（可选 off / clean / all）",
			agent.ParseAutoLevel(s.cfg.Behavior.AutoConfirm).Label())
		return
	}
	lv := agent.ParseAutoLevel(arg)
	if lv == agent.AutoOff && arg != "off" {
		fail("档位可选：off 逐项确认 / clean 自动整理 / all 全自动")
		return
	}
	if s.ag != nil {
		s.ag.SetAuto(lv)
	}
	s.cfg.Behavior.AutoConfirm = arg
	s.saveCfg()
	note("档位已切到「%s」", lv.Label())
}

func (s *Session) help() {
	var b strings.Builder
	b.WriteString(dim + `按键：
  tab / shift+tab       轮换视图：秘书 / 日程 / 待办 / 邮件 / 财务
                        正在打命令时，tab 是补全
  任意键                在别的视图里打字，即回到秘书视图接着输入
  滚轮 / PageUp PageDown  往回翻历史（输入框钉在下面不动）
  选择文字              按住 option（或 shift）拖；或 /mouse off 换成拖选优先
  ctrl+r                重新读一遍 org 文件并刷新当前视图
  esc / ctrl+c          打断秘书；确认框里是取消

输入：
  alt+回车              换行（回车是提交）
  粘贴多行              整段收下，不会被拆成好几条

行编辑（和 readline 一套）：
  ↑ ↓ / ctrl+p ctrl+n   翻输入历史
  ← → / ctrl+b ctrl+f   移动光标      alt+b alt+f  按词移动
  ctrl+a / ctrl+e       行首 / 行尾
  ctrl+k / ctrl+u       删到行尾 / 删到行首
  ctrl+w / alt+d        删前一个词 / 删后一个词
  ctrl+y                把刚删掉的粘回来
  ctrl+t                把光标前两个字掉个个儿
  ctrl+l                清掉翻得到的历史
  ctrl+d                删光标处；空行时退出

命令：
`)
	for _, c := range commands {
		name := c.name
		if c.args != "" {
			name += " " + c.args
		}
		fmt.Fprintf(&b, "  %s%s\n", pad(name, 26), c.desc)
	}
	// 末尾的换行去掉再收色，不然会多空出一行。
	out(strings.TrimRight(b.String(), "\n") + reset + "\n")
}

func (s *Session) dream() {
	if s.ag == nil {
		fail("Jarvis 不可用，无法整理记忆")
		return
	}
	content := strings.TrimSpace(s.memSkill.Content)
	if content == "" {
		note("还没有记忆。")
		return
	}
	note("正在整理记忆…")
	ctx := context.Background()
	msgs := []agent.Message{
		{Role: "system", Content: "你是记忆整理助手。把用户的多条记忆整理成精简、固化、统一的规则：去掉过时与重复；矛盾的以最近最明确为准；场景化偏好提炼成一般规则。直接输出整理后的记忆，每条一行。"},
		{Role: "user", Content: "当前记忆：\n\n" + content},
	}
	reply, _, err := s.ag.Client.Chat(ctx, msgs, nil)
	if err != nil {
		fail("%s", err.Error())
		return
	}
	text := strings.TrimSpace(reply.Content)
	if err := s.memSkill.Save(text); err != nil {
		fail("整理出来了但没存下：%v", err)
		return
	}
	s.memSkill.Content = strings.TrimSpace(text) + "\n"
	note("记忆已整理：%s\n%s", reset, text)
}

func (s *Session) addMail() {
	note("添加邮箱账号（esc 留空即取消）：")
	name, kind := s.readLine("账号名（如 gmail、qq）：")
	if kind != evLine || name == "" {
		return
	}
	user, kind := s.readLine("邮箱地址：")
	if kind != evLine || user == "" {
		return
	}
	host, kind := s.readLine("IMAP 服务器（如 imap.gmail.com）：")
	if kind != evLine || host == "" {
		return
	}
	portStr, kind := s.readLine("IMAP 端口（默认 993）：")
	if kind != evLine {
		return
	}
	port := 993
	if portStr != "" {
		fmt.Sscanf(portStr, "%d", &port)
	}
	pw, kind := s.readSecret("密码/授权码（Gmail 用应用专用密码）：")
	if kind != evLine || pw == "" {
		return
	}
	acc := config.MailAccount{Name: name, User: user, Host: host, Port: port}
	acc.Normalize()
	if err := config.KeychainSet("mail:"+name, user, pw); err != nil {
		fail("%s", err.Error())
		return
	}
	s.cfg.Accounts = append(s.cfg.Accounts, acc)
	s.mailS.SetConfig(s.cfg)
	s.saveCfg()
	s.reloadAgent("已添加邮箱 " + name)
}
