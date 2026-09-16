package chat

import (
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
)

// choice 是确认框里的一个选项。
type choice struct {
	label string
	// deny 为真表示这一项是否决。
	deny bool
	// level 非 nil 时表示选中后把自动档提到这一级（「以后不用问」）。
	level *agent.AutoLevel
}

// confirm 是一个可用方向键挑选的确认框：
// ↑↓ 移动、数字直选、回车确认、esc 取消。
type confirm struct {
	title    string
	detail   string
	question string
	options  []choice
	sel      int
}

// newConfirm 按动作的风险等级组织选项。
func newConfirm(p *agent.Pending) *confirm {
	c := &confirm{
		title:    p.Title,
		detail:   p.Detail,
		question: "要执行吗？",
		options:  []choice{{label: "确认"}},
	}
	switch p.Risk {
	case agent.RiskReversible:
		lv := agent.AutoClean
		c.options = append(c.options, choice{label: "确认，以后可逆的动作不必问我", level: &lv})
	case agent.RiskIrreversible:
		lv := agent.AutoAll
		c.options = append(c.options, choice{label: "确认，以后什么都不必问我（发信也是）", level: &lv})
	}
	c.options = append(c.options, choice{label: "取消，我另有交代", deny: true})
	return c
}

// lines 渲染出整框内容。
func (c *confirm) lines() []string {
	w := frameWidth() - 2
	if w > 76 {
		w = 76
	}
	inner := w - 4

	var body []string
	if d := strings.TrimSpace(c.detail); d != "" {
		body = append(body, "")
		for _, l := range strings.Split(d, "\n") {
			body = append(body, wrapANSI(l, inner)...)
		}
	}
	body = append(body, "", c.question)
	for i, o := range c.options {
		text := fmt.Sprintf("%d. %s", i+1, o.label)
		if i == c.sel {
			body = append(body, coral+"❯ "+text+reset)
		} else {
			body = append(body, "  "+dim+text+reset)
		}
	}

	rows := make([]string, 0, len(body)+1)
	for _, l := range append([]string{bold + c.title + reset}, body...) {
		rows = append(rows, padVis(l, inner))
	}
	return roundBox(rows, w)
}

// show 把确认框挂到底部那块区域上。挂着的时候不画输入框——
// 这会儿按键是用来挑选项的，不是打字。
func (c *confirm) show() {
	scr.confirmLines = c.lines
	scr.refresh()
}

// done 收工：把最后的样子定格成历史，然后把区域还给输入框。
func (c *confirm) done() {
	lines := c.lines()
	scr.confirmLines = nil
	scr.commit(lines)
}

// move 上下挪选择。
func (c *confirm) move(d int) {
	c.sel = (c.sel + d + len(c.options)) % len(c.options)
	scr.refresh()
}

// pick 按编号选中（1 起）；越界返回 false。
func (c *confirm) pick(n int) bool {
	if n < 1 || n > len(c.options) {
		return false
	}
	c.sel = n - 1
	scr.refresh()
	return true
}

// result 返回当前选项是否批准，以及要不要顺手提档。
func (c *confirm) result() (bool, *agent.AutoLevel) {
	o := c.options[c.sel]
	if o.deny {
		return false, nil
	}
	return true, o.level
}
