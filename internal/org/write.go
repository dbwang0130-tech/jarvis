package org

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// 所有修改都发生在标题行本身或其之后，因此节点的起始行号不会变，
// 调用方可以在 Reparse 之后用 NodeAt(line) 重新拿到节点。

// Reparse 在 Lines 被修改后重建节点树。
func (d *Document) Reparse() {
	d.Nodes, d.Roots = nil, nil
	d.parseKeywords()
	d.parseNodes()
}

// NodeAt 返回标题行号为 line 的节点。
func (d *Document) NodeAt(line int) *Node {
	for _, n := range d.Nodes {
		if n.Line == line {
			return n
		}
	}
	return nil
}

// Save 把 Lines 写回磁盘。
func (d *Document) Save() error {
	if d.Path == "" {
		return fmt.Errorf("文档没有关联路径")
	}
	text := strings.Join(d.Lines, "\n")
	if len(d.Lines) > 0 {
		text += "\n"
	}
	if err := os.WriteFile(d.Path, []byte(text), 0o644); err != nil {
		return err
	}
	if fi, err := os.Stat(d.Path); err == nil {
		d.ModTime = fi.ModTime()
	}
	return nil
}

func (d *Document) insertLines(at int, lines ...string) {
	if at > len(d.Lines) {
		at = len(d.Lines)
	}
	rest := append([]string{}, d.Lines[at:]...)
	d.Lines = append(append(d.Lines[:at:at], lines...), rest...)
}

func (d *Document) removeLine(at int) {
	if at < 0 || at >= len(d.Lines) {
		return
	}
	d.Lines = append(d.Lines[:at:at], d.Lines[at+1:]...)
}

// indentOf 返回一行的前导空白。
func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// bodyIndent 推断节点正文应使用的缩进：沿用已有正文行的风格，默认不缩进。
func (n *Node) bodyIndent() string {
	for i := n.Line + 1; i < n.End && i < len(n.Doc.Lines); i++ {
		line := n.Doc.Lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		return indentOf(line)
	}
	return ""
}

// planningLine 返回节点的 planning 行号，没有则返回 -1。
func (n *Node) planningLine() int {
	for i := n.Line + 1; i < n.End && i < len(n.Doc.Lines); i++ {
		line := n.Doc.Lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if planningRe.MatchString(line) {
			return i
		}
		return -1 // planning 行只能紧跟标题
	}
	return -1
}

// SetPlanning 设置或清除节点的 SCHEDULED / DEADLINE / CLOSED。ts 为 nil 表示清除。
// 返回重新解析后的节点。
func (d *Document) SetPlanning(n *Node, key string, ts *Timestamp) *Node {
	line := n.Line
	switch strings.ToUpper(key) {
	case "SCHEDULED":
		n.Scheduled = ts
	case "DEADLINE":
		n.Deadline = ts
	case "CLOSED":
		n.Closed = ts
	default:
		return n
	}
	n.writePlanning()
	d.Reparse()
	return d.NodeAt(line)
}

// writePlanning 按 org 的惯用顺序重建 planning 行（没有内容时删除该行）。
func (n *Node) writePlanning() {
	var parts []string
	if n.Closed != nil {
		parts = append(parts, "CLOSED: "+n.Closed.String())
	}
	if n.Deadline != nil {
		parts = append(parts, "DEADLINE: "+n.Deadline.String())
	}
	if n.Scheduled != nil {
		parts = append(parts, "SCHEDULED: "+n.Scheduled.String())
	}

	at := n.planningLine()
	if len(parts) == 0 {
		if at >= 0 {
			n.Doc.removeLine(at)
		}
		return
	}
	text := n.bodyIndent() + strings.Join(parts, " ")
	if at >= 0 {
		n.Doc.Lines[at] = text
		return
	}
	n.Doc.insertLines(n.Line+1, text)
}

