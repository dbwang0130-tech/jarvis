package chat

import (
	"context"
	"fmt"
	"path/filepath"
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
	"github.com/dabao/jarvis/internal/persona"
	"github.com/dabao/jarvis/internal/session"
	extskills "github.com/dabao/jarvis/internal/skills"
	"github.com/dabao/jarvis/internal/skillset"
)

// 极简 ANSI 配色（流式模式不依赖 lipgloss）。
const (
	dim       = "\x1b[2m"
	cyan      = "\x1b[36m"
	coral     = "\x1b[38;5;209m"
	green     = "\x1b[32m"
	red       = "\x1b[31m"
	warn      = "\x1b[33m"
	blue      = "\x1b[34m"
	gray      = "\x1b[38;5;245m"
	bold      = "\x1b[1m"
	underline = "\x1b[4m"
	reset     = "\x1b[0m"
)

// 所有输出都走 screen：它负责 raw 模式下的换行转换，
// 并保证状态行（spinner）不会和正文串在一起。
func outf(format string, args ...any) { scr.write(fmt.Sprintf(format, args...)) }

func outln(s string) { scr.write(s + "\n") }

func out(s string) { scr.write(s) }

// Session 是流式聊天的一次运行。
type Session struct {
	cfgPath string
	cfg     config.Config

	coll        *org.Collection
	orgWS       *orgtools.Workspace
	mailS       *mail.Session
	ledgerSkill *ledger.Skill
	healthSkill *health.Skill
	memSkill    *memory.Skill
	// extra 是启动时扫到的扩展技能，reloadAgent 也要用，所以留着。
	// 扫出来的警告不留字段——下一行就并进 startupWarn 了，留一份永远同步的
	// 副本只会让看的人先去确认它俩没有第二个写入点。
	extra    []agent.ExtraSkill
	skills   []agent.Skill
	ag       *agent.Agent
	agentErr error

	store     *session.Store
	sessionID string

	reader    *reader
	pendingMu sync.Mutex
	pending   *agent.Pending
	box       *confirm

	// lastCtrlC 用于「再按一次 ctrl+c 退出」。
	lastCtrlC time.Time
	// live 是正文的可重画区域：每来一点增量就整段重渲染。
	live live
	// warned 记录「还没配密钥」的提示是否已经说过。
	warned bool
	// startupWarn 是启动时没能办妥、但又不值得把人挡在门外的事。
	startupWarn []string
	// 上下文占比要全量扫一遍历史才能估出来，而状态栏每帧都要它。
	// 历史没长，结果就不会变，存着复用。
	ctxLen   int
	ctxRatio float64
	// queued 是秘书干活时用户又打进来的话，等这一轮忙完接着办。
	queueMu sync.Mutex
	queued  []string
	// sessWarned 记录「会话存不下来」是否已经说过——它每轮都要存一次，
	// 真坏了的话不提醒就丢历史，每轮都提醒又会把屏幕刷满。
	sessWarned bool
}

// Run 启动流式聊天。
func Run(cont bool) error {
	cfgPath := config.Path()
	// 配置读不出来也照进——Load 出错时给的是一份能用的默认配置，
	// 进到界面里还能 /setup 重配；这时候把人挡在门外最没意义。
	cfg, cfgErr := config.Load(cfgPath)

	s := &Session{cfgPath: cfgPath, cfg: cfg}
	if cfgErr != nil {
		s.startupWarn = append(s.startupWarn, cfgErr.Error())
	}

	rd, err := newReader()
	if err != nil {
		return err
	}
	s.reader = rd
	defer rd.close()

	// 整块屏幕交给 screen 画：内容区（历史 + 正在流的正文）在上，
	// 输入框钉在下面。退出时会把历史回放到主屏幕。
	scr.liveLines = s.live.activeLines
	scr.inputLines = rd.inputLines
	scr.hintLines = rd.hintLines
	scr.statusFn = s.statusLine
	scr.enter(s.cfg.MouseOn())
	defer scr.leave()

	if err := s.setup(); err != nil {
		return err
	}

	// 会话：-c 继续上次，否则新会话。
	s.store = session.New(filepath.Join(config.Dir(), "sessions"))
	s.sessionID = s.store.NewID()
	if cont {
		if id, ok := s.store.LastID(); ok {
			s.sessionID = id
			if h, err := s.store.Load(id); err == nil && h != nil {
				s.ag.History = h
			}
		}
	}

	printBanner()
	for _, w := range s.startupWarn {
		fail("%s", w)
	}
	s.loop()
	return nil
}

