package orgtools

import (
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/org"
)

// 下面这几行是**别名，不是第二份实现**——指向 agent 里那一份唯一定义
// （见 agent/schema.go）。原先每个技能包各写一份函数体，而 num 已经在
// ledger/health 和 mail/orgtools 之间漂成了 number 和 integer 两种。
var (
	schema      = agent.Obj
	schemaOneOf = agent.ObjAnyOf
	str         = agent.Str
	flag        = agent.Flag
)

// orgTool 是日程工具对 agent.Tool 的适配：只读、无风险、执行交给 execOrg。
type orgTool struct {
	spec     agent.ToolSpec
	exec     func(args string) (string, bool, error)
	readonly bool
}

func (t orgTool) Spec() agent.ToolSpec                   { return t.spec }
func (t orgTool) Risk() agent.Risk                       { return agent.RiskNone }
func (t orgTool) Preview(string) (string, string, error) { return "", "", nil }
func (t orgTool) Run(args string) (string, bool, error)  { return t.exec(args) }
func (t orgTool) ReadOnly() bool                         { return t.readonly }

// wsToolDefs 返回日程工具的全部声明。
func wsToolDefs() []agent.ToolSpec {
	return []agent.ToolSpec{
		{
			Name:        "list_agenda",
			Description: "查看某段日期范围内的日程安排，包含计划、截止、事件与逾期项。用于了解用户当前的时间占用情况。",
			Parameters: schema(map[string]any{
				"start_date": str("起始日期，格式 2006-01-02，留空表示今天"),
				"days":       agent.Int("天数，默认 7"),
			}),
		},
		{
			Name:        "list_todos",
			Description: "查看待办事项列表，可按状态、标签、关键词或类别筛选。返回的 ref 字段用于后续修改操作。",
			Parameters: schema(map[string]any{
				"query":        str("标题关键词"),
				"state":        str("TODO 状态，如 TODO、NEXT、WAITING"),
				"tag":          str("标签名"),
				"category":     str("所属类别（通常是文件名或 #+CATEGORY）"),
				"include_done": flag("是否包含已完成条目，默认否"),
			}),
		},
		{
			Name:        "find_free_slots",
			Description: "查看某天在可安排时段内的空闲时间段，用于给任务找合适的时间。",
			Parameters: schema(map[string]any{
				"date":        str("日期，格式 2006-01-02，留空表示今天"),
				"min_minutes": agent.Int("只返回不短于该分钟数的空档，默认 30"),
			}, "date"),
		},
		{
			Name:        "get_item",
			Description: "查看单个条目的详细信息，包括计划时间、截止时间、标签、属性与正文。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验，建议一并提供"),
			}, "ref"),
		},
		{
			Name: "update_item",
			Description: "修改已有条目：状态、优先级、计划时间、截止时间、循环规则，一次可改多项。" +
				"这几项跟 create_item 同名同义。只传要改的那几项——**没传的保持不变，" +
				"传空字符串才是清除**（scheduled 传空就是取消计划）。要改好几项就一次带上，" +
				"每调一次 ref 都会变。",
			Parameters: schema(map[string]any{
				"ref":       str("条目引用，形如 work.org:12"),
				"title":     str("条目标题，用于校验"),
				"state":     str("TODO 状态，如 TODO、NEXT、WAITING、DONE、CANCELLED；空表示去掉状态"),
				"priority":  str("优先级 A、B 或 C；空表示去掉优先级"),
				"scheduled": str("计划时间，格式 2006-01-02、2006-01-02 15:04 或 2006-01-02 15:04-16:30；空表示取消计划"),
				"deadline":  str("截止时间，格式 2006-01-02 或 2006-01-02 15:04；空表示取消截止"),
				"repeat":    str("循环规则：+1d 每天、+1w 每周、+1m 每月、.+3d 表示完成后再隔三天；空表示取消循环。条目需要先有计划或截止时间"),
			}, "ref"),
		},
		{
			Name: "delete_item",
			Description: "删除一个条目及其下属的全部子条目。这是破坏性操作，只在用户明确要求删除时使用；" +
				"如果只是做完了，应该用 update_item 把 state 标成完成。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，必须提供，用于确认删对了条目"),
			}, "ref", "title"),
		},
		{
			Name:        "create_item",
			Description: "新建一个待办条目。用户提出新任务时用这个。",
			Parameters: schema(map[string]any{
				"title":     str("条目标题"),
				"state":     str("TODO 状态，不传默认 TODO；明确传空字符串则建一条不带状态的普通条目"),
				"priority":  str("优先级 A、B 或 C"),
				"scheduled": str("计划时间，格式 2006-01-02、2006-01-02 15:04 或 2006-01-02 15:04-16:30"),
				"deadline":  str("截止时间，格式 2006-01-02 或 2006-01-02 15:04"),
				"repeat":    str("循环规则，如 +1w 表示每周重复；留空表示不循环"),
				"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "标签列表"},
				"body":      str("正文说明，可多行"),
				"file":      str("写入哪个文件，如 work.org；留空则写入默认收件箱"),
				"parent":    str("父条目引用，形如 work.org:5；给了就挂到它下面成为子条目，此时 file 忽略"),
			}, "title"),
		},
		{
			Name:        "clock_in",
			Description: "开始为某个条目计时，写入 LOGBOOK 的 CLOCK 记录。用户说「开始计时/开始做这件事」时用。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
			}, "ref"),
		},
		{
			Name:        "clock_out",
			Description: "结束某个条目的计时，补全 LOGBOOK 里的 CLOCK 记录。用户说「停止计时/做完了」时用。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
			}, "ref"),
		},
		{
			Name:        "add_note",
			Description: "在条目的 LOGBOOK 里记一条笔记。用户说「记一下进度/备注一下」时用。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"text":  str("笔记内容"),
			}, "ref", "text"),
		},
		{
			Name: "toggle_checkbox",
			Description: "勾上或取消条目正文里的某个复选框（`- [ ] 买菜` 那种），" +
				"标题上的 [n/m] 进度会跟着变。用户说「第二项做完了」时用。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"index": agent.Int("第几个复选框，从 1 数起"),
			}, "ref", "index"),
		},
		{
			Name: "move_item",
			Description: "调整条目及其子条目在大纲里的位置。二选一：" +
				"给 parent_ref 就挂到那个父条目下（传空字符串则移到文件顶层末尾）；" +
				"给 level_delta 就原地升降级（+1 降一级变成更深的子任务，-1 升一级）。",
			Parameters: schemaOneOf(map[string]any{
				"ref":         str("要移动的条目引用，形如 work.org:12"),
				"title":       str("条目标题，用于校验"),
				"parent_ref":  str("目标父条目引用，形如 work.org:5；传空字符串移到顶层末尾"),
				"level_delta": agent.Int("升降几级：+1 降一级，-1 升一级"),
			}, []string{"ref"}, "parent_ref", "level_delta"),
		},
	}
}