// SetTodo 切换节点的 TODO 关键字（空字符串表示去掉状态）。
// 进入完成态时自动记 CLOSED；带重复器的任务则推进时间戳并保持未完成。
// 返回重新解析后的节点。
func (d *Document) SetTodo(n *Node, kw string) (*Node, error) {
	if kw != "" {
		valid := false
		for _, k := range d.AllKeywords() {
			if k == kw {
				valid = true
				break
			}
		}
		if !valid {
			return n, fmt.Errorf("未知的 TODO 关键字 %q", kw)
		}
	}
	line := n.Line
	now := time.Now()
	becomingDone := d.IsDoneKeyword(kw) && !n.IsDone()

	if becomingDone && n.hasRepeater() {
		n.advanceRepeaters(now)
		n.Props["LAST_REPEAT"] = (&Timestamp{Start: now, HasTime: true}).String()
		n.writeProp("LAST_REPEAT")
		n.writePlanning()
		if d.LogStates {
			n.logStateChange(n.TODO, kw, now)
		}
		d.Reparse()
		return d.NodeAt(line), nil
	}

	from := n.TODO
	n.TODO = kw
	switch {
	case becomingDone:
		n.Closed = &Timestamp{Start: now, HasTime: true}
	case !d.IsDoneKeyword(kw):
		n.Closed = nil
	}
	d.Lines[n.Line] = n.String()
	n.writePlanning()
	if d.LogStates && from != kw {
		n.logStateChange(from, kw, now)
	}
	d.Reparse()
	return d.NodeAt(line), nil
}

// NextTodo 返回状态循环中的下一个关键字：无状态 -> 第一个未完成态 -> ... -> 完成态 -> 无状态。
func (d *Document) NextTodo(cur string) string {
	all := d.AllKeywords()
	if len(all) == 0 {
		return ""
	}
	if cur == "" {
		return all[0]
	}
	for i, k := range all {
		if k == cur {
			if i+1 < len(all) {
				return all[i+1]
			}
			return ""
		}
	}
	return all[0]
}

func (n *Node) hasRepeater() bool {
	return (n.Scheduled != nil && n.Scheduled.Repeat != nil) ||
		(n.Deadline != nil && n.Deadline.Repeat != nil)
}

// advanceRepeaters 把带重复器的计划时间推进到下一次。
func (n *Node) advanceRepeaters(now time.Time) {
	for _, ts := range []*Timestamp{n.Scheduled, n.Deadline} {
		if ts == nil || ts.Repeat == nil {
			continue
		}
		next := ts.Repeat.Next(ts.Start, now)
		if ts.HasEnd() {
			ts.End = ts.End.Add(next.Sub(ts.Start))
		}
		ts.Start = next
		ts.Raw = ""
	}
}

// drawerRange 返回名为 name 的抽屉的起止行号（含 :NAME: 与 :END:），不存在时返回 -1, -1。
func (n *Node) drawerRange(name string) (start, end int) {
	name = strings.ToUpper(name)
	for i := n.Line + 1; i < n.End && i < len(n.Doc.Lines); i++ {
		line := n.Doc.Lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "*") && headlineRe.MatchString(line) {
			break
		}
		m := drawerRe.FindStringSubmatch(line)
		if m == nil || strings.ToUpper(m[1]) != name {
			continue
		}
		for j := i + 1; j < n.End && j < len(n.Doc.Lines); j++ {
			if drawerEnd.MatchString(n.Doc.Lines[j]) {
				return i, j
			}
		}
		return i, -1
	}
	return -1, -1
}

// ensureDrawer 保证抽屉存在，返回可插入内容的行号（抽屉首行之后）。
func (n *Node) ensureDrawer(name string) int {
	name = strings.ToUpper(name)
	if start, end := n.drawerRange(name); start >= 0 && end > start {
		return start + 1
	}
	indent := n.bodyIndent()
	at := n.Line + 1
	if p := n.planningLine(); p >= 0 {
		at = p + 1
	}
	// PROPERTIES 必须紧跟标题/planning 行，其他抽屉排在它之后。
	if name != "PROPERTIES" {
		if _, pend := n.drawerRange("PROPERTIES"); pend >= 0 {
			at = pend + 1
		}
	}
	n.Doc.insertLines(at, indent+":"+name+":", indent+":END:")
	return at + 1
}

