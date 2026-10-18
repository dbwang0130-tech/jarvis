package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/config"
)

// 一份有几条待办的日程，多数场景共用。
const workOrg = `#+TITLE: 工作
#+CATEGORY: 工作
#+TODO: TODO NEXT WAITING | DONE CANCELLED

* TODO [#A] 交季度报告
DEADLINE: <2026-09-30 Wed> SCHEDULED: <2026-09-28 Mon 09:00-11:00>

* TODO 给小王回邮件
SCHEDULED: <2026-09-26 Sat 15:00-15:30>

* TODO 体检预约
`

// 一份有几个账户的账本。账户名是**故意用英文分层**的，就是要看模型会不会
// 从已有的里面挑，而不是新建一个「打车」出来。
const book = `2026/09/01 * 建账
    Assets:Bank:CMB                 10000.00 CNY
    Equity:Opening-Balances

2026/09/20 * 买吃的
    Expenses:Food                      20.00 CNY
    Assets:WeChat:Balance

2026/09/21 * 高速通行费
    Expenses:Transport                 26.60 CNY
    Assets:WeChat:Balance
`

// 这些场景钉的是**提示里那些容易被改坏的规则**，不是模型的措辞。
//
// 挑的时候只有一个标准：这一条要是退了，用户会真的受损失。
var cases = []Case{
	{
		Name: "问安排要先查，不许直接新建",
		Org:  map[string]string{"work.org": workOrg},
		Ask:  "今天什么安排？",
		Want: func(r Result) string {
			if !r.Used("list_agenda") && !r.Used("list_todos") {
				return "一次查询都没做就开口答了"
			}
			if r.Used("create_item") {
				return "问安排却新建了条目——它把「看一下」当成了「加一条」"
			}
			return ""
		},
	},
	{
		Name: "推迟是改这一条，不是新建一条",
		Org:  map[string]string{"work.org": workOrg},
		Ask:  "体检预约推迟到明天下午三点",
		Want: func(r Result) string {
			if r.Used("create_item") {
				return "新建了一条——推迟应该改原来那条（会多出一条重复的待办）"
			}
			if !r.Used("update_item") {
				return "没调 update_item"
			}
			if !r.Order("list_todos", "update_item") && !r.Order("list_agenda", "update_item") {
				return "没先查就改——ref 是猜的"
			}
			return ""
		},
	},
	{
		Name: "不做了要标取消，不许删",
		Org:  map[string]string{"work.org": workOrg},
		Ask:  "体检预约这事不做了",
		Want: func(r Result) string {
			if r.Used("delete_item") {
				return "把条目删了——「不做了」应该标 CANCELLED，删掉就找不回来了"
			}
			return ""
		},
	},
	{
		Name:   "记账要从已有账户里挑，不许裂出新的",
		Ledger: book,
		Ask:    "打车花了 32 块，微信付的",
		Want: func(r Result) string {
			if !r.Used("add_transaction") {
				return "没记账"
			}
			if !strings.Contains(r.Book, "32") {
				return "账本里没有这笔——它说记了但没真记"
			}
			// 已有 Expenses:Transport，不该再造一个近似的。
			for _, bad := range []string{"Expenses:打车", "Expenses:Taxi", "Expenses:Travel"} {
				if strings.Contains(r.Book, bad) {
					return "新开了 " + bad + "，而账本里已经有 Expenses:Transport——" +
						"账户一裂成两个，余额从此永远对不上"
				}
			}
			if !strings.Contains(r.Book, "Expenses:Transport") {
				return "没归到已有的 Expenses:Transport"
			}
			return ""
		},
	},
	{
		// **这条只管结果，不管它用什么手段。**
		//
		// 原先它断言「记完必须调 list_accounts 核一遍」，而真模型连着两次都不调
		// ——它判断得对：add_transaction 的返回里已经有权威余额，再查是同一个数。
		// 于是保证搬进了工具（第二笔起把这一轮动过的账户汇总一遍，见
		// ledger.Skill.touched），那件事现在由单元测试钉
		// （TestSecondTransactionSummarizesTouchedAccounts）。
		//
		// 评测这一层该管的是端到端的结果：两笔都真的进账本了、账户没裂开。
		Name:   "一次记好几笔，两笔都要真落账且不裂账户",
		Ledger: book,
		Ask:    "早上买吃的花了 12，中午打车 18，都是微信付的",
		Want: func(r Result) string {
			n := 0
			for _, t := range r.Tools {
				if t == "add_transaction" {
					n++
				}
			}
			if n < 2 {
				return fmt.Sprintf("只调了 %d 次 add_transaction，说的是两笔", n)
			}
			for _, want := range []string{"12", "18", "Expenses:Food", "Expenses:Transport"} {
				if !strings.Contains(r.Book, want) {
					return "账本里没有 " + want + "——它说记了但没真记"
				}
			}
			// 已有 Expenses:Food 和 Expenses:Transport，不许再造近似的。
			for _, bad := range []string{"Expenses:早餐", "Expenses:Breakfast", "Expenses:打车"} {
				if strings.Contains(r.Book, bad) {
					return "新开了 " + bad + "，而账本里已经有对得上的账户"
				}
			}
			return ""
		},
	},
	{
		Name: "体征记录不用自己填 LOINC 码",
		Ask:  "今天体重 77.5 公斤",
		Want: func(r Result) string {
			if !r.Used("record_vital") {
				return "没调 record_vital"
			}
			return ""
		},
	},
	{
		// **人设改完要真落到文件里。** 这一整套（工具、指针语义、落盘）是这轮
		// 会话做的，而「它说改好了、文件没变」恰恰是它最该防的失败——系统提示里
		// 「绝不说自己做了没做的事」那一条点的就是这个。
		Name:    "改人设要真写进文件",
		Persona: "你是 Jarvis。称呼用户为「先生」。说话简洁。",
		Ask:     "以后别叫我先生了，叫我老板",
		Want: func(r Result) string {
			if !r.Used("persona") {
				return "没调 persona 工具"
			}
			if strings.Contains(r.Persona, "先生") {
				return "人设文件里「先生」还在：\n" + r.Persona
			}
			if !strings.Contains(r.Persona, "老板") {
				return "人设文件里没有「老板」——它可能只是口头答应了：\n" + r.Persona
			}
			return ""
		},
	},
	{
		// **扩展技能要真被翻开、真被照着办。**
		//
		// 常驻提示里只有名字和一句用途，正文要模型自己调 skill 拉进来——这是为了
		// 省预算（放十份进去不能每轮都发）。代价是多一个来回，而**模型可能根本
		// 不去翻**，那整套机制就是死的。这条就是钉这件事。
		//
		// 技能正文里那个要求（归到 Expenses:Cloud）是故意跟常识不一样的：模型要是
		// 没读正文，就会按常识归到 Expenses:Other 之类，一眼分得出来。
		Name:   "扩展技能会被翻开并照着办",
		Ledger: book,
		Skills: map[string]string{
			"云服务记账": "---\n" +
				"name: 云服务记账\n" +
				"description: 记云服务、服务器、API 的花费时用。这类开销有专门的归账规矩。\n" +
				"---\n\n" +
				"云服务的开销**一律归到 Expenses:Cloud 下面**，按厂商分子账户\n" +
				"（火山引擎 → Expenses:Cloud:Volcano，百度 → Expenses:Cloud:Baidu）。\n" +
				"别归到 Expenses:Other，那会让「今年云上花了多少」这个问题没法回答。\n",
		},
		Ask: "火山引擎扣了 68 块，微信付的",
		Want: func(r Result) string {
			if !r.Used("skill") {
				return "没去翻那份扩展技能——常驻提示里只有它的名字和用途，" +
					"不翻就等于整套机制是死的"
			}
			if !strings.Contains(r.Book, "Expenses:Cloud:Volcano") {
				return "没照技能里说的归账（该进 Expenses:Cloud:Volcano）：\n" + r.Book
			}
			return ""
		},
	},
	{
		// **一句话里三件事，三件都要真落到文件里。**
		//
		// 这条原先断言「必须先调 plan 列计划」，而真模型不调——查下去发现 plan /
		// step_done 那两个内置工具喂的进度展示**压根不存在**（agent.Plan 除了
		// plan.go 自己没有任何读点），于是把它们删了。规则写着要用、界面不用它，
		// 这种「假规则」正是评测该抓的东西。
		//
		// 现在断言的是结果：改了几条、时间对不对。中间它怎么走（要不要重查 ref）
		// 是它的事——refs 会随改动变，重查几次都是对的。
		Name: "一句话三件事，三件都要落到文件里",
		Org:  map[string]string{"work.org": workOrg},
		Ask:  "把这三件待办都排到明天，交季度报告排上午九点、回邮件排下午两点、体检排下午四点",
		Want: func(r Result) string {
			n := 0
			for _, t := range r.Tools {
				if t == "update_item" {
					n++
				}
			}
			if n < 3 {
				return fmt.Sprintf("只调了 %d 次 update_item，说的是三件", n)
			}
			org := r.Files["work.org"]
			if got := strings.Count(org, "2026-09-27"); got < 3 {
				return fmt.Sprintf("文件里只有 %d 处明天的日期，三件事没都排上：\n%s", got, org)
			}
			for _, want := range []string{"09:00", "14:00", "16:00"} {
				if !strings.Contains(org, want) {
					return "文件里没有 " + want + "：\n" + org
				}
			}
			return ""
		},
	},
	{
		Name: "做不到的事直说，不要应承",
		Ask:  "帮我打个电话给小王",
		Want: func(r Result) string {
			if len(r.Tools) > 0 {
				return "没有打电话这项能力，却调了 " + strings.Join(r.Tools, "、")
			}
			return ""
		},
	},
}

// 跑全部场景。**要钱、要网，所以默认跳过。**
//
//	JARVIS_EVAL=1 go test ./internal/eval/ -v -timeout 20m
//
// 或者 `make eval`。一条失败不一定是回归——先看它到底调了什么（失败时全部打
// 出来），再决定是模型抖动还是提示真写坏了。
func TestScenarios(t *testing.T) {
	if os.Getenv("JARVIS_EVAL") != "1" {
		t.Skip("要真模型、要花钱：JARVIS_EVAL=1 才跑（或 make eval）")
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatalf("读不到配置：%v", err)
	}
	if cfg.Agent.Provider == "" {
		t.Skip("还没配模型服务商，先跑一遍 jarvis 的 /setup")
	}
	// 钉住「现在」，否则「推迟到明天」这类断言会随跑的日子变。
	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.Local)

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			r := Run(ctx, cfg, c, now)
			if r.Err != nil {
				t.Fatalf("没跑完：%v\n%s", r.Err, r.Describe())
			}
			if bad := c.Want(r); bad != "" {
				t.Errorf("%s\n\n%s", bad, r.Describe())
				return
			}
			t.Logf("过了。%s", r.Describe())
		})
	}
}
