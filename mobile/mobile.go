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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	// 把时区数据库打进二进制（约 450KB）。gomobile 编出来的 Go 在 iOS 沙箱里
	// 读不到系统的 zoneinfo，不带这个的话 LoadLocation 一律失败。
	"github.com/dabao/jarvis/internal/agenda"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/org"
	_ "time/tzdata"
)

// SnapshotInput 是 Snapshot 的入参。
type SnapshotInput struct {
	// Files 是 org 文件，键是文件名（也用来拼条目引用），值是全文。
	Files map[string]string `json:"files"`
	// Ledger 是账本全文，没有就留空。
	Ledger string `json:"ledger"`
	// Health 是健康记录全文（FHIR NDJSON），没有就留空。
	Health string `json:"health"`
	// Days 是日程往后看几天，零值按 7 天算。
	Days int `json:"days"`
	// TodoLimit 是待办最多给几条，零值按 1000 算。见 buildTodos。
	TodoLimit int `json:"todo_limit"`
	// Now 可以指定「现在」，留空就是真的现在（测试用）。
	Now string `json:"now"`
	// TZ 是设备的时区名（如 Asia/Shanghai）。见 SetTimeZone。
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

var (
	tzMu  sync.Mutex
	tzSet bool
)

// SetTimeZone 设置进程的本地时区。**只有第一次调用生效**，界面应该在
// app 启动时、任何别的调用之前调一次。
//
// gomobile 编出来的 Go 在 iOS 上找不到系统时区库，time.Local 会退成 UTC——
// 「今天」「明天」「这周」全按 UTC 算，东八区下午六点之后整体差一天。
// 桌面端读得到 /etc/localtime，所以只有手机上不对。
//
// **为什么只能设一次**：time.Local 是全局变量。改它的同时别的 goroutine
// 正在 time.Now() 或 os.Stat 里读它，那就是数据竞争（-race 直接抓得到），
// 而数据竞争是未定义行为，不是「顶多算错一次」。手机上下拉刷新和点开详情
// 是真会同时跑的。时区在一次运行里不会变，设一次就够；真跨了时区，
// 重开一次 app 就好。
// SetDataDir 告诉 Go 可以往哪儿写自己的东西（邮件同步进度这类）。
// **必须在任何别的调用之前调一次**，界面在 app 启动时调。
//
// 不设的话，config.Dir() 会算出 ~/.config/jarvis——而 iOS 上 ~ 是容器
// 根目录，**根目录不可写**，只有 Documents/ Library/ tmp/ 能写。结果是
// 同步进度一直存不下来，每次收信都当成第一次，全量重拉。
// 而且它是静默失败：写不进去只是返回个错误，没人看。
func SetDataDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	// 整条链路（同步进度、配置）都从 config.Path() 派生，设这一处就够。
	config.SetDir(dir)
}

// SetTimeZone 设置进程的本地时区。**只有第一次调用生效**，界面应该在
// app 启动时、任何别的调用之前调一次。
//
// gomobile 编出来的 Go 在 iOS 上找不到系统时区库，time.Local 会退成 UTC——
// 「今天」「明天」「这周」全按 UTC 算，东八区下午六点之后整体差一天。
// 桌面端读得到 /etc/localtime，所以只有手机上不对。
//
// **为什么只能设一次**：time.Local 是全局变量。改它的同时别的 goroutine
// 正在 time.Now() 或 os.Stat 里读它，那就是数据竞争（-race 直接抓得到），
// 而数据竞争是未定义行为，不是「顶多算错一次」。手机上下拉刷新和点开详情
// 是真会同时跑的。时区在一次运行里不会变，设一次就够；真跨了时区，
// 重开一次 app 就好。
func SetTimeZone(name string) {
	if name == "" {
		return
	}
	tzMu.Lock()
	defer tzMu.Unlock()
	if tzSet {
		return
	}
	if loc, err := time.LoadLocation(name); err == nil {
		time.Local = loc
		tzSet = true
	}
}