// execOrg 执行日程工具，按名称分发。
func (w *Workspace) execOrg(name, rawArgs string) (string, bool, error) {
	a, err := parseArgs(rawArgs)
	if err != nil {
		return "", false, err
	}

	switch name {
	case "list_agenda":
		return w.listAgenda(a.Str("start_date"), a.Int("days", w.Cfg.AgendaDays))
	case "list_todos":
		return w.listTodos(agenda.TodoFilter{
			Query:       a.Str("query"),
			Category:    a.Str("category"),
			IncludeDone: a.Bool("include_done"),
			Keywords:    nonEmptyList(a.Str("state")),
			Tags:        nonEmptyList(a.Str("tag")),
		})
	case "find_free_slots":
		return w.findFree(a.Str("date"), a.Int("min_minutes", 30))
	case "get_item":
		return w.getItem(a.Str("ref"), a.Str("title"))
	case "update_item":
		return w.updateItem(a)
	case "delete_item":
		return w.deleteItem(a.Str("ref"), a.Str("title"))
	case "create_item":
		return w.createItem(a)
	case "clock_in":
		return w.clockIn(a.Str("ref"), a.Str("title"))
	case "clock_out":
		return w.clockOut(a.Str("ref"), a.Str("title"))
	case "add_note":
		return w.addNote(a.Str("ref"), a.Str("title"), a.Str("text"))
	case "toggle_checkbox":
		return w.toggleBox(a.Str("ref"), a.Str("title"), a.Int("index", 0))
	case "move_item":
		return w.moveOrShift(a)
	}
	return "", false, fmt.Errorf("未知工具 %q", name)
}