// writeProp 把 Props[key] 写入 PROPERTIES 抽屉（key 不存在于 Props 时删除该属性行）。
func (n *Node) writeProp(key string) {
	key = strings.ToUpper(key)
	val, keep := n.Props[key]
	start, end := n.drawerRange("PROPERTIES")
	if start >= 0 && end > start {
		for i := start + 1; i < end; i++ {
			m := propRe.FindStringSubmatch(n.Doc.Lines[i])
			if m == nil || strings.ToUpper(m[1]) != key {
				continue
			}
			if !keep {
				n.Doc.removeLine(i)
				return
			}
			n.Doc.Lines[i] = fmt.Sprintf("%s:%s: %s", indentOf(n.Doc.Lines[i]), key, val)
			return
		}
	}
	if !keep {
		return
	}
	at := n.ensureDrawer("PROPERTIES")
	n.Doc.insertLines(at, fmt.Sprintf("%s:%s: %s", n.bodyIndent(), key, val))
}

// SetProp 设置一个属性并写回；val 为空字符串表示删除该属性。
func (d *Document) SetProp(n *Node, key, val string) *Node {
	line := n.Line
	key = strings.ToUpper(key)
	if val == "" {
		delete(n.Props, key)
	} else {
		n.Props[key] = val
	}
	n.writeProp(key)
	d.Reparse()
	return d.NodeAt(line)
}

