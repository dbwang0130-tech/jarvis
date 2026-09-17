// Package orgtools 把 org-mode 日程封装成秘书可用的一项技能（Skill）。
package orgtools

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
)

// Workspace 包装 org 文件集合，负责读写、撤销与修改记录。
type Workspace struct {
	Coll *org.Collection
	Cfg  config.Config
	// Now 可注入以便测试，零值时使用 time.Now。
	Now func() time.Time
	// Changes 记录本次会话中秘书做过的修改，供界面展示与撤销。
	Changes []Change
	// snapshots 保存每个文件被首次修改前的内容，用于整体撤销。
	snapshots map[string][]string
}

// Change 是秘书做出的一次修改记录。
type Change struct {
	Summary string
	File    string
	Time    time.Time
}

// NewWorkspace 构造工作区。
func NewWorkspace(coll *org.Collection, cfg config.Config) *Workspace {
	return &Workspace{Coll: coll, Cfg: cfg}
}

func (w *Workspace) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *Workspace) snapshot(d *org.Document) {
	if w.snapshots == nil {
		w.snapshots = map[string][]string{}
	}
	if _, ok := w.snapshots[d.Path]; !ok {
		w.snapshots[d.Path] = append([]string(nil), d.Lines...)
	}
}

// Snapshot 在改动文件之前留底，让这次改动可以被 Undo 撤销。
func (w *Workspace) Snapshot(d *org.Document) { w.snapshot(d) }

// Record 记一笔改动，摘要会显示在界面上。
func (w *Workspace) Record(d *org.Document, format string, args ...any) {
	w.record(d, format, args...)
}

// Undo 把本次会话修改过的文件恢复到秘书动手之前的状态。
func (w *Workspace) Undo() error {
	for path, lines := range w.snapshots {
		d := w.Coll.Doc(path)
		if d == nil {
			continue
		}
		d.Lines = append([]string(nil), lines...)
		d.Reparse()
		if err := d.Save(); err != nil {
			return err
		}
	}
	w.snapshots = nil
	w.Changes = nil
	return nil
}

// HasChanges 报告本次会话是否产生过修改。
func (w *Workspace) HasChanges() bool { return len(w.Changes) > 0 }

// ResetChanges 清空修改记录（例如用户确认保留之后）。
func (w *Workspace) ResetChanges() {
	w.Changes = nil
	w.snapshots = nil
}

func (w *Workspace) record(d *org.Document, format string, args ...any) {
	w.Changes = append(w.Changes, Change{
		Summary: fmt.Sprintf(format, args...),
		File:    filepath.Base(d.Path),
		Time:    w.now(),
	})
}

// ref 形如 "work.org:12"，指向某个文件第 12 行（0 起）的标题。
func refOf(d *org.Document, n *org.Node) string {
	return fmt.Sprintf("%s:%d", filepath.Base(d.Path), n.Line)
}

var refRe = regexp.MustCompile(`^(.+?):(\d+)$`)

// resolve 按 ref 定位节点；行号漂移时退回按标题匹配。
// save 把改动写回磁盘，顺带处理「文件被别人改过」这件事。
//
// 撞上之后必须把那份重新读进来，否则手里这份永远是旧的，之后每一次保存
// 都会被拒，直到重启——拒绝是为了让人重来一次，不是把路堵死。
//
// 重读之后行号全变了，所以不能替调用方重试：得让模型重新查一次引用。
// 错误信息里说的就是这件事。
func (w *Workspace) save(d *org.Document) error {
	err := d.Save()
	if !errors.Is(err, org.ErrStale) {
		return err
	}
	// 整个集合重读。只重读一个文件的话，别的文件也可能同时被改过，
	// 而下一次保存会撞上同样的事——一次收干净。
	_ = w.Coll.Reload()
	return fmt.Errorf("%w。我已经重新读了一遍，请重新查一次引用再改", err)
}

func (w *Workspace) resolve(ref, title string) (*org.Document, *org.Node, error) {
	ref = strings.TrimSpace(ref)
	m := refRe.FindStringSubmatch(ref)
	if m == nil {
		if title == "" {
			return nil, nil, fmt.Errorf("引用 %q 格式不对，应为 文件名:行号（例如 work.org:12）", ref)
		}
		return w.findByTitle(title)
	}
	doc := w.Coll.Doc(m[1])
	if doc == nil {
		return nil, nil, fmt.Errorf("找不到文件 %s", m[1])
	}
	line, _ := strconv.Atoi(m[2])
	if n := doc.NodeAt(line); n != nil {
		if title == "" || n.Title == title {
			return doc, n, nil
		}
	}
	if title != "" {
		var hits []*org.Node
		for _, n := range doc.Nodes {
			if n.Title == title {
				hits = append(hits, n)
			}
		}
		if len(hits) == 1 {
			return doc, hits[0], nil
		}
		if len(hits) > 1 {
			return nil, nil, fmt.Errorf("文件 %s 中有多个标题为 %q 的条目，请先用 list_todos 取得最新引用", m[1], title)
		}
	}
	return nil, nil, fmt.Errorf("引用 %s 已失效，请重新查询后再操作", ref)
}

