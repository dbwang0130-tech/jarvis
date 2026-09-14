// jarvis 是个人助理：一个终端同时管理日程（org-mode）与邮箱。
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/configskill"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/mail"
	"github.com/dabao/jarvis/internal/orgtools"
	"github.com/dabao/jarvis/internal/ui"
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "jarvis: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `jarvis — 个人助理：日程 + 邮箱，一个终端搞定

用法：
  jarvis

启动后直接在秘书面板说话。所有配置都在界面里用 / 命令完成：
  /login  /provider  /model    模型与密钥
  /mail  /accounts  /mailrm    邮箱账号
  /files /inbox                org 文件
  /ledger [路径]               账本（ledger-cli 格式）
  /help                        全部命令
`)
}

func run() error {
	cfgPath := config.Path()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	coll, err := ui.LoadOrg(cfg)
	if err != nil {
		return err
	}
	coll.SetLogStates(cfg.LogStateChanges)

	orgWS := orgtools.NewWorkspace(coll, cfg)
	mailS := mail.NewSession(cfg)
	ledgerSkill := ledger.NewSkill(cfg.LedgerFile)

	skills := []agent.Skill{
		orgtools.NewSkill(orgWS),
		mail.NewSkill(mailS),
		ledger.NewSkill(cfg.LedgerFile),
		configskill.New(cfgPath),
	}
	ag, agentErr := agent.Build(cfg, skills...)

	prog := tea.NewProgram(
		ui.New(cfgPath, cfg, coll, orgWS, mailS, ledgerSkill, skills, ag, agentErr),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = prog.Run()
	return err
}
