// Package eval 拿**真模型**跑固定场景，断言它实际调了哪些工具。
//
// 这是唯一能发现「提示改了、行为退了」的手段。别的测试都给模型写好台词
// （假 http.RoundTripper 放脚本），于是提示词改一个字都不会有任何测试响——
// 而这个 app 的行为几乎全长在提示里：`internal/chat/testdata/prompt.txt` 那份
// 金标只能证明「提示变了」，证明不了「变得更好还是更糟」。
//
// **要钱、要网、结果不完全确定，所以不进 make test。** 跑法见 `make eval`。
//
// 断言写成**性质**而不是逐字对照：模型换个说法是正常的，「问今天安排必须先查、
// 不许直接新建条目」这种才是不该变的。一条场景失败不一定是回归——先看它到底
// 调了什么（跑的时候会全部打印出来），再决定是模型抖动还是提示真写坏了。
package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/health"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
	"github.com/dabao/jarvis/internal/persona"
	extskills "github.com/dabao/jarvis/internal/skills"
	"github.com/dabao/jarvis/internal/skillset"
)

// Case 是一个场景。
type Case struct {
	// Name 一句话说清这条在测什么性质。
	Name string
	// Org 是 org 文件，键是文件名。留空就给一个空日程。
	Org map[string]string
	// Ledger 是账本全文，留空就没有账本。
	Ledger string
	// Ask 是用户说的那句话。
	Ask string
	// Persona 是这一轮的人设，留空就用内置的。
	Persona string
	// Skills 是 skills/ 下的扩展技能，键是目录名、值是 SKILL.md 全文。
	Skills map[string]string
	// Want 断言这一轮的表现；返回非空字符串表示不合格，内容是哪儿不对。
	Want func(r Result) string
}

// Result 是跑完一轮之后能看见的东西。
type Result struct {
	// Tools 是按顺序调过的工具名。
	Tools []string
	// Reply 是最后那句答复。
	Reply string
	// Files 是跑完之后各 org 文件的内容（工具真改了什么，看这个）。
	Files map[string]string
	// Book 是跑完之后的账本全文。
	Book string
	// Persona 是跑完之后人设文件的内容——「它说改了」和「文件真变了」
	// 是两件事，这一项就是为了分开这两件事。
	Persona string
	// Err 非空表示这一轮没跑完。
	Err error
}

// Used 报告调过 name。
func (r Result) Used(name string) bool {
	for _, t := range r.Tools {
		if t == name {
			return true
		}
	}
	return false
}

// Order 报告 a 在 b 之前调过（两个都得调过）。
func (r Result) Order(a, b string) bool {
	ai, bi := -1, -1
	for i, t := range r.Tools {
		if t == a && ai < 0 {
			ai = i
		}
		if t == b && bi < 0 {
			bi = i
		}
	}
	return ai >= 0 && bi >= 0 && ai < bi
}

// Run 跑一条场景。
//
// **所有文件都在临时目录里**，一个字都不会碰到 iCloud 上真的日程和账本——
// 评测会让模型真去改文件，跑在真数据上就是拿自己的日程当试验田。
func Run(ctx context.Context, cfg config.Config, c Case, now time.Time) Result {
	dir, err := os.MkdirTemp("", "jarvis-eval-")
	if err != nil {
		return Result{Err: err}
	}
	defer os.RemoveAll(dir)

	orgDir := filepath.Join(dir, "org")
	if err := os.MkdirAll(orgDir, 0o700); err != nil {
		return Result{Err: err}
	}
	files := c.Org
	if len(files) == 0 {
		files = map[string]string{"work.org": "#+TITLE: 工作\n"}
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(orgDir, name), []byte(body), 0o600); err != nil {
			return Result{Err: err}
		}
	}
	ledgerPath := filepath.Join(dir, "ledger.dat")
	if err := os.WriteFile(ledgerPath, []byte(c.Ledger), 0o600); err != nil {
		return Result{Err: err}
	}

	// 配置照抄一份，只把路径换成临时的——模型那几项（服务商、密钥）要用真的。
	cfg.Files = []string{orgDir}
	cfg.Inbox = filepath.Join(orgDir, "inbox.org")
	cfg.LedgerFile = ledgerPath
	cfg.HealthFile = filepath.Join(dir, "health.ndjson")
	cfg.MemoryFile = filepath.Join(dir, "memory.md")
	cfg.SkillsDir = filepath.Join(dir, "skills")
	cfg.PersonaFile = filepath.Join(dir, "persona.md")

	// 扩展技能：一个子目录一份 SKILL.md，跟真实布局一样。
	for name, body := range c.Skills {
		sub := filepath.Join(cfg.SkillsDir, name)
		if err := os.MkdirAll(sub, 0o700); err != nil {
			return Result{Err: err}
		}
		if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(body), 0o600); err != nil {
			return Result{Err: err}
		}
	}
	// 评测不该弹确认框，也不该真去连邮箱和 Chrome。
	cfg.Accounts = nil

	coll, err := org.Load(orgDir)
	if err != nil {
		return Result{Err: err}
	}
	ws := orgtools.NewWorkspace(coll, cfg)
	ws.Now = func() time.Time { return now }

	skills := skillset.Build(skillset.Deps{
		Org:    ws,
		Ledger: ledger.NewSkill(ledgerPath),
		Health: health.New(cfg.HealthFile),
		Memory: memory.New(cfg.MemoryFile),
		// 邮件和浏览器都要真连外部的东西，评测里一律不带。
	})
	ag, err := agent.Build(cfg, skills...)
	if err != nil {
		return Result{Err: err}
	}
	ag.Now = func() time.Time { return now }
	ag.Stream = false

	// 人设和扩展技能都要接上——它们是这一轮会话新做的两样东西，而装配错了的
	// 表现是「功能悄悄不在了」，编译和别的测试都不会响。
	personaPath := cfg.PersonaPath()
	if err := persona.Save(personaPath, c.Persona); err != nil {
		return Result{Err: err}
	}
	ag.SetPersona(persona.Load(personaPath), func(text string) error {
		return persona.Save(personaPath, text)
	})
	extra, _ := extskills.Load(cfg.SkillsPath())
	ag.SetExtraSkills(extra)
	// 全自动：评测里没有人来点头，挂在确认框上等于超时。
	ag.Auto = agent.AutoAll

	var r Result
	reply, err := ag.Ask(ctx, c.Ask, func(e agent.Event) {
		if e.Kind == agent.EventToolCall {
			r.Tools = append(r.Tools, e.Tool)
		}
	})
	r.Reply, r.Err = reply, err

	// 把模型改成什么样读回来——「它说改了」和「文件真变了」是两件事。
	r.Files = map[string]string{}
	for name := range files {
		if raw, err := os.ReadFile(filepath.Join(orgDir, name)); err == nil {
			r.Files[name] = string(raw)
		}
	}
	if raw, err := os.ReadFile(ledgerPath); err == nil {
		r.Book = string(raw)
	}
	r.Persona = persona.Load(personaPath)
	return r
}

// Describe 把一轮的表现拼成一段能读的话，失败时打出来看。
func (r Result) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "调用顺序：%s\n", strings.Join(r.Tools, " → "))
	if r.Err != nil {
		fmt.Fprintf(&b, "出错：%v\n", r.Err)
	}
	fmt.Fprintf(&b, "答复：%s\n", strings.TrimSpace(r.Reply))
	return b.String()
}