func (w *Workspace) findByTitle(title string) (*org.Document, *org.Node, error) {
	var hitDoc *org.Document
	var hits []*org.Node
	for _, d := range w.Coll.Docs {
		for _, n := range d.Nodes {
			if n.Title == title {
				hitDoc, hits = d, append(hits, n)
			}
		}
	}
	switch len(hits) {
	case 0:
		return nil, nil, fmt.Errorf("找不到标题为 %q 的条目", title)
	case 1:
		return hitDoc, hits[0], nil
	default:
		return nil, nil, fmt.Errorf("有多个标题为 %q 的条目，请提供 ref 精确指定", title)
	}
}

func (w *Workspace) agendaOpts() agenda.Options {
	o := agenda.DefaultOptions()
	o.DeadlineWarnDays = w.Cfg.DeadlineWarnDays
	o.Now = w.now()
	return o
}

func (w *Workspace) workHours() agenda.WorkHours {
	return agenda.WorkHours{StartHour: w.Cfg.WorkHours.Start, EndHour: w.Cfg.WorkHours.End}
}

func formatDate(t time.Time) string { return org.FormatDate(t) }

func parseTime(s string, base time.Time) (*org.Timestamp, error) {
	return org.ParseUserTime(s, base)
}

// Skill 让日程场景接入秘书。
type Skill struct {
	WS *Workspace
}

// NewSkill 构造日程技能。
func NewSkill(ws *Workspace) *Skill { return &Skill{WS: ws} }

func (p *Skill) Name() string { return "日程" }

// Describe 描述当前环境：时间、可安排时段、org 文件与 TODO 状态。
func (p *Skill) Describe(b *strings.Builder) {
	now := p.WS.now()
	var files []string
	keywords := map[string]bool{}
	for _, d := range p.WS.Coll.Docs {
		files = append(files, fmt.Sprintf("%s（类别 %s）", filepath.Base(d.Path), d.Category()))
		for _, k := range d.AllKeywords() {
			keywords[k] = true
		}
	}
	var kws []string
	for k := range keywords {
		kws = append(kws, k)
	}
	fmt.Fprintf(b, "当前时间：%s %s\n", formatDate(now), now.Format("15:04"))
	fmt.Fprintf(b, "可安排时段：每天 %02d:00-%02d:00\n", p.WS.Cfg.WorkHours.Start, p.WS.Cfg.WorkHours.End)
	fmt.Fprintf(b, "org 文件：%s\n", strings.Join(files, "、"))
	fmt.Fprintf(b, "可用的 TODO 状态：%s\n", strings.Join(kws, "、"))
}

// Instructions 是日程场景的行为规则。
func (p *Skill) Instructions() string {
	return `日程管理：
1. 动手之前先查清楚。要排期就先用 find_free_slots 看空档，要改条目就先用 list_todos 或 list_agenda 拿到准确的 ref。
2. 绝不凭空编造 ref，只能使用查询结果里返回的 ref。修改操作会让行号变化，改完之后如需再改别的条目，重新查询。
3. 安排任务时考虑合理性：高优先级和临近截止的先排；给任务留出符合 :EFFORT: 或常识的时长；别把事情排进已占用的时段；除非用户要求，不要排到深夜。
4. 用户说"明天""下周"这类相对时间时，你负责换算成具体日期再调用工具。
5. 说到"每周""每天""固定"这类反复发生的事，用 set_repeat 把它设成循环任务，不要一条条手工新建。
6. **用户描述一件事的状态变化，那是指令不是通知**，照着做，别等他再说一遍：
   - 「开始做 X」「X 开始了」「现在弄 X」→ set_todo_state 改成进行中（用该文件里表示进行中的
     关键字，一般是 NEXT；没有就保持原状），然后 clock_in 开始计时。
   - 「做完了」「搞定了」「X 完成」→ 正在计时就先 clock_out，再 set_todo_state 标完成。
   - 「先停一下」「歇会儿」「不弄了先」→ clock_out 停止计时，状态退回 TODO。
   - 「推迟到明天」「改到下周三」「挪到下午三点」→ schedule_item 改时间，别新建一条。
   - 「这事不做了」「取消吧」→ **不要删**。文件里有 CANCELLED 之类的取消态就标成那个，
     没有就标完成并 add_note 写明是取消的。
   - 「花了多少钱」「买了什么」→ 通常是要记账。
   都先用 list_todos 找到准确的 ref 再动手。同名的有多条时挑今天该做的那条；确实找不到，
   才回一句没找到。
7. 同一时间只给一个条目计时。要开始新的，先把还在计时的那个 clock_out——两个一起计时，
   统计出来的工时就是假的。
8. delete_item 会真的删掉内容，只有用户明确说"删掉""不要了"时才用；事情做完了应该标记完成，而不是删除。删之前先确认标题对得上。`
}

// Tools 返回日程场景的全部工具。
func (p *Skill) Tools() []agent.Tool {
	ws := p.WS
	defs := wsToolDefs()
	tools := make([]agent.Tool, 0, len(defs))
	for _, d := range defs {
		name := d.Name
		tools = append(tools, orgTool{spec: d, readonly: orgReadOnly(name), exec: func(args string) (string, bool, error) {
			return ws.execOrg(name, args)
		}})
	}
	return tools
}

// orgReadOnly 标记哪些日程工具是只读、可并发的。
func orgReadOnly(name string) bool {
	switch name {
	case "list_agenda", "list_todos", "find_free_slots", "get_item":
		return true
	}
	return false
}