// SnapshotOutput 是界面要显示的东西。
type SnapshotOutput struct {
	Days  []Day  `json:"days"`
	Todos []Item `json:"todos"`
	// TodosTotal 是截断前的总数，界面据此提示还有多少没显示。
	TodosTotal int         `json:"todos_total"`
	Ledger     *LedgerView `json:"ledger,omitempty"`
	Health     *HealthView `json:"health,omitempty"`
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
	// StateLabel 是 State 给人看的说法（「待办」「等回复」）。
	//
	// 翻译放在这边：了结用哪几个关键字、还有哪些自定义状态，是每份 org
	// 文件用 `#+TODO:` 自己定的，界面那头不可能知道。认不出来的原样给回去。
	StateLabel string `json:"state_label"`
	// Closed 表示这条已经了结了——**做完了或者取消了都算**。
	//
	// 这件事必须由 Go 说了算：了结用哪几个关键字是每份 org 文件自己定的
	// （`#+TODO:` 那一行），默认是 DONE 和 CANCELLED。界面那边照着
	// `state == "DONE"` 判过，结果取消掉的事跑到「逾期」里去了。
	Closed   bool   `json:"closed"`
	Category string `json:"category"`

	// Due 是这条归到哪一天（截止优先于计划），空表示没排期。
	// DueIn 是距今几天，负数是已经过去了。
	//
	// 待办列表靠这两项分组。没有的话界面只能摊成一长条，
	// 「今天该干的」和「下个月才到期的」混在一起，等于没排序。
	Due     string `json:"due"`
	DueIn   int    `json:"due_in"`
	DueKind string `json:"due_kind"` // deadline / scheduled / 空

	// Running 表示这条正在计时（org 里有一条没收尾的 CLOCK）。
	//
	// 以前只有 Detail 里给（LogItem.Running），于是列表上完全看不出哪条
	// 在走，界面也就没法给出「停止」——点了开始就只能靠「完成」收尾。
	// 判断在 Go 这边：什么算「没收尾」是 org 的事（CLOCK 行缺 end），
	// 界面不该去猜。
	Running bool `json:"running"`
	// RunningSince 是这次计时起于何时，RFC3339。
	//
	// 单给一个「已计时 25 分钟」的话，界面每过一分钟就得重算一次；
	// 给出发点，界面（尤其是小组件）就能让它自己走，不必回来问。
	RunningSince string `json:"running_since"`

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
	// Log 是 LOGBOOK 里的记录：每一段计时、状态怎么变的、记过什么笔记。
	// 只给总时长的话，「这件事什么时候做的、做了几回」全看不出来。
	Log []LogItem `json:"log"`
	// States 是这份文件认得的 TODO 关键字，顺序就是状态循环的顺序。
	//
	// **必须由 Go 给**：关键字是每份 org 文件用 `#+TODO:` 自己定的，
	// 界面那头不可能知道。写死一份「TODO/NEXT/DONE」的话，用了自定义
	// 状态的人在手机上就只能在别人的状态之间切。
	States []string `json:"states"`
	Error  string   `json:"error,omitempty"`
}

// LogItem 是 LOGBOOK 里的一条，新的排在前面。
type LogItem struct {
	// Kind 是 clock / state / note / other。
	Kind string `json:"kind"`
	When string `json:"when"`
	Text string `json:"text"`
	// Minutes 只有计时那种才有；仍在走的那条是 Running。
	Minutes int  `json:"minutes"`
	Running bool `json:"running"`
}

func (o *DetailOutput) normalize() {
	if o.Boxes == nil {
		o.Boxes = []Checkbox{}
	}
	if o.Log == nil {
		o.Log = []LogItem{}
	}
	if o.States == nil {
		o.States = []string{}
	}
}

