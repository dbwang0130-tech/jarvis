package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap 是全局键位表，风格贴近 org-agenda。
type keyMap struct {
	Up, Down       key.Binding
	Top, Bottom    key.Binding
	PageUp, PgDown key.Binding

	NextView key.Binding
	Agenda   key.Binding
	Todos    key.Binding
	Mail     key.Binding
	Ledger   key.Binding
	Chat     key.Binding
	// Say 在秘书面板的浏览状态下重新聚焦输入框。
	Say key.Binding

	// SyncMail 同步新邮件，LoadMoreMail 往前翻页，AutoLevel 切换自动档。
	SyncMail, LoadMoreMail, AutoLevel key.Binding
	// ToggleMouse 开关鼠标捕获，关掉后可用鼠标选择文字。
	ToggleMouse key.Binding

	DayView, WeekView, MonthView key.Binding
	Forward, Back, Today         key.Binding

	Cycle, State, Sched, Dead, Prio key.Binding
	// Repeat 设置条目的循环规则。
	Repeat                         key.Binding
	ClockIn, ClockOut              key.Binding
	Capture, Search, Refresh, Undo key.Binding
	// LogMode 切换是否显示当天完成的条目，对应 org 的 log 模式。
	LogMode key.Binding
	// Detail 打开条目详情，Toggle 勾选详情里的复选框，Delete 删除条目。
	Detail, Toggle, Delete key.Binding
	// Edit 用外部编辑器改条目内容。
	Edit key.Binding

	Help, Quit, Cancel key.Binding
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "上下移动")),
	Down:     key.NewBinding(key.WithKeys("j", "down")),
	Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "首/末行")),
	Bottom:   key.NewBinding(key.WithKeys("G", "end")),
	PageUp:   key.NewBinding(key.WithKeys("ctrl+u", "pgup")),
	PgDown:   key.NewBinding(key.WithKeys("ctrl+d", "pgdown")),
	NextView: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "切换视图")),
	Chat:     key.NewBinding(key.WithKeys("1", "a"), key.WithHelp("a", "回到秘书")),
	Agenda:   key.NewBinding(key.WithKeys("2")),
	Todos:    key.NewBinding(key.WithKeys("3")),
	Mail:     key.NewBinding(key.WithKeys("4")),
	Ledger:   key.NewBinding(key.WithKeys("5")),
	Say:      key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "继续说话")),

	SyncMail:     key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "同步邮件")),
	LoadMoreMail: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "加载更早邮件")),
	AutoLevel:    key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "切换自动档")),
	ToggleMouse:  key.NewBinding(key.WithKeys("f2"), key.WithHelp("f2", "开关鼠标捕获")),

	DayView:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d/w/m", "日/周/月")),
	WeekView:  key.NewBinding(key.WithKeys("w")),
	MonthView: key.NewBinding(key.WithKeys("m")),
	Forward:   key.NewBinding(key.WithKeys("f", "]"), key.WithHelp("f/b", "前后翻页")),
	Back:      key.NewBinding(key.WithKeys("b", "[")),
	Today:     key.NewBinding(key.WithKeys("."), key.WithHelp(".", "回到今天")),

	Cycle:    key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "切换状态")),
	State:    key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "指定状态")),
	Sched:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "设置计划")),
	Dead:     key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "设置截止")),
	Prio:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "优先级")),
	Repeat:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "设为循环任务")),
	ClockIn:  key.NewBinding(key.WithKeys("I"), key.WithHelp("I/O", "开始/结束计时")),
	ClockOut: key.NewBinding(key.WithKeys("O")),
	Capture:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "新建条目")),
	Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "搜索")),
	Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "重新加载")),
	Undo:     key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "撤销秘书改动")),
	LogMode:  key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "显示当天完成的条目")),
	Detail:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "查看详情")),
	Toggle:   key.NewBinding(key.WithKeys("x", " "), key.WithHelp("x", "勾选复选框")),
	Delete:   key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "删除条目")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "用编辑器改内容")),

	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "帮助")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "退出")),
	Cancel: key.NewBinding(key.WithKeys("esc")),
}

// helpRows 是帮助面板的内容。秘书是主界面，所以它的键位排在最前。
var helpRows = [][2]string{
	{"直接打字", "对秘书说话，回车发送"},
	{"/login /help", "秘书面板里的命令，/help 看全部"},
	{"esc", "秘书说话时打断它；否则松开输入框"},
	{"i", "回到输入框继续说话"},
	{"tab", "在秘书、日程、待办、邮件、财务之间轮换"},
	{"1 / 2 / 3 / 4 / 5", "直接切到秘书 / 日程 / 待办 / 邮件 / 财务"},
	{"ctrl+r / ctrl+n", "同步新邮件 / 往前加载更早邮件"},
	{"ctrl+a", "切换邮件自动档：逐项确认 → 自动整理 → 全自动"},
	{"f2", "开关鼠标捕获：关掉后可用鼠标直接选择文字"},
	{"j / k", "上下移动"},
	{"g / G", "跳到首行 / 末行"},
	{"d / w / m", "日视图 / 周视图 / 月视图"},
	{"f / b", "后翻 / 前翻一个周期"},
	{".", "回到今天"},
	{"t", "循环切换 TODO 状态"},
	{"T", "输入指定状态"},
	{"s", "设置计划时间（SCHEDULED）"},
	{"D", "设置截止时间（DEADLINE）"},
	{"p", "设置优先级"},
	{"R", "设为循环任务，如 +1w、每周、.+3d；留空取消"},
	{"I / O", "开始 / 结束计时"},
	{"c", "新建条目"},
	{"/", "按关键词过滤"},
	{"l", "显示或隐藏当天完成的条目"},
	{"enter", "查看条目详情：正文、复选框、链接、子条目"},
	{"x", "详情里勾选复选框"},
	{"X", "删除条目及其子树，删完可以按 u 恢复"},
	{"e", "用 $EDITOR 编辑整棵子树，存盘后自动读回"},
	{"a", "从任何地方回到秘书"},
	{"u", "撤销最近的改动（秘书的或自己删的）"},
	{"r", "重新加载 org 文件"},
	{"q / ctrl+c", "退出（输入框里用 ctrl+c 或 /quit）"},
}
