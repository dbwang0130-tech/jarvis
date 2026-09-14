package ui

import "github.com/charmbracelet/lipgloss"

// 配色沿用 org-agenda 的视觉习惯：状态用色区分，逾期与截止最醒目。
var (
	colFg       = lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#e6e6e6"}
	colDim      = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#8b8b8b"}
	colAccent   = lipgloss.AdaptiveColor{Light: "#0550ae", Dark: "#79b8ff"}
	colToday    = lipgloss.AdaptiveColor{Light: "#953800", Dark: "#ffab70"}
	colDanger   = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
	colWarn     = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e3b341"}
	colDone     = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#7ee787"}
	colSelectBg = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#2d333b"}
	colBorder   = lipgloss.AdaptiveColor{Light: "#c9d1d9", Dark: "#444c56"}

	styTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styDim   = lipgloss.NewStyle().Foreground(colDim)
	styBase  = lipgloss.NewStyle().Foreground(colFg)

	styDayHeader   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styTodayHeader = lipgloss.NewStyle().Bold(true).Foreground(colToday)
	stySelected    = lipgloss.NewStyle().Background(colSelectBg).Bold(true)

	styTodo     = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	styNext     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styWaiting  = lipgloss.NewStyle().Foreground(colDim).Bold(true)
	styDoneKw   = lipgloss.NewStyle().Foreground(colDone).Bold(true)
	styPriority = lipgloss.NewStyle().Foreground(colDanger).Bold(true)
	styTag      = lipgloss.NewStyle().Foreground(colDim)
	styRepeat   = lipgloss.NewStyle().Foreground(colDone)
	styAccent   = lipgloss.NewStyle().Foreground(colAccent)
	styWarn     = lipgloss.NewStyle().Foreground(colWarn)
	styOverdue  = lipgloss.NewStyle().Foreground(colDanger)
	styTime     = lipgloss.NewStyle().Foreground(colAccent)

	styStatus  = lipgloss.NewStyle().Foreground(colDim)
	styError   = lipgloss.NewStyle().Foreground(colDanger).Bold(true)
	styOK      = lipgloss.NewStyle().Foreground(colDone)
	styPrompt  = lipgloss.NewStyle().Foreground(colToday).Bold(true)
	styUser    = lipgloss.NewStyle().Foreground(colDim)
	styAgent   = lipgloss.NewStyle().Foreground(colFg)
	styToolLog = lipgloss.NewStyle().Foreground(colDim)

	// 顶栏与对话流标记，向 Claude Code 靠拢。
	styHeaderTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styHeaderRule  = lipgloss.NewStyle().Foreground(colBorder)
	styUserMark    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styUserText    = lipgloss.NewStyle().Foreground(colFg)
	styToolCall    = lipgloss.NewStyle().Foreground(colAccent)

	// 财务页专用：净资产概览、账户分组、正负金额配色。
	styFinanceNet     = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styFinanceGroup   = lipgloss.NewStyle().Bold(true).Foreground(colFg)
	styFinanceSection = lipgloss.NewStyle().Bold(true).Foreground(colToday).Padding(0, 1)
	styFinancePos     = lipgloss.NewStyle().Foreground(colDone)
	styFinanceNeg     = lipgloss.NewStyle().Foreground(colDanger)

	// 对话流的行首标记，沿用 Claude Code 的观感：
	// 用户输入用 >，一次动作用 ●，动作的结果挂在 ⎿ 下面。
	styBullet     = lipgloss.NewStyle().Foreground(colFg)
	styBulletTool = lipgloss.NewStyle().Foreground(colAccent)
	styBranch     = lipgloss.NewStyle().Foreground(colDim)
	styInputBox   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	styInputMark  = lipgloss.NewStyle().Foreground(colDim)

	styTabActive   = lipgloss.NewStyle().Bold(true).Foreground(colFg).Background(colSelectBg).Padding(0, 1)
	styTabInactive = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)
)

// keywordStyle 按 TODO 关键字返回对应样式。
func keywordStyle(kw string, done bool) lipgloss.Style {
	switch {
	case done:
		return styDoneKw
	case kw == "NEXT":
		return styNext
	case kw == "WAITING":
		return styWaiting
	default:
		return styTodo
	}
}
