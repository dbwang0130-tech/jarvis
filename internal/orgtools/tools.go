package orgtools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/org"
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

func schema(props map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": props, "required": required}
}

func str(desc string) map[string]any  { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any  { return map[string]any{"type": "integer", "description": desc} }
func flag(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// wsToolDefs 返回日程工具的全部声明。
func wsToolDefs() []agent.ToolSpec {
	return []agent.ToolSpec{
		{
			Name:        "list_agenda",
			Description: "查看某段日期范围内的日程安排，包含计划、截止、事件与逾期项。用于了解用户当前的时间占用情况。",
			Parameters: schema(map[string]any{
				"start_date": str("起始日期，格式 2006-01-02，留空表示今天"),
				"days":       num("天数，默认 7"),
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
				"min_minutes": num("只返回不短于该分钟数的空档，默认 30"),
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
			Name:        "schedule_item",
			Description: "设置或修改条目的计划时间（SCHEDULED）。安排任务到具体时间用这个。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"time":  str("计划时间，格式 2006-01-02、2006-01-02 15:04 或 2006-01-02 15:04-16:30；末尾可加循环规则如 +1w；留空表示取消计划"),
			}, "ref"),
		},
		{
			Name:        "set_deadline",
			Description: "设置或修改条目的截止时间（DEADLINE）。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"time":  str("截止时间，格式 2006-01-02 或 2006-01-02 15:04；留空表示取消截止"),
			}, "ref"),
		},
		{
			Name:        "set_todo_state",
			Description: "修改条目的 TODO 状态，例如标记完成（DONE）或改为进行中（NEXT）。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"state": str("目标状态，如 TODO、NEXT、WAITING、DONE、CANCELLED；留空表示去掉状态"),
			}, "ref"),
		},
		{
			Name:        "set_priority",
			Description: "设置条目优先级。",
			Parameters: schema(map[string]any{
				"ref":      str("条目引用，形如 work.org:12"),
				"title":    str("条目标题，用于校验"),
				"priority": str("优先级 A、B 或 C；留空表示去掉优先级"),
			}, "ref"),
		},
		{
			Name:        "set_repeat",
			Description: "把条目设成循环任务，或取消循环。条目需要先有计划或截止时间。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
				"rule":  str("循环规则：+1d 每天、+1w 每周、+1m 每月、+2w 每两周、.+3d 表示完成后再隔三天；留空表示取消循环"),
			}, "ref"),
		},
		{
			Name: "delete_item",
			Description: "删除一个条目及其下属的全部子条目。这是破坏性操作，只在用户明确要求删除时使用；" +
				"如果只是做完了，应该用 set_todo_state 标记完成。",
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
				"state":     str("TODO 状态，默认 TODO"),
				"priority":  str("优先级 A、B 或 C"),
				"scheduled": str("计划时间，格式同 schedule_item"),
				"deadline":  str("截止时间，格式同 set_deadline"),
				"repeat":    str("循环规则，如 +1w 表示每周重复；留空表示不循环"),
				"tags":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "标签列表"},
				"body":      str("正文说明，可多行"),
				"file":      str("写入哪个文件，如 work.org；留空则写入默认收件箱"),
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
				"index": num("第几个复选框，从 1 数起"),
			}, "ref", "index"),
		},
		{
			Name:        "demote_item",
			Description: "把条目及其子条目降一级（变成更深一层的子任务）。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
			}, "ref"),
		},
		{
			Name:        "promote_item",
			Description: "把条目及其子条目升一级（减少一层缩进）。",
			Parameters: schema(map[string]any{
				"ref":   str("条目引用，形如 work.org:12"),
				"title": str("条目标题，用于校验"),
			}, "ref"),
		},
		{
			Name:        "move_item",
			Description: "把条目及其子条目移动到另一个父条目下（parent_ref 留空则移到文件顶层末尾）。",
			Parameters: schema(map[string]any{
				"ref":        str("要移动的条目引用，形如 work.org:12"),
				"title":      str("条目标题，用于校验"),
				"parent_ref": str("目标父条目引用，形如 work.org:5；留空移到顶层末尾"),
			}, "ref"),
		},
	}
}