// buildLog 把计时和日志并成一条时间线，新的在前。
//
// org 里这两样分开存（CLOCK 行和 - State/- Note 行），但看的人不关心这个
// 区别——他想知道的是「这件事都发生过什么」，按时间排下来就对了。
func buildLog(n *org.Node) []LogItem {
	var out []LogItem
	for _, c := range n.Clocks {
		if c.Start == nil {
			continue
		}
		it := LogItem{
			Kind:    "clock",
			When:    c.Start.Start.Format("2006-01-02 15:04"),
			Minutes: int(c.Duration().Minutes()),
			Running: c.Running(),
		}
		if it.Running {
			it.Text = "还在计时"
		} else if c.End != nil {
			it.Text = "到 " + c.End.Start.Format("15:04")
		}
		out = append(out, it)
	}
	for _, e := range n.Logs {
		it := LogItem{When: stampText(e.Time), Text: e.Text}
		switch e.Kind {
		case org.LogState:
			it.Kind = "state"
			it.Text = fmt.Sprintf("%s → %s", orDash(e.From), orDash(e.To))
		case org.LogNote:
			it.Kind = "note"
			it.Text = noteBody(n, e)
		default:
			it.Kind = "other"
		}
		out = append(out, it)
	}
	// 新的排前面。没时间戳的沉底，免得插在中间乱了顺序。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].When == "" != (out[j].When == "") {
			return out[j].When == ""
		}
		return out[i].When > out[j].When
	})
	return out
}

// noteBody 把一条笔记的正文捞出来。
//
// org 写的笔记是这个样子：
//
//   - Note taken on [2026-09-16 三 18:20] \
//     卡在数据那一段，问了老张才通
//
// 解析器逐行看，只留下了头一行（「Note taken on …」），正文那几行续行
// 全丢了——照原样显示就是一条没有内容的笔记。这里回文档里把续行捞回来。
func noteBody(n *org.Node, e org.LogEntry) string {
	if n.Doc == nil {
		return e.Text
	}
	var body []string
	for i := e.Line + 1; i < n.End && i < len(n.Doc.Lines); i++ {
		t := strings.TrimSpace(n.Doc.Lines[i])
		// 碰到下一条记录、抽屉收尾或空行就停。
		if t == "" || t == ":END:" || strings.HasPrefix(t, "- ") ||
			strings.HasPrefix(t, "CLOCK:") {
			break
		}
		body = append(body, t)
	}
	if len(body) == 0 {
		// 只有头一行的话，把「Note taken on …」这串没信息量的字去掉，
		// 时间已经单独显示了。
		return strings.TrimSpace(strings.TrimSuffix(
			regexpNotePrefix.ReplaceAllString(e.Text, ""), "\\"))
	}
	return strings.Join(body, "\n")
}

var regexpNotePrefix = regexp.MustCompile(`^- Note taken on\s*\[[^\]]*\]\s*`)

func orDash(s string) string {
	if s == "" {
		return "无"
	}
	return s
}

