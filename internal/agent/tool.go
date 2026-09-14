// Package agent 是 Jarvis 的核心：一个由模型驱动的工具调用循环。
//
// 它不关心具体业务——日程、邮件、以及未来接入的任何场景，都以
// Skill（技能）的形式插进来。每个 Skill 声明一组 Tool，并贡献自己的
// 系统提示与执行逻辑。核心循环只负责：问模型、执行工具、处理风险分级
// 与确认挂起、压缩上下文。
package agent

import "strings"

// Risk 是一个动作的风险等级，决定自动模式下它能不能免确认放行。
type Risk int

const (
	// RiskNone 只读，或只在本地生成东西，随便跑。
	RiskNone Risk = iota
	// RiskReversible 会改动外部状态，但能挽回（比如移进垃圾箱）。
	RiskReversible
	// RiskIrreversible 收不回来（比如把信发出去）。
	RiskIrreversible
)

// AutoLevel 决定哪些动作可以不经确认直接做。
type AutoLevel int

const (
	// AutoOff 任何有风险的动作都要点头。
	AutoOff AutoLevel = iota
	// AutoClean 可逆动作自动做，不可逆仍要点头。
	AutoClean
	// AutoAll 全部自动。
	AutoAll
)

// Allows 判断这个档位下某个风险等级能不能免确认。
func (l AutoLevel) Allows(r Risk) bool {
	switch l {
	case AutoAll:
		return true
	case AutoClean:
		return r <= RiskReversible
	}
	return r == RiskNone
}

// Label 是展示用的说法。
func (l AutoLevel) Label() string {
	switch l {
	case AutoAll:
		return "全自动"
	case AutoClean:
		return "自动整理"
	}
	return "逐项确认"
}

// ParseAutoLevel 解析配置里的写法。
func ParseAutoLevel(s string) AutoLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "all", "full", "yes", "true":
		return AutoAll
	case "clean", "tidy", "auto":
		return AutoClean
	}
	return AutoOff
}

// ToolSpec 是交给模型的工具声明。
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Tool 是秘书能调用的一个动作。Provider 负责实现它。
type Tool interface {
	// Spec 返回交给模型的声明。
	Spec() ToolSpec
	// Risk 告诉核心循环这个动作要不要确认。
	Risk() Risk
	// Preview 在确认前生成给人看的标题和详情；RiskNone 的工具可返回空。
	Preview(args string) (title, detail string, err error)
	// Run 真正执行。changed 表示是否改动了本地文件（界面据此刷新）。
	Run(args string) (result string, changed bool, err error)
}

// Pending 是一个等待用户确认的动作。
type Pending struct {
	Tool   string
	Title  string
	Detail string
	// Reply 是界面回传是否批准的通道（缓冲 1，发送不阻塞）。
	Reply chan bool
	run   func() (string, bool, error)
}

// Do 在用户批准后执行该动作。
func (p *Pending) Do() (string, bool, error) { return p.run() }

// Skill 是一项技能，向秘书贡献工具、环境描述和行为规则。
// 新增场景时实现这个接口即可接入，无需改动核心循环。
type Skill interface {
	// Name 是技能名，用于展示与日志。
	Name() string
	// Describe 把当前环境快照追加进系统提示（如文件列表、邮箱账号）。
	Describe(b *strings.Builder)
	// Instructions 返回该技能的行为规则。
	Instructions() string
	// Tools 返回该技能提供的全部工具。
	Tools() []Tool
}