func (s *Session) setup() error {
	coll, err := LoadOrg(s.cfg)
	if err != nil {
		// 日程读不了同样不该挡着启动：先给个空的顶上，
		// 进去之后 /files 或 /setup 都能改。
		s.startupWarn = append(s.startupWarn,
			fmt.Sprintf("日程没读上来：%v（/files 或 /setup 可以改路径）", err))
		coll = &org.Collection{}
	}
	coll.SetLogStates(s.cfg.LogStateChanges)
	s.coll = coll
	s.orgWS = orgtools.NewWorkspace(coll, s.cfg)
	s.mailS = mail.NewSession(s.cfg)
	s.ledgerSkill = ledger.NewSkill(s.cfg.LedgerFile)
	if moved, err := config.MigrateMemory(s.cfg); err != nil {
		s.startupWarn = append(s.startupWarn,
			fmt.Sprintf("记忆没能挪到 %s：%v", s.cfg.MemoryPath(), err))
	} else if moved {
		s.startupWarn = append(s.startupWarn,
			fmt.Sprintf("记忆已挪到 %s（原件留在配置目录里没动）", s.cfg.MemoryPath()))
	}
	s.healthSkill = health.New(s.cfg.HealthFile)
	s.memSkill = memory.New(s.cfg.MemoryPath())
	// 用户放在 skills/ 下的扩展技能。读不出来只是少几项扩展能力，
	// 照常进界面——进去了才能用 /setup 修。
	extra, warns := extskills.Load(s.cfg.SkillsPath())
	s.extra = extra
	s.startupWarn = append(s.startupWarn, warns...)

	s.skills = s.buildSkills()
	s.ag, s.agentErr = s.buildAgent()
	return nil
}

// buildAgent 装配秘书，并把人设接上。
//
// 人设文件由会话宿主读写，agent 内核只接收已加载的人设。
func (s *Session) buildAgent() (*agent.Agent, error) {
	ag, err := agent.Build(s.cfg, s.skills...)
	if err != nil {
		return nil, err
	}
	path := s.cfg.PersonaPath()
	ag.SetPersona(persona.Load(path), func(text string) error {
		return persona.Save(path, text)
	})
	// 扩展技能只把名字和用途放进提示，正文等模型调 skill 再读。
	ag.SetExtraSkills(s.extra)
	return ag, nil
}

// buildSkills 交出这一轮的技能清单。
//
// 清单本身归 internal/skillset——那儿只有一份，ACP 走的也是这里（见 acphost.go）。
// 这边只负责说清手上有什么。
func (s *Session) buildSkills() []agent.Skill {
	return skillset.Build(skillset.Deps{
		Org:    s.orgWS,
		Mail:   s.mailS,
		Ledger: s.ledgerSkill,
		Health: s.healthSkill,
		Memory: s.memSkill,
	})
}

func (s *Session) reloadAgent(msg string) {
	s.skills = s.buildSkills()
	s.ag, s.agentErr = s.buildAgent()
	if s.agentErr != nil {
		fail("Jarvis 不可用：%v", s.agentErr)
		return
	}
	note("%s", msg)
}

func (s *Session) saveSession() {
	if s.store == nil || s.ag == nil {
		return
	}
	if err := s.store.Save(s.sessionID, s.ag.History); err != nil && !s.sessWarned {
		// 说一次就够。不说的话，这一整轮对话在 `jarvis -c` 那边就是不存在的。
		s.sessWarned = true
		fail("这次对话存不下来，下次 -c 接不上：%v", err)
	}
}

// saveCfg 存配置，存不下来要吭声——改了设置却悄悄没生效，
// 下次启动又变回去，人只会以为自己记错了。
func (s *Session) saveCfg() {
	if err := s.cfg.Save(s.cfgPath); err != nil {
		fail("设置没存下来：%v", err)
	}
}

func printBanner() {
	outln(cyan + `     _   _    ______     _____ ____
    | | / \  |  _ \ \   / /_ _/ ___|
 _  | |/ _ \ | |_) \ \ / / | |\___ \
| |_| / ___ \|  _ < \ V /  | | ___) |
 \___/_/   \_\_| \_\ \_/  |___|____/` + reset)
	// 欢迎块是块招牌，不是回执，所以不带 · 前缀。
	outln(dim + "  个人助理 · 日程 / 邮件 / 记账 / 健康" + reset)
	outln(dim + "  滚轮翻历史 · /help 看命令" + reset)
}

