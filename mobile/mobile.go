// Package mobile 是给 iOS 端用的桥接层。
//
// iOS 上文件在沙箱里，iCloud 的文件还要经 NSFileCoordinator 协调读写，
// 所以这一层刻意做成**纯函数**：文件内容由 Swift 那边读进来、改完再由
// Swift 写回去，Go 这头一个文件都不碰。这样沙箱、iCloud 占位符、安全
// 作用域那些事全留在 Swift 侧，Go 只管解析和计算——两端共用同一套 org
// 解析，不会各解析各的解出两个样。
//
// 进出都用 JSON 字符串：gomobile 能导出的类型很有限（基本类型、[]byte、
// error 之类），map 和结构体切片都过不去，JSON 是最省事的办法。
package mobile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	// 把时区数据库打进二进制（约 450KB）。gomobile 编出来的 Go 在 iOS 沙箱里
	// 读不到系统的 zoneinfo，不带这个的话 LoadLocation 一律失败。
	_ "time/tzdata"

	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/org"
)

// SnapshotInput 是 Snapshot 的入参。
type SnapshotInput struct {
	// Files 是 org 文件，键是文件名（也用来拼条目引用），值是全文。
	Files map[string]string `json:"files"`
	// Ledger 是账本全文，没有就留空。
	Ledger string `json:"ledger"`
	// Days 是日程往后看几天，零值按 7 天算。
	Days int `json:"days"`
	// TodoLimit 是待办最多给几条，零值按 1000 算。见 buildTodos。
	TodoLimit int `json:"todo_limit"`
	// Now 可以指定「现在」，留空就是真的现在（测试用）。
	Now string `json:"now"`
	// TZ 是设备的时区名（如 Asia/Shanghai）。见 applyTimeZone。
	TZ string `json:"tz"`
}

var (
	parseMu    sync.Mutex
	parseKey   string
	parseCache *org.Collection
)

// collectCached 解析一批文件，内容没变就复用上次的结果。
//
// Snapshot 在每次下拉刷新、每轮对话结束后都会被调一次，而文件多半一个字
// 都没动——五千条重新解析一遍要 150ms 起步，全花在重复劳动上。只留最近
// 一份就够了：界面任何时候看的都是同一份内容。
//
// 缓存的 Collection 是只读的——agenda 那边只读不写，Ask 改文件走的是自己
// 的临时目录。哪天 agenda 开始改节点，这里要一并重新考虑。
func collectCached(files map[string]string) *org.Collection {
	key := filesKey(files)
	parseMu.Lock()
	defer parseMu.Unlock()
	if parseCache != nil && parseKey == key {
		return parseCache
	}
	c := collect(files)
	parseKey, parseCache = key, c
	return c
}