func nonEmptyList(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

func (w *Workspace) listAgenda(startStr string, days int) (string, bool, error) {
	start := w.now()
	if startStr != "" {
		ts, err := parseTime(startStr, w.now())
		if err != nil {
			return "", false, err
		}
		start = ts.Start
	}
	if days < 1 {
		days = 1
	}
	if days > 60 {
		days = 60
	}
	ag := agenda.Build(w.Coll, start, days, w.agendaOpts())

	var b strings.Builder
	empty := true
	for _, day := range ag.Days {
		fmt.Fprintf(&b, "%s\n", formatDate(day.Date))
		if len(day.Items) == 0 {
			b.WriteString("  （无安排）\n")
			continue
		}
		empty = false
		for _, it := range day.Items {
			b.WriteString("  " + formatItem(it) + "\n")
		}
	}
	if empty {
		b.WriteString("\n这段时间没有任何安排。\n")
	}
	return b.String(), false, nil
}

// formatItem 把一条 agenda 记录渲染成给模型看的一行。
func formatItem(it agenda.Item) string {
	var parts []string
	if it.HasTime {
		slot := it.Start.Format("15:04")
		if it.Time != nil && it.Time.HasEnd() {
			slot += "-" + it.Time.End.Format("15:04")
		}
		parts = append(parts, slot)
	}
	parts = append(parts, "["+it.Kind.Label()+"]")
	switch it.Kind {
	case agenda.KindDeadline:
		switch {
		case it.DaysLeft > 0:
			parts = append(parts, fmt.Sprintf("(还剩%d天)", it.DaysLeft))
		case it.DaysLeft == 0:
			parts = append(parts, "(今天到期)")
		default:
			parts = append(parts, fmt.Sprintf("(已逾期%d天)", -it.DaysLeft))
		}
	case agenda.KindOverdue:
		parts = append(parts, fmt.Sprintf("(拖延%d天)", -it.DaysLeft))
	}
	if it.Node.TODO != "" {
		parts = append(parts, it.Node.TODO)
	}
	if it.Node.Priority != "" {
		parts = append(parts, "[#"+it.Node.Priority+"]")
	}
	parts = append(parts, it.Node.Title)

	meta := []string{"ref=" + refOf(it.Doc, it.Node), "类别=" + it.Category()}
	if tags := it.Node.AllTags(); len(tags) > 0 {
		meta = append(meta, "标签="+strings.Join(tags, ","))
	}
	return strings.Join(parts, " ") + "  (" + strings.Join(meta, ", ") + ")"
}

func (w *Workspace) listTodos(f agenda.TodoFilter) (string, bool, error) {
	items := agenda.Todos(w.Coll, f)
	if len(items) == 0 {
		return "没有符合条件的待办事项。\n", false, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 条待办：\n", len(items))
	for _, it := range items {
		line := fmt.Sprintf("- %s", it.Node.TODO)
		if it.Node.Priority != "" {
			line += " [#" + it.Node.Priority + "]"
		}
		line += " " + it.Node.Title
		if it.Node.Scheduled != nil {
			line += fmt.Sprintf(" | 计划 %s", it.Node.Scheduled.Start.Format("2006-01-02 15:04"))
		}
		if it.Node.Deadline != nil {
			line += fmt.Sprintf(" | 截止 %s", it.Node.Deadline.Start.Format("2006-01-02"))
		}
		meta := []string{"ref=" + refOf(it.Doc, it.Node), "类别=" + it.Category()}
		if tags := it.Node.AllTags(); len(tags) > 0 {
			meta = append(meta, "标签="+strings.Join(tags, ","))
		}
		b.WriteString(line + "  (" + strings.Join(meta, ", ") + ")\n")
	}
	return b.String(), false, nil
}

func (w *Workspace) findFree(dateStr string, minMinutes int) (string, bool, error) {
	date := w.now()
	if dateStr != "" {
		ts, err := parseTime(dateStr, w.now())
		if err != nil {
			return "", false, err
		}
		date = ts.Start
	}
	if minMinutes < 1 {
		minMinutes = 30
	}
	min := time.Duration(minMinutes) * time.Minute
	free := agenda.Free(w.Coll, date, w.workHours(), min, w.agendaOpts())

	var b strings.Builder
	fmt.Fprintf(&b, "%s 的可安排时段（%02d:00-%02d:00）：\n",
		formatDate(date), w.Cfg.WorkHours.Start, w.Cfg.WorkHours.End)
	if len(free) == 0 {
		b.WriteString("  没有不短于 " + fmt.Sprint(minMinutes) + " 分钟的空档。\n")
	}
	for _, s := range free {
		fmt.Fprintf(&b, "  %s-%s（%s）\n",
			s.Start.Format("15:04"), s.End.Format("15:04"), humanDuration(s.Duration()))
	}
	if busy := agenda.Busy(w.Coll, date, w.agendaOpts()); len(busy) > 0 {
		b.WriteString("已占用：\n")
		for _, s := range busy {
			fmt.Fprintf(&b, "  %s-%s %s\n", s.Start.Format("15:04"), s.End.Format("15:04"), s.Title)
		}
	}
	return b.String(), false, nil
}

func humanDuration(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d小时%d分钟", h, m)
	case h > 0:
		return fmt.Sprintf("%d小时", h)
	default:
		return fmt.Sprintf("%d分钟", m)
	}
}

func (w *Workspace) getItem(ref, title string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "标题: %s\n", n.Title)
	fmt.Fprintf(&b, "引用: %s\n", refOf(doc, n))
	fmt.Fprintf(&b, "状态: %s\n", orDash(n.TODO))
	fmt.Fprintf(&b, "优先级: %s\n", orDash(n.Priority))
	fmt.Fprintf(&b, "所在位置: %s\n", strings.Join(n.Breadcrumb(), " / "))
	fmt.Fprintf(&b, "类别: %s\n", n.Category())
	if tags := n.AllTags(); len(tags) > 0 {
		fmt.Fprintf(&b, "标签: %s\n", strings.Join(tags, ", "))
	}
	if n.Scheduled != nil {
		fmt.Fprintf(&b, "计划: %s\n", n.Scheduled.String())
	}
	if n.Deadline != nil {
		fmt.Fprintf(&b, "截止: %s\n", n.Deadline.String())
	}
	if n.Closed != nil {
		fmt.Fprintf(&b, "完成于: %s\n", n.Closed.String())
	}
	if total := n.ClockedTotal(); total > 0 {
		fmt.Fprintf(&b, "已投入: %s\n", humanDuration(total))
	}
	if body := nodeBody(doc, n); body != "" {
		fmt.Fprintf(&b, "正文:\n%s\n", body)
	}
	if len(n.Children) > 0 {
		b.WriteString("子条目:\n")
		for _, c := range n.Children {
			fmt.Fprintf(&b, "  - %s %s (ref=%s)\n", orDash(c.TODO), c.Title, refOf(doc, c))
		}
	}
	return b.String(), false, nil
}

