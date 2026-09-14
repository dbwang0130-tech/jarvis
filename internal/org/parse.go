package org

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	headlineRe = regexp.MustCompile(`^(\*+)(?:\s+(.*))?$`)
	keywordRe  = regexp.MustCompile(`^\s*#\+([A-Za-z_]+):\s*(.*)$`)
	tagsRe     = regexp.MustCompile(`\s+(:[\p{L}\p{N}_@#%:]+:)\s*$`)
	prioRe     = regexp.MustCompile(`^\[#([A-Za-z0-9])\]\s*`)
	planningRe = regexp.MustCompile(`\b(SCHEDULED|DEADLINE|CLOSED):\s*(<[^>\n]*>|\[[^\]\n]*\])`)
	clockRe    = regexp.MustCompile(`^\s*CLOCK:\s*(.*)$`)
	logStateRe = regexp.MustCompile(`^\s*-\s+State\s+"([^"]*)"\s+from\s+"([^"]*)"\s*(.*)$`)
	logItemRe  = regexp.MustCompile(`^\s*-\s+(.*)$`)
	propRe     = regexp.MustCompile(`^\s*:([^:\s]+):\s*(.*)$`)
	drawerRe   = regexp.MustCompile(`^\s*:([A-Za-z][A-Za-z0-9_-]*):\s*$`)
	drawerEnd  = regexp.MustCompile(`^\s*:END:\s*$`)
	blockBegin = regexp.MustCompile(`(?i)^\s*#\+BEGIN_(\w+)`)
	blockEnd   = regexp.MustCompile(`(?i)^\s*#\+END_(\w+)`)
	checkboxRe = regexp.MustCompile(`^\s*[-+*]\s+\[([ xX＋-])\]\s`)
	cookieRe   = regexp.MustCompile(`\s*\[(\d+/\d+|\d+%)\]`)
)

// CommentKeyword 标记一个不参与 agenda 的子树，与 org 的 COMMENT 语义一致。
const CommentKeyword = "COMMENT"

// ArchiveTag 标记已归档的子树，同样不参与 agenda。
const ArchiveTag = "ARCHIVE"

// ParseFile 读取并解析一个 org 文件。
func ParseFile(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := Parse(string(data))
	doc.Path = path
	if fi, err := os.Stat(path); err == nil {
		doc.ModTime = fi.ModTime()
	}
	return doc, nil
}

// Parse 解析 org 文本。
func Parse(text string) *Document {
	doc := &Document{
		TodoKeywords: append([]string(nil), DefaultTodoKeywords...),
		DoneKeywords: append([]string(nil), DefaultDoneKeywords...),
	}
	sc := bufio.NewScanner(bytes.NewBufferString(text))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		doc.Lines = append(doc.Lines, sc.Text())
	}
	// 保留结尾空行的信息：以换行结尾的文件，最后一个元素不应被吞掉。
	if strings.HasSuffix(text, "\n") && len(doc.Lines) > 0 {
		// bufio.Scanner 已正确处理，这里无需补行。
		_ = text
	}

	doc.parseKeywords()
	doc.parseNodes()
	return doc
}

// parseKeywords 收集文件级的 #+KEY: value 设置。
func (d *Document) parseKeywords() {
	for _, line := range d.Lines {
		if strings.HasPrefix(strings.TrimSpace(line), "*") {
			continue
		}
		m := keywordRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := strings.ToUpper(m[1]), strings.TrimSpace(m[2])
		switch key {
		case "TITLE":
			d.Title = val
		case "CATEGORY":
			d.CategoryName = val
		case "FILETAGS":
			for _, t := range strings.Split(strings.Trim(val, ": \t"), ":") {
				if t = strings.TrimSpace(t); t != "" {
					d.FileTags = append(d.FileTags, t)
				}
			}
		case "STARTUP":
			for _, opt := range strings.Fields(val) {
				switch strings.ToLower(opt) {
				case "logdone", "lognotestate":
					d.LogStates = true
				case "nologdone", "nologstate":
					d.LogStates = false
				}
			}
		case "TODO", "SEQ_TODO", "TYP_TODO":
			todo, done := parseTodoSpec(val)
			if len(todo)+len(done) > 0 {
				d.TodoKeywords, d.DoneKeywords = todo, done
			}
		}
	}
}

// hasStartupLogSetting 报告文件是否用 #+STARTUP: 显式指定过日志开关。
func (d *Document) hasStartupLogSetting() bool {
	for _, line := range d.Lines {
		m := keywordRe.FindStringSubmatch(line)
		if m == nil || !strings.EqualFold(m[1], "STARTUP") {
			continue
		}
		for _, opt := range strings.Fields(m[2]) {
			switch strings.ToLower(opt) {
			case "logdone", "lognotestate", "nologdone", "nologstate":
				return true
			}
		}
	}
	return false
}