// Detail 取一条条目的详情。ref 形如 work.org:12，用列表里给的那个。
func Detail(inputJSON string) string {
	var in DetailInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&DetailOutput{Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

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
	out.Log = buildLog(n)
	out.States = doc.AllKeywords()
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
	// Kind 是科目大类：asset / debt / income / expense / equity / other。
	// 界面据此分组——把 Income:Salary 跟银行卡并排列在「账户」里，
	// 看着像是有这么个账户，其实那是记账科目，不是钱放在哪儿。
	Kind string `json:"kind"`
}

// accountKind 按 ledger 的顶层科目名分类。这套前缀是 ledger-cli 的约定，
// 判断逻辑只该有一份，所以放在 Go 这边，界面只管照着分组。
func accountKind(name string) string {
	top := name
	if i := strings.Index(name, ":"); i > 0 {
		top = name[:i]
	}
	switch top {
	case "Assets":
		return "asset"
	case "Liabilities":
		return "debt"
	case "Income":
		return "income"
	case "Expenses":
		return "expense"
	case "Equity":
		return "equity"
	default:
		return "other"
	}
}

// TxnBrief 是流水里的一笔。
type TxnBrief struct {
	Date  string `json:"date"`
	Payee string `json:"payee"`
	// Amount 带符号：花出去是负的，收进来是正的。
	// 原来取「最大的那笔分录」，收入支出都是正数，一眼分不出方向。
	Amount float64 `json:"amount"`
	// Kind 是 expense / income / transfer。转账是钱在自己账户间挪，
	// 既不是花也不是挣，混在一起算会把当天的支出撑得很难看。
	Kind string `json:"kind"`
	// Category 是花在哪儿 / 哪来的，取科目的末一段（餐饮、工资）。
	Category string `json:"category"`
}

// briefOf 把一笔交易压成流水里的一行。
//
// 判断方向看它碰了哪些科目：动了 Expenses 就是花钱，动了 Income 就是进账，
// 两样都没动就是在自己账户之间挪。金额取那一类科目的合计，而不是最大的
// 单笔分录——一笔账拆成好几项（午饭 35 + 饮料 8）时，只报 35 是错的。
func briefOf(tx ledger.Transaction) TxnBrief {
	b := TxnBrief{Date: tx.Date.Format("2006-01-02"), Payee: tx.Payee}

	var expense, income, moved float64
	var expAcct, incAcct string
	// 用 Resolved 而不是 Postings：ledger 允许留一行不写金额由其余倒推，
	// 直接读 Postings 的话那一行是 0，收入这类常写成省略的就全看不见了。
	for _, p := range tx.Resolved() {
		switch accountKind(p.Account) {
		case "expense":
			expense += p.Amount
			if p.Amount > 0 && (expAcct == "" || p.Amount > 0) {
				expAcct = p.Account
			}
		case "income":
			income += p.Amount // 收入在账本里记成负数
			if p.Amount < 0 {
				incAcct = p.Account
			}
		}
		if p.Amount > moved {
			moved = p.Amount
		}
	}

	switch {
	case expense > 0:
		b.Kind, b.Amount, b.Category = "expense", -expense, leafOf(expAcct)
	case income < 0:
		b.Kind, b.Amount, b.Category = "income", -income, leafOf(incAcct)
	default:
		b.Kind, b.Amount = "transfer", moved
	}
	return b
}

// leafOf 取科目的末一段：Expenses:Food:午饭 → 午饭。
// 整串太长，手机上一行摆不下，而前缀对人没信息量。
func leafOf(account string) string {
	if i := strings.LastIndex(account, ":"); i >= 0 {
		return account[i+1:]
	}
	return account
}

// Snapshot 把一批 org 文件和账本解析成界面要显示的内容。
// 入参出参都是 JSON 字符串；出了问题会填在出参的 error 里，不会 panic。
func Snapshot(inputJSON string) string {
	var in SnapshotInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return errorOut("入参不是合法 JSON：%v", err)
	}

	SetTimeZone(in.TZ)

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
	// 当天做完的也列出来（org 里叫 log mode）。默认不列是给终端省地方的
	// 老规矩，可手机上一天就那么几条，全划掉的看不见反而像白干了一天。
	// 天然有界：只有看得见的这几天里完成的才会出现。
	opts.ShowDone = true

	out := SnapshotOutput{Days: buildDays(coll, now, days, opts)}
	out.Todos, out.TodosTotal = buildTodos(coll, in.TodoLimit)
	if strings.TrimSpace(in.Ledger) != "" {
		out.Ledger = buildLedger(ledger.ParseBook(in.Ledger))
	}
	if strings.TrimSpace(in.Health) != "" {
		out.Health = buildHealth(in.Health, now)
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
		kinds := make([]agenda.Kind, 0, len(d.Items)) // 跟 items 一一对应
		seen := map[*org.Node]int{}
		for _, it := range d.Items {
			// 同一条目一天只占一行。一件事既排在今天做、又在几天内截止，
			// 投影出来就是「计划」和「截止」两条，标题一模一样——桌面端
			// 有空间摆开（「还有 2 天到期」是额外信息），手机上一屏就那么
			// 几行，同一句话写两遍纯属占地方。
			//
			// 留计划那条：它说的是你什么时候动手。截止那天自己会单独出现，
			// 详情页里两个时间也都在。
			if i, ok := seen[it.Node]; ok {
				if rankKind(it.Kind) < rankKind(kinds[i]) {
					items[i], kinds[i] = toItem(it), it.Kind
				}
				continue
			}
			seen[it.Node] = len(items)
			items = append(items, toItem(it))
			kinds = append(kinds, it.Kind)
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

// rankKind 决定同一条目一天里留哪一条：逾期最要紧不能被盖掉，
// 其次是「什么时候动手」，截止和事件排后面。
func rankKind(k agenda.Kind) int {
	switch k {
	case agenda.KindOverdue:
		return 0
	case agenda.KindScheduled:
		return 1
	case agenda.KindDeadline:
		return 2
	default:
		return 3
	}
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

// doneWindow 是清单里「最近完成」往回看几天。
//
// 完成项全列出来是没用的——攒上几年谁也不会往下翻。但一条都不列，
// 做完的事就凭空消失了，看着像白干。一周正好：这几天干了什么还记得住，
// 再往前就该是归档的事了。
const doneWindow = 7 * 24 * time.Hour

func buildTodos(c *org.Collection, limit int) (items []Item, total int) {
	its := agenda.Todos(c, agenda.TodoFilter{IncludeDone: true})
	// 完成项只留最近这几天的，其余照旧全给。
	cut := time.Now().Add(-doneWindow)
	kept := its[:0]
	for _, it := range its {
		if it.Node.IsDone() && (it.Node.Closed == nil || it.Node.Closed.Start.Before(cut)) {
			continue
		}
		kept = append(kept, it)
	}
	its = kept
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

// stateLabelCN 把 org 的状态关键字翻成人话。
//
// 原样摆在界面上等于把文件格式漏给了用户——他要的是「等回复」，
// 不是「WAITING」。文件里用 `#+TODO:` 自定义的关键字认不出来，
// 就照原样显示，总比空着强。
func stateLabelCN(state string) string {
	switch state {
	case "TODO":
		return "待办"
	case "NEXT":
		return "下一步"
	case "WAITING":
		return "等回复"
	case "HOLD":
		return "搁置"
	case "SOMEDAY":
		return "将来"
	case "DONE":
		return "已完成"
	case "CANCELLED", "CANCELED":
		return "已取消"
	default:
		return state
	}
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
		Closed:   n.IsDone(),
	}
	out.StateLabel = stateLabelCN(n.TODO)
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
	if c := n.RunningClock(); c != nil && c.Start != nil {
		out.Running = true
		out.RunningSince = c.Start.Start.Format(time.RFC3339)
	}
	if r := n.Repeater(); r != nil {
		out.Repeat = r.Describe()
	}
	out.Overdue = it.Kind == agenda.KindOverdue ||
		(it.Kind == agenda.KindDeadline && it.DaysLeft <= 0)

	if !it.At.IsZero() {
		out.Due = it.At.Format("2006-01-02")
		out.DueIn = it.DaysLeft
	}
	switch {
	case n.Deadline != nil:
		out.DueKind = "deadline"
	case n.Scheduled != nil:
		out.DueKind = "scheduled"
	}

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
		v.Accounts = append(v.Accounts, Account{
			Name: name, Balance: bal[name], Kind: accountKind(name)})
	}
	// 给多一点，界面按天分组之后二十条也就三四天。
	for i := len(b.Transactions) - 1; i >= 0 && i > len(b.Transactions)-61; i-- {
		v.Recent = append(v.Recent, briefOf(b.Transactions[i]))
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
	// 往下钻一层。Swift 那头数组字段是非可选的，解到 null 整个出参作废。
	if o.Health != nil {
		o.Health.normalize()
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