func orDash(s string) string {
	if s == "" {
		return "无"
	}
	return s
}

func nodeBody(doc *org.Document, n *org.Node) string {
	end := n.End
	if len(n.Children) > 0 {
		end = n.Children[0].Line
	}
	var lines []string
	inDrawer := false
	for i := n.Line + 1; i < end && i < len(doc.Lines); i++ {
		line := doc.Lines[i]
		trimmed := strings.TrimSpace(line)
		if inDrawer {
			if strings.EqualFold(trimmed, ":END:") {
				inDrawer = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, ":") && strings.HasSuffix(trimmed, ":") && len(trimmed) > 2 {
			inDrawer = true
			continue
		}
		if strings.HasPrefix(trimmed, "SCHEDULED:") || strings.HasPrefix(trimmed, "DEADLINE:") ||
			strings.HasPrefix(trimmed, "CLOSED:") || strings.HasPrefix(trimmed, "CLOCK:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// updateItem 一次改一个条目的若干项：状态、优先级、计划、截止、循环。
//
// 合并之前这是五个工具（schedule_item / set_deadline / set_todo_state /
// set_priority / set_repeat），每个都要 ref 和 title，光这两项的描述就在
// 工具表里重复了十几遍。合成一个之后，「没传」和「传了空串」靠字段在不在
// JSON 里区分——那正是原先「留空表示取消」的语义，只是不必五个工具各说一遍。
//
// 改动顺序不能随便调：**状态放最后**。把循环任务标成 DONE 会顺手把计划推到
// 下一个周期，得先让新的计划时间和循环规则就位，推出来的才是对的那一天。
//
// 全程只 snapshot 一次、save 一次。中间每改一步都要接住返回的新节点指针——
// 写回会重新解析文档，之前那个指针就失效了。
func (w *Workspace) updateItem(a args) (string, bool, error) {
	p := patchFrom(a)
	if p.empty() {
		return "", false, fmt.Errorf("没说要改什么：state / priority / scheduled / deadline / repeat 至少给一个")
	}
	doc, n, err := w.resolve(a.Str("ref"), a.Str("title"))
	if err != nil {
		return "", false, err
	}
	w.snapshot(doc)

	var done []string

	if p.Priority.ok {
		prio, perr := checkPriority(p.Priority.v)
		if perr != nil {
			return "", false, perr
		}
		// 就地改字段再同步那一行，不必 Reparse：优先级只改标题行的内容、
		// 不增减行数，n 本来就是 doc.Nodes 里的那个指针，已经是新值了。
		n.Priority = prio
		doc.Lines[n.Line] = n.String()
		done = append(done, "优先级设为 "+orDash(prio))
	}

	// 计划和截止是同一件事的两个键：解析时间、继承原有的循环与提前量、写回去。
	for _, f := range []struct {
		key, label string
		val        optStr
	}{
		{"SCHEDULED", "计划时间", p.Scheduled},
		{"DEADLINE", "截止时间", p.Deadline},
	} {
		if !f.val.ok {
			continue
		}
		var ts *org.Timestamp
		if f.val.v != "" {
			if ts, err = parseTime(f.val.v, w.now()); err != nil {
				return "", false, err
			}
			old := n.Scheduled
			if f.key == "DEADLINE" {
				old = n.Deadline
			}
			if old != nil {
				ts.Repeat, ts.Warn = old.Repeat, old.Warn
			}
		}
		n = doc.SetPlanning(n, f.key, ts)
		if ts == nil {
			done = append(done, "取消"+f.label)
		} else {
			done = append(done, f.label+"设为 "+ts.String())
		}
	}

	if p.Repeat.ok {
		r, rerr := org.ParseRepeat(p.Repeat.v)
		if rerr != nil {
			return "", false, rerr
		}
		if n, err = doc.SetRepeat(n, "", r); err != nil {
			return "", false, err
		}
		if r == nil {
			done = append(done, "不再循环")
		} else {
			done = append(done, r.Describe()+"重复")
		}
	}

	// 状态放最后：把循环任务标成 DONE 会顺手把计划推到下一个周期，
	// 得先让新的计划时间和循环规则就位，推出来的才是对的那一天。
	if p.State.ok {
		old := n.TODO
		if n, err = doc.SetTodo(n, strings.ToUpper(p.State.v)); err != nil {
			return "", false, fmt.Errorf("%w（该文件可用状态：%s）", err, strings.Join(doc.AllKeywords(), "、"))
		}
		// 重复任务标完成后，org 的做法是把状态退回原样、把计划推到下一个周期。
		// 照字面说成「从 TODO 改为 TODO」，看着像没生效——模型也会原样转述给用户。
		if n.TODO == old && n.Scheduled != nil {
			done = append(done, fmt.Sprintf("这一次已完成，状态回到 %s，下次计划推进到 %s",
				orDash(n.TODO), n.Scheduled.Start.Format("2006-01-02")))
		} else {
			done = append(done, fmt.Sprintf("状态从 %s 改为 %s", orDash(old), orDash(n.TODO)))
		}
	}

	if err := w.save(doc); err != nil {
		return "", false, err
	}
	summary := strings.Join(done, "、")
	w.record(doc, "改「%s」：%s", n.Title, summary)
	return fmt.Sprintf("已改「%s」：%s。（新引用 %s）",
		n.Title, summary, refOf(doc, n)), true, nil
}

// moveOrShift 把 move_item 的两种用法分开。二选一，都不给就说清楚——
// 判据统一用「键在不在」，别一条按键、一条按值是不是零。
func (w *Workspace) moveOrShift(a args) (string, bool, error) {
	ref, title := a.Str("ref"), a.Str("title")
	switch {
	case a.Has("parent_ref"):
		return w.moveItem(ref, title, a.Str("parent_ref"))
	case a.Has("level_delta"):
		return w.shiftLevel(ref, title, a.Int("level_delta", 0))
	}
	return "", false, fmt.Errorf("要么给 parent_ref（挂到某个父条目下，传空字符串移到顶层），" +
		"要么给 level_delta（±1 升降级）")
}

// shiftLevel 把条目连同子树升降若干级：正数降级（缩得更深），负数升级。
// 升级前先算够不够升——升到一半发现顶到天花板，内存里的文档已经改花了。
func (w *Workspace) shiftLevel(ref, title string, delta int) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	if delta == 0 {
		return "", false, fmt.Errorf("level_delta 得是正数（降级）或负数（升级）")
	}
	if n.Level+delta < 1 {
		return "", false, fmt.Errorf("「%s」在第 %d 级，升不了 %d 级", n.Title, n.Level, -delta)
	}
	w.snapshot(doc)

	step, dir, lv := doc.Demote, "降", delta
	if delta < 0 {
		step, dir, lv = doc.Promote, "升", -delta
	}
	for i := 0; i < lv; i++ {
		n = step(n)
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "把「%s」%s了 %d 级", n.Title, dir, lv)
	return fmt.Sprintf("已把「%s」%s了 %d 级（新引用 %s）。",
		n.Title, dir, lv, refOf(doc, n)), true, nil
}

func (w *Workspace) deleteItem(ref, title string) (string, bool, error) {
	if strings.TrimSpace(title) == "" {
		return "", false, fmt.Errorf("删除前必须提供 title，用来确认删的是哪一条")
	}
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	if n.Title != title {
		return "", false, fmt.Errorf("引用 %s 指向的是「%s」，和你给的标题「%s」对不上，请重新查询",
			ref, n.Title, title)
	}

	kids := n.DescendantCount()
	w.snapshot(doc)
	doc.DeleteNode(n)
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "删除「%s」", title)

	msg := fmt.Sprintf("已删除「%s」", title)
	if kids > 0 {
		msg += fmt.Sprintf("及其 %d 个子条目", kids)
	}
	return msg + "。用户可以按 u 撤销。", true, nil
}

func (w *Workspace) createItem(a args) (string, bool, error) {
	title := a.Str("title")
	if title == "" {
		return "", false, fmt.Errorf("必须提供标题")
	}

	// 给了父条目就跟着它走：子条目只能跟父亲待在同一个文件里，
	// 这时候 file 说什么都不算数。
	var doc *org.Document
	var parent *org.Node
	if pref := a.Str("parent"); pref != "" {
		var perr error
		if doc, parent, perr = w.resolve(pref, ""); perr != nil {
			return "", false, perr
		}
	} else {
		target := a.Str("file")
		doc = w.Coll.Doc(target)
		if doc == nil && target != "" {
			return "", false, fmt.Errorf("找不到文件 %s，可用文件：%s", target, strings.Join(w.fileNames(), "、"))
		}
		if doc == nil {
			if doc = w.Coll.Doc(w.Cfg.InboxFile()); doc == nil {
				if len(w.Coll.Docs) == 0 {
					return "", false, fmt.Errorf("没有可写入的 org 文件")
				}
				doc = w.Coll.Docs[0]
			}
		}
	}

	// 跟 update_item 读同一份结构，但零值的意思相反：新建时**没给**状态
	// 才落成默认的 TODO，而不是「给了空串」。这两件事以前都写成 ""，
	// 于是同一个字段名在两个工具里是两个意思。
	p := patchFrom(a)
	prio, err := checkPriority(p.Priority.Or(""))
	if err != nil {
		return "", false, err
	}
	spec := org.NewNodeSpec{
		Title: title,
		// 没提状态才落成默认的 TODO；明写了空串是「要一条不带 TODO
		// 关键字的普通条目」——跟 update_item 的「空串即清除」对得上。
		TODO:     strings.ToUpper(p.State.Or("TODO")),
		Priority: prio,
		Parent:   parent,
		Tags:     a.Strings("tags"),
	}
	if v := p.Scheduled.Or(""); v != "" {
		if spec.Scheduled, err = parseTime(v, w.now()); err != nil {
			return "", false, err
		}
	}
	if v := p.Deadline.Or(""); v != "" {
		if spec.Deadline, err = parseTime(v, w.now()); err != nil {
			return "", false, err
		}
	}
	if v := p.Repeat.Or(""); v != "" {
		r, rerr := org.ParseRepeat(v)
		if rerr != nil {
			return "", false, rerr
		}
		switch {
		case spec.Scheduled != nil:
			spec.Scheduled.Repeat = r
		case spec.Deadline != nil:
			spec.Deadline.Repeat = r
		default:
			return "", false, fmt.Errorf("要循环的条目得先有计划或截止时间")
		}
	}
	if body := a.Str("body"); body != "" {
		spec.Body = strings.Split(body, "\n")
	}

	w.snapshot(doc)
	n := doc.AddNode(spec)
	if n == nil {
		return "", false, fmt.Errorf("创建条目失败")
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "新建「%s」", title)

	var extra []string
	if r := n.Repeater(); r != nil {
		extra = append(extra, r.Describe()+"重复")
	}
	if spec.Scheduled != nil {
		extra = append(extra, "计划 "+spec.Scheduled.String())
	}
	if spec.Deadline != nil {
		extra = append(extra, "截止 "+spec.Deadline.String())
	}
	msg := fmt.Sprintf("已在 %s 新建「%s」", doc.Category(), title)
	if len(extra) > 0 {
		msg += "（" + strings.Join(extra, "，") + "）"
	}
	return msg + fmt.Sprintf("，引用 %s。", refOf(doc, n)), true, nil
}

func (w *Workspace) fileNames() []string {
	var out []string
	for _, d := range w.Coll.Docs {
		out = append(out, d.Category())
	}
	return out
}

func (w *Workspace) clockIn(ref, title string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}

	// 别的条目还在计时就先收掉。同时开着两个，统计出来的工时就是假的，
	// 而且那条没收尾的 CLOCK 会一直挂着。
	//
	// 这条写在提示词里也说了一遍，但提示词是建议、代码才是保证——模型忘了
	// 先 clock_out 的代价是一份查不出来的错账。
	closed := w.closeRunningClocks(n)

	w.snapshot(doc)
	n, err = doc.ClockIn(n, w.now())
	if err != nil {
		return "", false, err
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "开始为「%s」计时", n.Title)

	msg := fmt.Sprintf("已开始为「%s」计时。", n.Title)
	if len(closed) > 0 {
		msg += fmt.Sprintf("（顺带停掉了还在计时的：%s）", strings.Join(closed, "、"))
	}
	return msg, true, nil
}

// closeRunningClocks 把除 keep 之外所有还开着的计时收掉，返回它们的标题。
func (w *Workspace) closeRunningClocks(keep *org.Node) []string {
	var closed []string
	now := w.now()
	for _, d := range w.Coll.Docs {
		dirty := false
		for _, n := range d.Nodes {
			if n == keep || n.RunningClock() == nil {
				continue
			}
			if !dirty {
				w.snapshot(d)
			}
			if _, err := d.ClockOut(n, now); err != nil {
				continue // 收不掉就算了，别为这个把 clock_in 整个搅黄
			}
			closed = append(closed, n.Title)
			dirty = true
		}
		if dirty {
			if err := d.Save(); err != nil {
				// 同上：记一笔，但不挡着主要的事
				w.record(d, "停掉旧计时时保存失败：%v", err)
			}
		}
	}
	return closed
}

func (w *Workspace) clockOut(ref, title string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	w.snapshot(doc)
	n, err = doc.ClockOut(n, w.now())
	if err != nil {
		return "", false, err
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "结束「%s」计时", n.Title)
	return fmt.Sprintf("已结束「%s」计时，累计 %s。", n.Title, humanDuration(n.ClockedTotal())), true, nil
}

func (w *Workspace) addNote(ref, title, text string) (string, bool, error) {
	if strings.TrimSpace(text) == "" {
		return "", false, fmt.Errorf("笔记内容不能为空")
	}
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	w.snapshot(doc)
	doc.AddNote(n, strings.TrimSpace(text), w.now())
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "为「%s」记了一条笔记", n.Title)
	return fmt.Sprintf("已在「%s」的 LOGBOOK 里记下笔记。", n.Title), true, nil
}