// parseTodoSpec 解析 "TODO NEXT | DONE CANCELLED" 形式的关键字定义。
// 没有竖线时，最后一个关键字视为完成态（与 org 的行为一致）。
func parseTodoSpec(spec string) (todo, done []string) {
	clean := func(s string) string {
		if i := strings.IndexByte(s, '('); i >= 0 { // 去掉 "TODO(t)" 里的快捷键
			s = s[:i]
		}
		return strings.TrimSpace(s)
	}
	if bar := strings.Index(spec, "|"); bar >= 0 {
		for _, f := range strings.Fields(spec[:bar]) {
			if c := clean(f); c != "" {
				todo = append(todo, c)
			}
		}
		for _, f := range strings.Fields(spec[bar+1:]) {
			if c := clean(f); c != "" {
				done = append(done, c)
			}
		}
		return todo, done
	}
	var all []string
	for _, f := range strings.Fields(spec) {
		if c := clean(f); c != "" {
			all = append(all, c)
		}
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all[:len(all)-1], all[len(all)-1:]
}

// parseNodes 扫描出全部标题并填充每个节点的元数据。
func (d *Document) parseNodes() {
	var stack []*Node
	for i, line := range d.Lines {
		m := headlineRe.FindStringSubmatch(line)
		if m == nil || len(m[1]) == 0 {
			continue
		}
		n := &Node{
			Doc:   d,
			Level: len(m[1]),
			Line:  i,
			Props: map[string]string{},
		}
		d.parseHeadline(n, m[2])

		// 出栈直到找到父节点。
		for len(stack) > 0 && stack[len(stack)-1].Level >= n.Level {
			stack[len(stack)-1].End = i
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			n.Parent = stack[len(stack)-1]
			n.Parent.Children = append(n.Parent.Children, n)
		} else {
			d.Roots = append(d.Roots, n)
		}
		stack = append(stack, n)
		d.Nodes = append(d.Nodes, n)
	}
	for _, n := range stack {
		n.End = len(d.Lines)
	}
	for _, n := range d.Nodes {
		d.parseBody(n)
	}
}

// parseHeadline 从标题文本中拆出 TODO 关键字、优先级、标签和标题。
func (d *Document) parseHeadline(n *Node, text string) {
	text = strings.TrimRight(text, " \t")
	if m := tagsRe.FindStringSubmatch(text); m != nil {
		for _, t := range strings.Split(strings.Trim(m[1], ":"), ":") {
			if t != "" {
				n.Tags = append(n.Tags, t)
			}
		}
		text = strings.TrimRight(text[:len(text)-len(m[0])], " \t")
	}
	if fields := strings.Fields(text); len(fields) > 0 {
		for _, kw := range d.AllKeywords() {
			if fields[0] == kw {
				n.TODO = kw
				text = strings.TrimLeft(strings.TrimPrefix(text, kw), " \t")
				break
			}
		}
	}
	if m := prioRe.FindStringSubmatch(text); m != nil {
		n.Priority = strings.ToUpper(m[1])
		text = text[len(m[0]):]
	}
	// COMMENT 紧跟在关键字之后，整个子树都不参与 agenda。
	if rest, ok := strings.CutPrefix(text, CommentKeyword); ok &&
		(rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t")) {
		n.Commented = true
		text = strings.TrimLeft(rest, " \t")
	}
	// 记下进度 cookie 的位置，勾选复选框时好同步更新。
	if loc := cookieRe.FindStringIndex(text); loc != nil {
		n.Cookie = strings.TrimSpace(text[loc[0]:loc[1]])
		text = text[:loc[0]] + text[loc[1]:]
	}
	n.Title = strings.TrimSpace(text)
}

// parseBody 解析标题之后、下一个标题之前的内容：
// planning 行、属性抽屉、计时记录以及正文中的活动时间戳。
func (d *Document) parseBody(n *Node) {
	inDrawer, inBlock := "", ""
	for i := n.Line + 1; i < n.End && i < len(d.Lines); i++ {
		line := d.Lines[i]
		if headlineRe.MatchString(line) && strings.HasPrefix(line, "*") {
			break // 子标题由它自己解析
		}

		// #+BEGIN_SRC 之类的块里可能有形似时间戳的文本，不能当成日程。
		if inBlock != "" {
			if m := blockEnd.FindStringSubmatch(line); m != nil &&
				strings.EqualFold(m[1], inBlock) {
				inBlock = ""
			}
			continue
		}
		if m := blockBegin.FindStringSubmatch(line); m != nil {
			inBlock = m[1]
			continue
		}
		if m := checkboxRe.FindStringSubmatch(line); m != nil {
			n.Boxes = append(n.Boxes, Checkbox{
				Line: i,
				Done: m[1] == "x" || m[1] == "X",
				Text: strings.TrimSpace(line[len(m[0]):]),
			})
		}

		if inDrawer != "" {
			if drawerEnd.MatchString(line) {
				inDrawer = ""
				continue
			}
			switch inDrawer {
			case "PROPERTIES":
				if m := propRe.FindStringSubmatch(line); m != nil {
					n.Props[strings.ToUpper(m[1])] = strings.TrimSpace(m[2])
				}
			case "LOGBOOK":
				if m := clockRe.FindStringSubmatch(line); m != nil {
					n.Clocks = append(n.Clocks, parseClock(m[1], i))
					continue
				}
				if e, ok := parseLogEntry(line, i); ok {
					n.Logs = append(n.Logs, e)
				}
			}
			continue
		}

		if m := drawerRe.FindStringSubmatch(line); m != nil && !drawerEnd.MatchString(line) {
			inDrawer = strings.ToUpper(m[1])
			continue
		}
		// LOGBOOK 抽屉之外的裸 CLOCK 行同样计入。
		if m := clockRe.FindStringSubmatch(line); m != nil {
			n.Clocks = append(n.Clocks, parseClock(m[1], i))
			continue
		}
		if pm := planningRe.FindAllStringSubmatch(line, -1); pm != nil {
			for _, p := range pm {
				ts := ParseTimestamp(p[2])
				if ts == nil {
					continue
				}
				switch p[1] {
				case "SCHEDULED":
					n.Scheduled = ts
				case "DEADLINE":
					n.Deadline = ts
				case "CLOSED":
					n.Closed = ts
				}
			}
			continue
		}
		for _, ts := range FindTimestamps(line) {
			if ts.Active {
				n.Timestamps = append(n.Timestamps, ts)
			}
		}
	}
}

// parseLogEntry 解析 LOGBOOK 里的一条非计时记录。
func parseLogEntry(line string, at int) (LogEntry, bool) {
	if m := logStateRe.FindStringSubmatch(line); m != nil {
		e := LogEntry{Line: at, Kind: LogState, To: m[1], From: m[2], Text: strings.TrimSpace(m[0])}
		if stamps := FindTimestamps(m[3]); len(stamps) > 0 {
			e.Time = stamps[0]
		}
		return e, true
	}
	m := logItemRe.FindStringSubmatch(line)
	if m == nil {
		return LogEntry{}, false
	}
	e := LogEntry{Line: at, Kind: LogOther, Text: strings.TrimSpace(m[1])}
	if strings.HasPrefix(e.Text, "Note taken on") {
		e.Kind = LogNote
	}
	if stamps := FindTimestamps(m[1]); len(stamps) > 0 {
		e.Time = stamps[0]
	}
	return e, true
}

// parseClock 解析 "CLOCK: [start]--[end] =>  1:30" 的记录体。
func parseClock(body string, line int) Clock {
	c := Clock{Line: line}
	stamps := FindTimestamps(body)
	if len(stamps) > 0 {
		// 区间形式会被合并成一个带 End 的时间戳。
		c.Start = stamps[0]
		if stamps[0].HasEnd() {
			c.End = &Timestamp{Start: stamps[0].End, HasTime: true, Active: false}
			c.Start = &Timestamp{Start: stamps[0].Start, HasTime: stamps[0].HasTime, Active: stamps[0].Active}
		} else if len(stamps) > 1 {
			c.End = stamps[1]
		}
	}
	return c
}

// String 重新拼出节点的标题行。
func (n *Node) String() string {
	var b strings.Builder
	b.WriteString(strings.Repeat("*", n.Level))
	if n.TODO != "" {
		fmt.Fprintf(&b, " %s", n.TODO)
	}
	if n.Priority != "" {
		fmt.Fprintf(&b, " [#%s]", n.Priority)
	}
	if n.Commented {
		b.WriteString(" " + CommentKeyword)
	}
	if n.Title != "" {
		fmt.Fprintf(&b, " %s", n.Title)
	}
	if n.Cookie != "" {
		fmt.Fprintf(&b, " %s", n.Cookie)
	}
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, " :%s:", strings.Join(n.Tags, ":"))
	}
	return b.String()
}