// EnsureID 保证节点带有稳定的 :ID: 属性，返回该 ID 和重新解析后的节点。
func (d *Document) EnsureID(n *Node) (string, *Node) {
	if id := n.Props["ID"]; id != "" {
		return id, n
	}
	id := newID()
	return id, d.SetProp(n, "ID", id)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("orgtui-%d", time.Now().UnixNano())
	}
	h := hex.EncodeToString(b[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// ClockIn 为节点开始一段计时。已有进行中的计时时返回错误。
func (d *Document) ClockIn(n *Node, at time.Time) (*Node, error) {
	if n.RunningClock() != nil {
		return n, fmt.Errorf("该条目已有进行中的计时")
	}
	line := n.Line
	pos := n.ensureDrawer("LOGBOOK")
	ts := &Timestamp{Start: at, HasTime: true}
	n.Doc.insertLines(pos, n.bodyIndent()+"CLOCK: "+ts.String())
	d.Reparse()
	return d.NodeAt(line), nil
}

// ClockOut 结束节点进行中的计时。
func (d *Document) ClockOut(n *Node, at time.Time) (*Node, error) {
	c := n.RunningClock()
	if c == nil {
		return n, fmt.Errorf("该条目没有进行中的计时")
	}
	line := n.Line
	end := &Timestamp{Start: at, HasTime: true}
	dur := at.Sub(c.Start.Start)
	if dur < 0 {
		dur = 0
	}
	d.Lines[c.Line] = fmt.Sprintf("%sCLOCK: %s--%s =>  %d:%02d",
		indentOf(d.Lines[c.Line]), c.Start.String(), end.String(),
		int(dur.Hours()), int(dur.Minutes())%60)
	d.Reparse()
	return d.NodeAt(line), nil
}

// NewNodeSpec 描述要新建的条目。
type NewNodeSpec struct {
	Title     string
	TODO      string
	Priority  string
	Tags      []string
	Scheduled *Timestamp
	Deadline  *Timestamp
	Body      []string
	// Parent 为 nil 时追加为顶层节点。
	Parent *Node
}

// AddNode 在文档中追加一个新条目，返回新节点。
// 注意：文档会被重新解析，调用前持有的其他节点指针随之失效。
func (d *Document) AddNode(spec NewNodeSpec) *Node {
	level, at := 1, len(d.Lines)
	if spec.Parent != nil {
		level = spec.Parent.Level + 1
		at = spec.Parent.End
	}
	n := &Node{
		Doc: d, Level: level, TODO: spec.TODO, Priority: spec.Priority,
		Title: spec.Title, Tags: spec.Tags, Props: map[string]string{},
	}
	lines := []string{n.String()}
	var planning []string
	if spec.Deadline != nil {
		planning = append(planning, "DEADLINE: "+spec.Deadline.String())
	}
	if spec.Scheduled != nil {
		planning = append(planning, "SCHEDULED: "+spec.Scheduled.String())
	}
	if len(planning) > 0 {
		lines = append(lines, strings.Join(planning, " "))
	}
	lines = append(lines, spec.Body...)

	// 与前一行之间留出空行，避免黏连到上一段正文。
	if at > 0 && at <= len(d.Lines) && at-1 < len(d.Lines) && strings.TrimSpace(d.Lines[at-1]) != "" && spec.Parent == nil {
		lines = append([]string{""}, lines...)
	}
	d.insertLines(at, lines...)
	d.Reparse()
	for _, node := range d.Nodes {
		if node.Line >= at && node.Title == spec.Title {
			return node
		}
	}
	return nil
}

// RepeatTarget 返回节点上适合挂重复规则的计划项：优先 SCHEDULED，其次 DEADLINE。
// 两者都没有时返回空字符串。
func (n *Node) RepeatTarget() string {
	switch {
	case n.Scheduled != nil:
		return "SCHEDULED"
	case n.Deadline != nil:
		return "DEADLINE"
	default:
		return ""
	}
}

// Repeater 返回节点当前的重复规则，没有则返回 nil。
func (n *Node) Repeater() *Repeater {
	if n.Scheduled != nil && n.Scheduled.Repeat != nil {
		return n.Scheduled.Repeat
	}
	if n.Deadline != nil && n.Deadline.Repeat != nil {
		return n.Deadline.Repeat
	}
	return nil
}

// SetRepeat 给节点设置或清除重复规则，时间本身不变。
// key 为空时按 RepeatTarget 自动选择挂在计划还是截止上。
func (d *Document) SetRepeat(n *Node, key string, r *Repeater) (*Node, error) {
	if key == "" {
		key = n.RepeatTarget()
	}
	key = strings.ToUpper(key)

	var ts *Timestamp
	switch key {
	case "SCHEDULED":
		ts = n.Scheduled
	case "DEADLINE":
		ts = n.Deadline
	default:
		return n, fmt.Errorf("先给这个条目设一个计划或截止时间，才能让它循环")
	}
	if ts == nil {
		return n, fmt.Errorf("这个条目还没有%s时间", map[string]string{
			"SCHEDULED": "计划", "DEADLINE": "截止",
		}[key])
	}

	line := n.Line
	ts.Repeat = r
	ts.Raw = "" // 原始文本已失效，按结构重新渲染
	// 清除重复规则时，另一个计划项上的重复规则也一并清掉，避免只去掉一半。
	if r == nil {
		for _, other := range []*Timestamp{n.Scheduled, n.Deadline} {
			if other != nil && other != ts && other.Repeat != nil {
				other.Repeat, other.Raw = nil, ""
			}
		}
	}
	n.writePlanning()
	d.Reparse()
	return d.NodeAt(line), nil
}

// ToggleCheckbox 切换节点正文里第 idx 个复选框的勾选状态，并同步标题上的进度 cookie。
func (d *Document) ToggleCheckbox(n *Node, idx int) (*Node, error) {
	if idx < 0 || idx >= len(n.Boxes) {
		return n, fmt.Errorf("这个条目没有第 %d 个复选框", idx+1)
	}
	line := n.Line
	box := n.Boxes[idx]
	mark, replacement := "[ ]", "[X]"
	if box.Done {
		mark, replacement = "[X]", "[ ]"
	}
	text := d.Lines[box.Line]
	i := strings.Index(text, mark)
	if i < 0 {
		// 大小写或半角差异导致找不到，退回按通用模式替换
		if i = strings.Index(strings.ToUpper(text), mark); i < 0 {
			return n, fmt.Errorf("这一行看不出复选框状态")
		}
	}
	d.Lines[box.Line] = text[:i] + replacement + text[i+len(mark):]

	d.Reparse()
	n = d.NodeAt(line)
	if n != nil {
		n.updateCookie()
		d.Reparse()
		n = d.NodeAt(line)
	}
	return n, nil
}

// updateCookie 按当前进度刷新标题上的 [n/m] 或 [p%] 标记。
func (n *Node) updateCookie() {
	if n.Cookie == "" {
		return
	}
	done, total := n.Progress()
	if strings.HasSuffix(n.Cookie, "%]") {
		pct := 0
		if total > 0 {
			pct = done * 100 / total
		}
		n.Cookie = fmt.Sprintf("[%d%%]", pct)
	} else {
		n.Cookie = fmt.Sprintf("[%d/%d]", done, total)
	}
	n.Doc.Lines[n.Line] = n.String()
}

// RefreshCookie 重算标题上的进度标记并写回，没有标记时什么也不做。
func (d *Document) RefreshCookie(n *Node) *Node {
	if n.Cookie == "" {
		return n
	}
	line := n.Line
	n.updateCookie()
	d.Reparse()
	return d.NodeAt(line)
}

// DeleteNode 删除节点及其整个子树，返回被删掉的行数。
// 调用后此前持有的节点指针全部失效。
func (d *Document) DeleteNode(n *Node) int {
	start, end := n.Line, n.End
	if start < 0 || start >= len(d.Lines) {
		return 0
	}
	if end > len(d.Lines) {
		end = len(d.Lines)
	}
	// 顺带吃掉子树后面紧跟的空行，免得删完留下一串空档。
	for end < len(d.Lines) && strings.TrimSpace(d.Lines[end]) == "" {
		end++
	}
	removed := end - start
	d.Lines = append(d.Lines[:start:start], d.Lines[end:]...)
	d.Reparse()
	return removed
}

// subtreeNodes 返回节点及其全部子条目，按文档顺序（先序）。
func (n *Node) subtreeNodes() []*Node {
	out := []*Node{n}
	for _, c := range n.Children {
		out = append(out, c.subtreeNodes()...)
	}
	return out
}

// adjustHeadingLevel 把一行标题的星号数增加 delta（负数为减少，至少保留 1 个星号）。
func adjustHeadingLevel(line string, delta int) string {
	stars := 0
	for stars < len(line) && line[stars] == '*' {
		stars++
	}
	if stars == 0 {
		return line
	}
	if delta >= 0 {
		return strings.Repeat("*", delta) + line
	}
	remove := -delta
	if stars > remove {
		return line[remove:]
	}
	return "*" + line[stars:]
}

// Demote 把节点及其子树降一级（星号 +1）。
func (d *Document) Demote(n *Node) *Node {
	line := n.Line
	for _, x := range n.subtreeNodes() {
		d.Lines[x.Line] = adjustHeadingLevel(d.Lines[x.Line], 1)
	}
	d.Reparse()
	return d.NodeAt(line)
}

// Promote 把节点及其子树升一级（星号 -1）；已在顶层时不变。
func (d *Document) Promote(n *Node) *Node {
	if n.Level <= 1 {
		return n
	}
	line := n.Line
	for _, x := range n.subtreeNodes() {
		d.Lines[x.Line] = adjustHeadingLevel(d.Lines[x.Line], -1)
	}
	d.Reparse()
	return d.NodeAt(line)
}

// MoveNode 把节点及其子树移动到 parent 的末尾；parent 为 nil 表示移到文件顶层末尾。
func (d *Document) MoveNode(n, parent *Node) (*Node, error) {
	if parent != nil && containsNode(n, parent) {
		return nil, fmt.Errorf("不能移动到自身或其子条目下")
	}

	title := n.Title
	start := n.Line
	subtreeEnd := n.End
	removeEnd := subtreeEnd
	for removeEnd < len(d.Lines) && strings.TrimSpace(d.Lines[removeEnd]) == "" {
		removeEnd++
	}
	subtree := append([]string(nil), d.Lines[start:subtreeEnd]...)
	removed := removeEnd - start

	// 计算插入点（删除后的行号坐标）。
	target := len(d.Lines) - removed
	targetLevel := 1
	if parent != nil {
		targetLevel = parent.Level + 1
		if n.Line < parent.Line {
			target = parent.End - removed
		} else {
			target = parent.End
		}
	}

	// 删除子树。
	d.Lines = append(d.Lines[:start], d.Lines[removeEnd:]...)

	// 调整子树标题层级。
	if delta := targetLevel - n.Level; delta != 0 {
		for _, x := range n.subtreeNodes() {
			off := x.Line - start
			if off >= 0 && off < len(subtree) {
				subtree[off] = adjustHeadingLevel(subtree[off], delta)
			}
		}
	}

	// 与上一行之间留空行。
	if target > len(d.Lines) {
		target = len(d.Lines)
	}
	d.Lines = append(d.Lines[:target], append(subtree, d.Lines[target:]...)...)
	d.Reparse()

	for _, x := range d.Nodes {
		if x.Title == title && x.Line >= target {
			return x, nil
		}
	}
	return nil, fmt.Errorf("移动后未找到节点")
}

// containsNode 报告 node 是否是 maybeDesc 的祖先（即 maybeDesc 在 node 的子树里）。
func containsNode(node, maybeDesc *Node) bool {
	for cur := maybeDesc; cur != nil; cur = cur.Parent {
		if cur == node {
			return true
		}
	}
	return false
}

// Subtree 返回节点及其子树的原始行，可用于删除前留底或移动。
func (n *Node) Subtree() []string {
	end := n.End
	if end > len(n.Doc.Lines) {
		end = len(n.Doc.Lines)
	}
	if n.Line >= end {
		return nil
	}
	return append([]string(nil), n.Doc.Lines[n.Line:end]...)
}

// DescendantCount 返回子树里除自身之外的条目数。
func (n *Node) DescendantCount() int {
	count := 0
	var walk func(*Node)
	walk = func(cur *Node) {
		for _, c := range cur.Children {
			count++
			walk(c)
		}
	}
	walk(n)
	return count
}

// logStateChange 往 LOGBOOK 里记一条状态变更，新的记录排在最前面。
func (n *Node) logStateChange(from, to string, at time.Time) {
	pos := n.ensureDrawer("LOGBOOK")
	ts := &Timestamp{Start: at, HasTime: true}
	n.Doc.insertLines(pos, fmt.Sprintf(`%s- State %-12s from %-12s %s`,
		n.bodyIndent(), quoteState(to), quoteState(from), ts.String()))
}

// quoteState 把状态包成 org 日志里的 "DONE" 形式，无状态时记成 ""。
func quoteState(s string) string { return `"` + s + `"` }

// AddNote 往 LOGBOOK 里记一条笔记。
func (d *Document) AddNote(n *Node, text string, at time.Time) *Node {
	text = strings.TrimSpace(text)
	if text == "" {
		return n
	}
	line := n.Line
	pos := n.ensureDrawer("LOGBOOK")
	ts := &Timestamp{Start: at, HasTime: true}
	indent := n.bodyIndent()
	lines := []string{fmt.Sprintf("%s- Note taken on %s \\\\", indent, ts.String())}
	for _, l := range strings.Split(text, "\n") {
		lines = append(lines, indent+"  "+l)
	}
	d.insertLines(pos, lines...)
	d.Reparse()
	return d.NodeAt(line)
}

// ReplaceSubtree 用新的内容替换节点及其整棵子树。
// 调用后此前持有的节点指针全部失效。
func (d *Document) ReplaceSubtree(n *Node, lines []string) {
	start, end := n.Line, n.End
	if end > len(d.Lines) {
		end = len(d.Lines)
	}
	if start < 0 || start > len(d.Lines) {
		return
	}
	rest := append([]string{}, d.Lines[end:]...)
	d.Lines = append(append(d.Lines[:start:start], lines...), rest...)
	d.Reparse()
}
