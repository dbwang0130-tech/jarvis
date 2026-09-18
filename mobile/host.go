package mobile

import "github.com/dabao/jarvis/internal/agent"

// Host 是界面那头实现的回调。gomobile 支持把 Go 接口交给 Swift 实现，
// 于是 Go 这边干活的过程能实时报上去，要拿主意的事也能回头问。
//
// 两个方法都在后台线程上被调用，**Confirm 会一直阻塞到用户点了为止**——
// 界面那头要把弹窗切到主线程，然后让这个调用等着。
type Host interface {
	// Step 报告一步进展。kind 是 tool / result / error。
	// label 是工具名的中文说法（agent.ToolLabel 查好的），界面直接显示。
	Step(kind, name, label, text string)
	// Confirm 问用户批不批。返回 false 就是不批，这一步不会执行。
	//
	// tool 给的是**中文标签**（agent.ToolLabel 查好的），界面直接显示——
	// 界面那边不该再维护一张工具名对照表，两份表迟早走散。
	// irreversible 为真表示做了收不回来（比如发信）。
	Confirm(tool, title, detail string, irreversible bool) bool
}

// hostEmit 把 agent 的事件翻成对 Host 的调用。
//
// host 为 nil 时（比如测试、或者老版本界面）走安全的那条路：
// 要确认的一律不批。**绝不能不管**——p.Reply 是个带缓冲的 channel，
// emit 返回后 agent 立刻阻塞在上面收答复，没人写就是整轮卡死。
func hostEmit(host Host, steps *[]Step) func(agent.Event) {
	return func(e agent.Event) {
		switch e.Kind {
		case agent.EventToolCall:
			record(steps, host, "tool", e.Tool, e.Text)
		case agent.EventToolResult:
			record(steps, host, "result", e.Tool, e.Text)
		case agent.EventError:
			record(steps, host, "error", e.Tool, e.Text)
		case agent.EventAuto:
			// 可逆的动作不问就做了，但得让人看见做了什么。
			record(steps, host, "auto", e.Tool, e.Text)
		case agent.EventPending:
			p := e.Pending
			if p == nil {
				return
			}
			ok := false
			if host != nil {
				ok = host.Confirm(agent.ToolLabel(p.Tool), p.Title, p.Detail,
					p.Risk == agent.RiskIrreversible)
			}
			if !ok {
				record(steps, host, "error", p.Tool, "没批准："+p.Title)
			}
			p.Reply <- ok
		}
	}
}

func record(steps *[]Step, host Host, kind, name, text string) {
	label := agent.ToolLabel(name)
	*steps = append(*steps, Step{Kind: kind, Name: name, Label: label, Text: text})
	if host != nil {
		host.Step(kind, name, label, text)
	}
}