// toggleBox 勾上/取消正文里的第 index 个复选框（从 1 数起）。
//
// 对外从 1 数起是有意的：用户和模型说的是「第二项」，不是「下标 1」。
// 转成下标这一下放在这里，org 那边照旧按 0 起。
func (w *Workspace) toggleBox(ref, title string, index int) (string, bool, error) {
	if index < 1 {
		return "", false, fmt.Errorf("要说清是第几个复选框（从 1 数起）")
	}
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	if len(n.Boxes) == 0 {
		return "", false, fmt.Errorf("「%s」正文里没有复选框", n.Title)
	}
	if index > len(n.Boxes) {
		return "", false, fmt.Errorf("「%s」只有 %d 个复选框，没有第 %d 个",
			n.Title, len(n.Boxes), index)
	}
	// 先把要报的话攒下来：Toggle 之后 n 会被重新解析，Boxes 也跟着换了。
	box := n.Boxes[index-1]
	w.snapshot(doc)
	n, err = doc.ToggleCheckbox(n, index-1)
	if err != nil {
		return "", false, err
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	what := "勾上了"
	if box.Done {
		what = "取消了"
	}
	done, total := n.Progress()
	w.record(doc, "%s「%s」的第 %d 项", what, n.Title, index)
	return fmt.Sprintf("%s「%s」里的「%s」，现在 %d/%d。",
		what, n.Title, strings.TrimSpace(box.Text), done, total), true, nil
}

func (w *Workspace) moveItem(ref, title, parentRef string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	var parent *org.Node
	if strings.TrimSpace(parentRef) != "" {
		pdoc, pn, perr := w.resolve(parentRef, "")
		if perr != nil {
			return "", false, perr
		}
		if pdoc != doc {
			return "", false, fmt.Errorf("暂不支持跨文件移动")
		}
		parent = pn
	}
	w.snapshot(doc)
	n, err = doc.MoveNode(n, parent)
	if err != nil {
		return "", false, err
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	where := "文件顶层"
	if parent != nil {
		where = "「" + parent.Title + "」下"
	}
	w.record(doc, "把「%s」移动到%s", n.Title, where)
	return fmt.Sprintf("已把「%s」移动到%s（新引用 %s）。", n.Title, where, refOf(doc, n)), true, nil
}