// statusDot 是状态栏最前面那个点：绿=待命，黄=正忙，红=秘书不可用。
func (s *Session) statusDot() string {
	switch {
	case s.agentErr != nil:
		return red + "●" + reset
	case s.pending != nil:
		return warn + "◆" + reset
	case s.reader != nil && s.reader.busy.Load():
		return warn + "●" + reset
	}
	return green + "●" + reset
}

func (s *Session) loop() {
	for {
		if s.agentErr != nil && !s.warned {
			s.warned = true
			note("还没配好。敲 /setup 走一遍向导，或者 /login sk-... 只设密钥。")
		}
		// 上一轮忙的时候排进来的话，先办掉。
		if q := s.dequeue(); q != "" {
			s.echo(q)
			if strings.HasPrefix(q, "/") {
				if quit := s.runCommand(q); quit {
					return
				}
			} else {
				s.ask(q)
				s.saveSession()
			}
			continue
		}

		line, kind := s.readLine("")
		if kind == evEOF {
			outln("")
			return
		}
		if kind == evCtrlC {
			// 空输入下按 ctrl+c：两秒内再按一次才退出。
			if time.Since(s.lastCtrlC) < 2*time.Second {
				outln("")
				return
			}
			s.lastCtrlC = time.Now()
			note("再按一次 ctrl+c 退出。")
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		scr.toBottom()
		s.echo(line)
		if strings.HasPrefix(line, "/") {
			if quit := s.runCommand(line); quit {
				return
			}
			continue
		}
		s.ask(line)
		s.saveSession()
	}
}

// echo 把用户刚说的话记进历史。前面空一行，一轮一轮才分得开；
// 多行的话续行缩进两格，跟 "> " 后面对齐。
func (s *Session) echo(line string) {
	out("\n")
	msg(lvUser, "%s", line)
}

// statusLine 是输入框下面那一行：状态点、模型、上下文占用、档位、排队数。
func (s *Session) statusLine() string {
	parts := []string{s.statusDot()}
	if m := s.modelName(); m != "" {
		parts = append(parts, m)
	}
	if s.ag != nil {
		if ratio := s.contextRatio(); ratio > 0.01 {
			parts = append(parts, fmt.Sprintf("上下文 %.0f%%", ratio*100))
		}
		parts = append(parts, s.ag.Auto.Label())
	}
	if n := s.queueLen(); n > 0 {
		parts = append(parts, fmt.Sprintf("%s排队 %d%s", warn, n, reset+dim))
	}
	return dim + "  " + strings.Join(parts, " · ") + reset
}

// contextRatio 是上下文占了多少，按历史长度缓存——估算要扫全部消息，
// 而状态栏每帧都问一次。
func (s *Session) contextRatio() float64 {
	if n := len(s.ag.History); n != s.ctxLen {
		_, s.ctxRatio = s.ag.ContextUsage()
		s.ctxLen = n
	}
	return s.ctxRatio
}

// modelName 是状态栏上显示的模型名。
func (s *Session) modelName() string {
	if s.cfg.Agent.Provider == "" {
		return ""
	}
	if s.cfg.Agent.Model != "" {
		return s.cfg.Agent.Model
	}
	return agent.Presets[s.cfg.Agent.Provider].Model
}

// readLine 画出输入框读一行。hint 非空时先在框上方打一行提示。
func (s *Session) readLine(hint string) (string, eventKind) {
	if hint != "" {
		promptLine(hint)
	}
	s.reader.draw()
	for {
		ev := <-s.reader.ch
		switch ev.kind {
		case evLine:
			return ev.line, evLine
		case evEOF:
			return "", evEOF
		case evCtrlC:
			return "", evCtrlC
		case evRedraw:
			// ctrl+r：重新读一遍 org 文件，把外部改动也捞进来。
			if err := s.coll.Reload(); err != nil {
				fail("%s", err.Error())
			}
			scr.refresh()
		}
	}
}

// readSecret 读一行但不回显，用于密码。
func (s *Session) readSecret(hint string) (string, eventKind) {
	s.reader.setMasked(true)
	defer s.reader.setMasked(false)
	return s.readLine(hint)
}

func (s *Session) ask(input string) {
	if s.ag == nil {
		fail("Jarvis 不可用：%v", s.agentErr)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.live = live{}
	s.reader.setBusy(true)
	scr.startSpin()

	go func() {
		for {
			select {
			case ev := <-s.reader.ch:
				switch ev.kind {
				case evPick:
					s.decide(ev.line)
				case evLine:
					// 秘书正忙，这句话先排队。
					if line := strings.TrimSpace(ev.line); line != "" {
						s.enqueue(line)
						note("⏎ 已排队：%s", line)
					}
				case evCtrlC, evEOF:
					cancel()
				}
			case <-done:
				return
			}
		}
	}()

	answer, err := s.ag.Ask(ctx, input, s.emit)
	close(done)
	cancel()
	s.live.freeze()
	scr.stopSpin()
	s.reader.setBusy(false)

	if agent.IsCanceled(err) {
		// 跟 Claude Code 一样：没来得及答复的那条话退回输入框，
		// 用户可以改一改再发，不用重新敲一遍。
		note("已打断，刚才那句话退回输入框了")
		s.ag.NoteInterrupted()
		s.reader.insert(input)
	} else if err != nil {
		fail("%v", err)
	}
	_ = answer // 正文已经在可重画区里流完并定格了
}

// enqueue 把一句话排进队列。
func (s *Session) enqueue(line string) {
	s.queueMu.Lock()
	s.queued = append(s.queued, line)
	s.queueMu.Unlock()
}

// dequeue 取出排队里的下一句；没有就返回空。
func (s *Session) dequeue() string {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if len(s.queued) == 0 {
		return ""
	}
	line := s.queued[0]
	s.queued = s.queued[1:]
	return line
}

// queueLen 是还排着几句，状态栏要显示。
func (s *Session) queueLen() int {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	return len(s.queued)
}

// decide 消化确认框里的一次按键。选定后放行或否决挂起的动作。
func (s *Session) decide(key string) {
	s.pendingMu.Lock()
	p, c := s.pending, s.box
	s.pendingMu.Unlock()
	if p == nil || c == nil {
		return
	}

	switch key {
	case "up":
		c.move(-1)
		return
	case "down":
		c.move(1)
		return
	case "esc":
		c.sel = len(c.options) - 1 // 落到「取消」那一项上
		scr.refresh()
	case "enter":
	default:
		n := 0
		fmt.Sscanf(key, "%d", &n)
		if !c.pick(n) {
			return
		}
	}

	c.done()

	ok, level := c.result()
	if ok && level != nil && s.ag != nil {
		s.ag.SetAuto(*level)
		note("档位已切到「%s」", level.Label())
	}

	s.pendingMu.Lock()
	s.pending, s.box = nil, nil
	s.pendingMu.Unlock()

	s.reader.setPicking(false)
	s.reader.setBusy(true)
	scr.startSpin()
	p.Reply <- ok
}

func (s *Session) emit(e agent.Event) {
	if s.ag != nil {
		scr.setTokens(s.ag.Usage.TotalTokens)
	}
	switch e.Kind {
	case agent.EventDelta:
		// live 没开着说明这是这段回复的头一个字，先空一行再起个圆点。
		if !s.live.on {
			scr.stopSpin()
			out("\n")
			s.live.start(glyphOf(lvAgent))
		}
		s.live.feed(e.Text)
	case agent.EventReasoning:
		s.live.freeze()
		out(dim + e.Text + reset)
	case agent.EventThinking:
		scr.stopSpin()
		s.live.start(glyphOf(lvAgent))
		s.live.feed(e.Text)
		s.live.freeze()
	case agent.EventToolCall:
		s.live.freeze()
		out("\n")
		msg(lvTool, "%s%s%s", bold, toolLabel(e.Tool), reset+dim+callArgs(e.Text)+reset)
		scr.startSpin() // 又开始干活了，转圈接着转
	case agent.EventToolResult:
		// 结果为空也会发一条（给 ACP 收卡片用），终端上空行没意义。
		if e.Text != "" {
			msg(lvResult, "%s", e.Text)
		}
	case agent.EventError:
		// 拒绝是用户自己在确认框里点的，不当错误再报一遍。
		if !e.Declined {
			msg(lvError, "%s", e.Text)
		}
	case agent.EventAuto:
		note("已自动执行：%s", e.Text)
	case agent.EventCompact:
		msg(lvSystem, "%s", e.Text)
	case agent.EventPending:
		s.live.freeze()
		scr.stopSpin()
		c := newConfirm(e.Pending)
		s.pendingMu.Lock()
		s.pending, s.box = e.Pending, c
		s.pendingMu.Unlock()
		c.show()
		s.reader.setPicking(true)
		s.reader.setBusy(false)
	}
}

// toolLabel 把工具名翻成界面上显示的中文。
//
// 表在 agent 包里（只此一份，见 agent/label.go）——ACP 的工具卡片标题也用它，
// 各写一份的结果是两边慢慢走散。
func toolLabel(name string) string { return agent.ToolLabel(name) }

func callArgs(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return "(" + text + ")"
}