// execOrg 执行日程工具，按名称分发。
func (w *Workspace) execOrg(name, rawArgs string) (string, bool, error) {
	var args map[string]any
	if strings.TrimSpace(rawArgs) != "" {
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", false, fmt.Errorf("参数不是合法 JSON: %w", err)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	getStr := func(k string) string {
		if v, ok := args[k].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	getInt := func(k string, def int) int {
		switch v := args[k].(type) {
		case float64:
			return int(v)
		case string:
			var n int
			if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
				return n
			}
		}
		return def
	}
	getBool := func(k string) bool {
		b, _ := args[k].(bool)
		return b
	}

	switch name {
	case "list_agenda":
		return w.listAgenda(getStr("start_date"), getInt("days", w.Cfg.AgendaDays))
	case "list_todos":
		return w.listTodos(agenda.TodoFilter{
			Query:       getStr("query"),
			Category:    getStr("category"),
			IncludeDone: getBool("include_done"),
			Keywords:    nonEmptyList(getStr("state")),
			Tags:        nonEmptyList(getStr("tag")),
		})
	case "find_free_slots":
		return w.findFree(getStr("date"), getInt("min_minutes", 30))
	case "get_item":
		return w.getItem(getStr("ref"), getStr("title"))
	case "schedule_item":
		return w.setPlanning(getStr("ref"), getStr("title"), "SCHEDULED", getStr("time"))
	case "set_deadline":
		return w.setPlanning(getStr("ref"), getStr("title"), "DEADLINE", getStr("time"))
	case "set_todo_state":
		return w.setState(getStr("ref"), getStr("title"), getStr("state"))
	case "set_priority":
		return w.setPriority(getStr("ref"), getStr("title"), getStr("priority"))
	case "set_repeat":
		return w.setRepeat(getStr("ref"), getStr("title"), getStr("rule"))
	case "delete_item":
		return w.deleteItem(getStr("ref"), getStr("title"))
	case "create_item":
		return w.createItem(args)
	case "clock_in":
		return w.clockIn(getStr("ref"), getStr("title"))
	case "clock_out":
		return w.clockOut(getStr("ref"), getStr("title"))
	case "add_note":
		return w.addNote(getStr("ref"), getStr("title"), getStr("text"))
	case "toggle_checkbox":
		return w.toggleBox(getStr("ref"), getStr("title"), getInt("index", 0))
	case "demote_item":
		return w.demote(getStr("ref"), getStr("title"))
	case "promote_item":
		return w.promote(getStr("ref"), getStr("title"))
	case "move_item":
		return w.moveItem(getStr("ref"), getStr("title"), getStr("parent_ref"))
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

func (w *Workspace) setPlanning(ref, title, key, timeStr string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	var ts *org.Timestamp
	if timeStr != "" {
		if ts, err = parseTime(timeStr, w.now()); err != nil {
			return "", false, err
		}
		old := n.Scheduled
		if key == "DEADLINE" {
			old = n.Deadline
		}
		if old != nil {
			ts.Repeat, ts.Warn = old.Repeat, old.Warn
		}
	}
	w.snapshot(doc)
	n = doc.SetPlanning(n, key, ts)
	if err := w.save(doc); err != nil {
		return "", false, err
	}

	label := "计划时间"
	if key == "DEADLINE" {
		label = "截止时间"
	}
	if ts == nil {
		w.record(doc, "取消「%s」的%s", n.Title, label)
		return fmt.Sprintf("已取消「%s」的%s。", n.Title, label), true, nil
	}
	w.record(doc, "把「%s」的%s设为 %s", n.Title, label, ts.String())
	return fmt.Sprintf("已把「%s」的%s设为 %s（新引用 %s）。",
		n.Title, label, ts.String(), refOf(doc, n)), true, nil
}

func (w *Workspace) setState(ref, title, state string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	state = strings.ToUpper(strings.TrimSpace(state))
	old := n.TODO
	w.snapshot(doc)
	n, err = doc.SetTodo(n, state)
	if err != nil {
		return "", false, fmt.Errorf("%w（该文件可用状态：%s）", err, strings.Join(doc.AllKeywords(), "、"))
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	// 重复任务标完成后，org 的做法是把状态退回原样、把计划推到下一个周期。
	// 照字面说成「从 TODO 改为 TODO」，看着像没生效——模型也会原样转述给用户。
	repeated := n.TODO == old && n.Scheduled != nil
	if repeated {
		next := n.Scheduled.Start.Format("2006-01-02")
		w.record(doc, "完成「%s」这一次，下次计划推进到 %s", n.Title, next)
		return fmt.Sprintf("「%s」这一次已完成。这是重复任务，状态回到 %s，下次计划推进到 %s。（新引用 %s）",
			n.Title, orDash(n.TODO), next, refOf(doc, n)), true, nil
	}

	w.record(doc, "把「%s」从 %s 改为 %s", n.Title, orDash(old), orDash(n.TODO))
	return fmt.Sprintf("已把「%s」的状态从 %s 改为 %s。（新引用 %s）",
		n.Title, orDash(old), orDash(n.TODO), refOf(doc, n)), true, nil
}

func (w *Workspace) setPriority(ref, title, prio string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	prio = strings.ToUpper(strings.TrimSpace(prio))
	if prio != "" && (len(prio) != 1 || prio[0] < 'A' || prio[0] > 'Z') {
		return "", false, fmt.Errorf("优先级 %q 不合法，应为单个字母，如 A", prio)
	}
	w.snapshot(doc)
	n.Priority = prio
	doc.Lines[n.Line] = n.String()
	doc.Reparse()
	n = doc.NodeAt(n.Line)
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "把「%s」的优先级设为 %s", n.Title, orDash(prio))
	return fmt.Sprintf("已把「%s」的优先级设为 %s。", n.Title, orDash(prio)), true, nil
}

func (w *Workspace) setRepeat(ref, title, rule string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	r, err := org.ParseRepeat(rule)
	if err != nil {
		return "", false, err
	}
	w.snapshot(doc)
	n, err = doc.SetRepeat(n, "", r)
	if err != nil {
		return "", false, err
	}
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	if r == nil {
		w.record(doc, "取消「%s」的循环", n.Title)
		return fmt.Sprintf("「%s」不再循环。", n.Title), true, nil
	}
	w.record(doc, "把「%s」设为%s重复", n.Title, r.Describe())
	return fmt.Sprintf("「%s」现在%s重复，下一次是 %s。（新引用 %s）",
		n.Title, r.Describe(), nextOccurrence(n).Format("2006-01-02"), refOf(doc, n)), true, nil
}

func nextOccurrence(n *org.Node) time.Time {
	if n.Scheduled != nil {
		return n.Scheduled.Start
	}
	if n.Deadline != nil {
		return n.Deadline.Start
	}
	return time.Time{}
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

func (w *Workspace) createItem(args map[string]any) (string, bool, error) {
	getStr := func(k string) string {
		v, _ := args[k].(string)
		return strings.TrimSpace(v)
	}
	title := getStr("title")
	if title == "" {
		return "", false, fmt.Errorf("必须提供标题")
	}

	target := getStr("file")
	doc := w.Coll.Doc(target)
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

	spec := org.NewNodeSpec{
		Title:    title,
		TODO:     strings.ToUpper(getStr("state")),
		Priority: strings.ToUpper(getStr("priority")),
	}
	if spec.TODO == "" {
		spec.TODO = "TODO"
	}
	if raw, ok := args["tags"].([]any); ok {
		for _, t := range raw {
			if s, ok := t.(string); ok && strings.TrimSpace(s) != "" {
				spec.Tags = append(spec.Tags, strings.TrimSpace(s))
			}
		}
	}
	var err error
	if v := getStr("scheduled"); v != "" {
		if spec.Scheduled, err = parseTime(v, w.now()); err != nil {
			return "", false, err
		}
	}
	if v := getStr("deadline"); v != "" {
		if spec.Deadline, err = parseTime(v, w.now()); err != nil {
			return "", false, err
		}
	}
	if v := getStr("repeat"); v != "" {
		r, err := org.ParseRepeat(v)
		if err != nil {
			return "", false, err
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
	if body := getStr("body"); body != "" {
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

func (w *Workspace) demote(ref, title string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	w.snapshot(doc)
	n = doc.Demote(n)
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "把「%s」降了一级", n.Title)
	return fmt.Sprintf("已把「%s」降了一级（新引用 %s）。", n.Title, refOf(doc, n)), true, nil
}

func (w *Workspace) promote(ref, title string) (string, bool, error) {
	doc, n, err := w.resolve(ref, title)
	if err != nil {
		return "", false, err
	}
	if n.Level <= 1 {
		return "", false, fmt.Errorf("「%s」已在顶层，无法再升", n.Title)
	}
	w.snapshot(doc)
	n = doc.Promote(n)
	if err := w.save(doc); err != nil {
		return "", false, err
	}
	w.record(doc, "把「%s」升了一级", n.Title)
	return fmt.Sprintf("已把「%s」升了一级（新引用 %s）。", n.Title, refOf(doc, n)), true, nil
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