func filesKey(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%s\x00%d\x00", n, len(files[n]))
		h.Write([]byte(files[n]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var tzMu sync.Mutex

// applyTimeZone 把进程的本地时区设成设备的那个。
//
// gomobile 编出来的 Go 在 iOS 上找不到系统时区库，time.Local 会退成 UTC——
// 「今天」「明天」「这周」全按 UTC 算，东八区下午六点之后就整体差一天。
// 桌面端读得到 /etc/localtime，所以只有手机上不对。
//
// 时区是进程级的全局状态，但在 iOS 上这个进程就只有这一个 app，设一次就行。
func applyTimeZone(name string) {
	if name == "" {
		return
	}
	tzMu.Lock()
	defer tzMu.Unlock()
	if time.Local != nil && time.Local.String() == name {
		return
	}
	if loc, err := time.LoadLocation(name); err == nil {
		time.Local = loc
	}
}

// SnapshotOutput 是界面要显示的东西。
type SnapshotOutput struct {
	Days  []Day  `json:"days"`
	Todos []Item `json:"todos"`
	// TodosTotal 是截断前的总数，界面据此提示还有多少没显示。
	TodosTotal int         `json:"todos_total"`
	Ledger     *LedgerView `json:"ledger,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// Day 是日程里的一天。
type Day struct {
	Date    string `json:"date"`
	Weekday string `json:"weekday"`
	Today   bool   `json:"today"`
	Items   []Item `json:"items"`
}

// Item 是一条日程或待办。
type Item struct {
	Ref      string   `json:"ref"`   // 形如 work.org:12，改动时要原样传回来
	Kind     string   `json:"kind"`  // 计划 / 截止 / 事件 / 逾期 / 待办
	Time     string   `json:"time"`  // 10:00-11:30，没有就空着
	State    string   `json:"state"` // TODO / NEXT / DONE…
	Priority string   `json:"priority"`
	Title    string   `json:"title"`
	Parents  string   `json:"parents"` // 父条目路径，子任务才有
	Tags     []string `json:"tags"`
	Done     int      `json:"done"`  // 勾选项进度
	Total    int      `json:"total"` //
	Repeat   string   `json:"repeat"`
	Overdue  bool     `json:"overdue"`
	Category string   `json:"category"`

	// HasNotes 只说有没有正文，正文本身走 Detail 单独取。
	// 正文和勾选项挂在每一条上的话，单条从约 180 字节涨到 900——
	// 五千条就是 4.5MB，每次刷新都要过一遍 gomobile 的桥。
	HasNotes bool   `json:"has_notes"`
	File     string `json:"file"` // 来自哪个文件
}

// DetailInput 是 Detail 的入参：拿 ref 要一条的详情。
type DetailInput struct {
	Files map[string]string `json:"files"`
	Ref   string            `json:"ref"`
	TZ    string            `json:"tz"`
}

// DetailOutput 只带列表里没有的那几样，界面拿到后跟手上的条目合起来显示。
type DetailOutput struct {
	Scheduled string     `json:"scheduled"`
	Deadline  string     `json:"deadline"`
	Effort    string     `json:"effort"`
	Clocked   int        `json:"clocked"`
	Notes     string     `json:"notes"`
	Boxes     []Checkbox `json:"boxes"`
	Error     string     `json:"error,omitempty"`
}

func (o *DetailOutput) normalize() {
	if o.Boxes == nil {
		o.Boxes = []Checkbox{}
	}
}

// Detail 取一条条目的详情。ref 形如 work.org:12，用列表里给的那个。
func Detail(inputJSON string) string {
	var in DetailInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&DetailOutput{Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	applyTimeZone(in.TZ)

	path, line, ok := splitRef(in.Ref)
	if !ok {
		return marshal(&DetailOutput{Error: fmt.Sprintf("引用 %q 不合法", in.Ref)})
	}
	text, ok := in.Files[path]
	if !ok {
		return marshal(&DetailOutput{Error: fmt.Sprintf("没有文件 %s", path)})
	}
	doc := org.Parse(text)
	n := doc.NodeAt(line)
	if n == nil {
		return marshal(&DetailOutput{Error: fmt.Sprintf("%s 第 %d 行没有条目", path, line+1)})
	}

	out := DetailOutput{
		Scheduled: stampText(n.Scheduled),
		Deadline:  stampText(n.Deadline),
		Effort:    n.Props["EFFORT"],
		Clocked:   int(n.ClockedTotal().Minutes()),
		Notes:     strings.TrimSpace(strings.Join(n.Notes(), "\n")),
	}
	for _, b := range n.Boxes {
		out.Boxes = append(out.Boxes, Checkbox{Done: b.Done, Text: b.Text})
	}
	return marshal(&out)
}

// splitRef 拆 "work.org:12"。文件名里可能有冒号，从最后一个切。
func splitRef(ref string) (path string, line int, ok bool) {
	i := strings.LastIndex(ref, ":")
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(ref[i+1:])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return ref[:i], n, true
}

// Checkbox 是正文里的一个勾选项。
type Checkbox struct {
	Done bool   `json:"done"`
	Text string `json:"text"`
}

// LedgerView 是账本概览。
type LedgerView struct {
	Net      float64    `json:"net"`
	Assets   float64    `json:"assets"`
	Debts    float64    `json:"debts"`
	Accounts []Account  `json:"accounts"`
	Recent   []TxnBrief `json:"recent"`
}

// Account 是一个账户的余额。
type Account struct {
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
}

// TxnBrief 是流水里的一笔。
type TxnBrief struct {
	Date   string  `json:"date"`
	Payee  string  `json:"payee"`
	Amount float64 `json:"amount"`
}

// Snapshot 把一批 org 文件和账本解析成界面要显示的内容。
// 入参出参都是 JSON 字符串；出了问题会填在出参的 error 里，不会 panic。
func Snapshot(inputJSON string) string {
	var in SnapshotInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return errorOut("入参不是合法 JSON：%v", err)
	}

	applyTimeZone(in.TZ)

	now := time.Now()
	if in.Now != "" {
		t, err := time.Parse(time.RFC3339, in.Now)
		if err != nil {
			return errorOut("now 得是 RFC3339 格式：%v", err)
		}
		now = t
	}
	days := in.Days
	if days < 1 {
		days = 7
	}

	coll := collectCached(in.Files)
	opts := agenda.DefaultOptions()
	opts.Now = now

	out := SnapshotOutput{Days: buildDays(coll, now, days, opts)}
	out.Todos, out.TodosTotal = buildTodos(coll, in.TodoLimit)
	if strings.TrimSpace(in.Ledger) != "" {
		out.Ledger = buildLedger(ledger.ParseBook(in.Ledger))
	}
	return marshal(&out)
}

// collect 把「文件名 → 内容」解析成一个集合。
// 文件名同时充当条目引用的前缀，所以两端得用同一个名字。
func collect(files map[string]string) *org.Collection {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names) // 顺序固定，界面才不会跳来跳去

	c := &org.Collection{}
	for _, name := range names {
		doc := org.Parse(files[name])
		doc.Path = name
		c.Docs = append(c.Docs, doc)
	}
	return c
}

func buildDays(c *org.Collection, now time.Time, days int, opts agenda.Options) []Day {
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	ag := agenda.Build(c, day0, days, opts)

	out := make([]Day, 0, len(ag.Days))
	for _, d := range ag.Days {
		// 不能用 d.IsToday()——它按真实时钟算，而这里的 now 是入参给的。
		// 两者不一致时「今天」会标到别的日子上，日程窗口和高亮各说各话。
		today := sameDay(d.Date, now)
		if len(d.Items) == 0 && !today {
			continue // 空日子不占地方，今天除外
		}
		items := make([]Item, 0, len(d.Items))
		for _, it := range d.Items {
			items = append(items, toItem(it))
		}
		out = append(out, Day{
			Date:    d.Date.Format("2006-01-02"),
			Weekday: weekday(d.Date),
			Today:   today,
			Items:   items,
		})
	}
	return out
}

func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

// defaultTodoLimit 是待办的默认上限。这是道保险，不是真限制——
// 未完成的待办上千条本身就说明该清理了，而不是该滑得更久。
// 截断了会在 TodosTotal 里说清楚。
const defaultTodoLimit = 1000

func buildTodos(c *org.Collection, limit int) (items []Item, total int) {
	its := agenda.Todos(c, agenda.TodoFilter{})
	if limit <= 0 {
		limit = defaultTodoLimit
	}
	total = len(its)
	if len(its) > limit {
		its = its[:limit]
	}
	out := make([]Item, 0, len(its))
	for _, it := range its {
		out = append(out, toItem(it))
	}
	return out, total
}

func toItem(it agenda.Item) Item {
	n := it.Node
	out := Item{
		Ref:      fmt.Sprintf("%s:%d", it.Doc.Path, n.Line),
		Kind:     it.Kind.Label(),
		State:    n.TODO,
		Priority: n.Priority,
		Title:    org.Plain(n.Title),
		Tags:     n.Tags,
		Category: it.Category(),
	}
	if it.HasTime {
		out.Time = it.Start.Format("15:04")
		if it.Time != nil && it.Time.HasEnd() {
			out.Time += "-" + it.Time.End.Format("15:04")
		}
	}
	if crumbs := n.Breadcrumb(); len(crumbs) > 1 {
		parts := make([]string, 0, len(crumbs)-1)
		for _, p := range crumbs[:len(crumbs)-1] {
			parts = append(parts, org.Plain(p))
		}
		out.Parents = strings.Join(parts, " / ")
	}
	out.Done, out.Total = n.Progress()
	if r := n.Repeater(); r != nil {
		out.Repeat = r.Describe()
	}
	out.Overdue = it.Kind == agenda.KindOverdue ||
		(it.Kind == agenda.KindDeadline && it.DaysLeft <= 0)

	out.File = it.Doc.Path
	// 正文本身不带，只说有没有——详情走 Detail 单独取。
	out.HasNotes = len(n.Notes()) > 0
	return out
}

// stampText 把时间戳写成人能读的样子。没有就返回空串。
func stampText(ts *org.Timestamp) string {
	if ts == nil {
		return ""
	}
	s := ts.Start.Format("2006-01-02")
	if ts.HasTime {
		s += " " + ts.Start.Format("15:04")
		if ts.HasEnd() {
			s += "-" + ts.End.Format("15:04")
		}
	}
	return s
}

func buildLedger(b *ledger.Book) *LedgerView {
	bal := b.Balances()
	assets := ledger.SumPrefix(bal, "Assets")
	debts := ledger.SumPrefix(bal, "Liabilities")

	v := &LedgerView{Net: assets + debts, Assets: assets, Debts: debts}
	for _, name := range ledger.SortedFrom(bal) {
		if bal[name] == 0 {
			continue // 结平的账户不占地方
		}
		v.Accounts = append(v.Accounts, Account{Name: name, Balance: bal[name]})
	}
	for i := len(b.Transactions) - 1; i >= 0 && i > len(b.Transactions)-21; i-- {
		tx := b.Transactions[i]
		var amt float64
		for _, p := range tx.Postings {
			if p.Amount > amt {
				amt = p.Amount
			}
		}
		v.Recent = append(v.Recent, TxnBrief{
			Date:   tx.Date.Format("2006-01-02"),
			Payee:  tx.Payee,
			Amount: amt,
		})
	}
	return v
}

func weekday(t time.Time) string {
	return []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[int(t.Weekday())]
}

func errorOut(format string, args ...any) string {
	return marshal(&SnapshotOutput{Error: fmt.Sprintf(format, args...)})
}

func (o *SnapshotOutput) normalize() {
	if o.Days == nil {
		o.Days = []Day{}
	}
	for i := range o.Days {
		if o.Days[i].Items == nil {
			o.Days[i].Items = []Item{}
		}
		fillTags(o.Days[i].Items)
	}
	if o.Todos == nil {
		o.Todos = []Item{}
	}
	fillTags(o.Todos)
	if o.Ledger != nil {
		if o.Ledger.Accounts == nil {
			o.Ledger.Accounts = []Account{}
		}
		if o.Ledger.Recent == nil {
			o.Ledger.Recent = []TxnBrief{}
		}
	}
}

func fillTags(items []Item) {
	for i := range items {
		if items[i].Tags == nil {
			items[i].Tags = []string{}
		}
	}
}

// Swift 那边的数组字段是非可选的，Go 的 nil slice 会序列化成 null，
// 解码时直接抛错、整个出参作废。空数组和 null 在这里差别是致命的，
// 所以统一在序列化出口把数组补齐，不指望每个赋值点都记得。
type normalizer interface{ normalize() }

func marshal(v any) string {
	if n, ok := v.(normalizer); ok {
		n.normalize()
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	return string(raw)
}
