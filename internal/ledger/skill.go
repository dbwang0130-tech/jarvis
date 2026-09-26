package ledger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// 下面这几行是**别名，不是第二份实现**——指向 agent 里那一份唯一定义
// （见 agent/schema.go）。原先每个技能包各写一份函数体，而 num 已经在
// ledger/health 和 mail/orgtools 之间漂成了 number 和 integer 两种。
var (
	obj = agent.Obj
	str = agent.Str
	arr = agent.Arr
)

// Skill 把记账能力作为秘书的一项技能接入。
type Skill struct {
	Book *Book
	Now  func() time.Time
}

// NewSkill 构造记账技能。path 为空表示尚未配置账本。
func NewSkill(path string) *Skill {
	b, err := Load(path)
	if err != nil {
		b = &Book{Path: path}
	}
	return &Skill{Book: b}
}

func (s *Skill) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Skill) Name() string { return "记账" }

func (s *Skill) Describe(b *strings.Builder) {
	if s.Book == nil || s.Book.Path == "" {
		b.WriteString("未配置账本文件（输入 /ledger <路径> 设置，格式遵循 ledger-cli）。\n")
		return
	}
	bal := s.Book.Balances()
	fmt.Fprintf(b, "账本文件：%s\n", s.Book.Path)
	fmt.Fprintf(b, "共 %d 笔交易、%d 个账户。\n", len(s.Book.Transactions), len(bal))
	if len(bal) > 0 {
		// 这张表有两个用处，标题里都要说到：
		// ① 它是**账户表**——记账时从这里挑，不然账户会裂成近似的两个；
		// ② 余额是**这一轮开始时**的，记完账就旧了。不说的话模型会拿它
		//    跟工具返回的新余额比，把差的那一笔当成账目异常报出来。
		b.WriteString("这个账本在用的账户，以及**这一轮对话开始时**的余额" +
			"（记账要从这张表里挑账户；记完之后余额就不是这个数了）：\n")
		// 列宽按最长的那个账户名来。写死 24 的话，
		// Liabilities:Mortgage:Principal 这种会把后面的数字顶出去，
		// 整张表就不成列了——模型跟人一样，对不齐的表更容易看串行。
		// 账户名排一次就够——SortedAccounts() 内部是 SortedFrom(Balances())，
		// 调一次就重扫一遍账本，而余额表这儿本来就有。
		names := SortedFrom(bal)
		w := 0
		for _, name := range names {
			if len(name) > w {
				w = len(name)
			}
		}
		for _, name := range names {
			if len(name) == 0 {
				continue
			}
			fmt.Fprintf(b, "  %-*s %12.2f\n", w, name, bal[name])
		}
	}
}

func (s *Skill) Instructions() string {
	return `记账走 ledger-cli 的纯文本格式，账本只追加、不改写。

**账户先从【当下】里那张账户表里挑。** 那是这个账本已经在用的账户。用户说的是生活话——
「白条」「招行」「微信」「打车」——你的活是把它对到已有的那个账户上
（白条 → Liabilities:JD:Baitiao，微信零钱 → Assets:WeChat:Balance）。
这一步做错的后果不是记错一笔，而是**账户裂成两个**：以后「白条」有时进
Liabilities:JD:Baitiao、有时进 Liabilities:Baitiao，两边各记一半，余额从此
永远对不上，而且从账面上看不出是为什么。

实在对不上（第一次出现的开销类型）才新建，并且：
- 沿用表里已有的写法——英文、冒号分层，顶层只有 Expenses / Assets /
  Liabilities / Income / Equity 这五个；层级深浅也跟着相近的那个走。
- 在回话里说一句「新开了 Expenses:Transport:Parking」。用户看见了才有机会
  说「不对，归到 Expenses:Car 里」；闷声建出来的账户没有人会发现。

怎么记：
- 复式记账，每笔至少两条 posting。支出那条记正数进 Expenses:xxx，付款的那条
  （现金、银行卡、白条）**只写账户名、金额留空**，让系统配平成负数。
  收入反过来：Income:xxx 记负数，到账的资产账户留空。
- 货币默认 CNY，写在金额后面。
- 负债账户余额是负数表示欠款，欠得越多越负。这是 ledger 的惯例，不是数据错了。
- 日期留空就是今天；用户说「昨天」「上周三」，自己按【当下】给的今天换算成 2006/01/02。
- 只追加，不改写历史。记错了就再记一笔冲销——账本文件电脑和手机两头都在写，
  去改已经写进去的那笔，会把另一头的改动一起抹掉。

怎么答：
- 记完用一两句话说清记了什么、相关账户的新余额。余额用工具返回的那个数，
  别自己加减。
- **记完再查一遍，确保对得上。** 一次记了不止一笔、或者同一个账户被动了两次，
  末了调一次 list_accounts 把相关账户的余额取回来核一遍，再写进回话。
  多笔叠加最容易出错：第二笔报的「前 → 后」是在第一笔生效之后算的，
  顺着往下加会差一笔。
  **核的基准是 add_transaction 刚返回的那个数**，不是【当下】里那张余额表——
  那张是这一轮开始时的，跟它对不上是正常的（差的正是你刚记的那几笔）。
- 用户只说记一笔，就只记这一笔。别顺带去查别的账户，别主动报「另外我发现」。
- 查余额用 list_accounts，查流水用 list_transactions。

**什么时候别开口说账有问题。** 下面这几种都是正常的：
- 【当下】里那张余额表是这一轮开始时的，你记完账它就旧了。工具返回的新余额跟它差
  一笔，差的正是你刚记的那笔——这是记账生效了。
- 「建账」「期初余额」那笔的金额，跟账户当前余额本来就不同，中间隔着所有流水。
- 两个余额碰巧相等、或者差额恰好等于某笔金额，都只是巧合。

真要说账有问题，前提是你已经用 list_transactions 把那个账户的流水拉出来逐笔
核过，并且能指出是哪一笔缺了或重了。做不到就什么都别说——误报一次，用户白
核对一遍，以后你报什么数他都得自己再算一遍，这项能力就废了。`
}

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		s.toolAddTransaction(),
		s.toolListAccounts(),
		s.toolListTransactions(),
	}
}

type tool struct {
	spec     agent.ToolSpec
	run      func(args string) (string, bool, error)
	readonly bool
}

func (t tool) Spec() agent.ToolSpec                   { return t.spec }
func (t tool) Risk() agent.Risk                       { return agent.RiskNone }
func (t tool) Preview(string) (string, string, error) { return "", "", nil }
func (t tool) Run(args string) (string, bool, error)  { return t.run(args) }
func (t tool) ReadOnly() bool                         { return t.readonly }

func (s *Skill) toolAddTransaction() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "add_transaction",
			Description: "记一笔账。用户说「记一笔」「花了」「收入」「报销」时用。至少两条 posting，付款那条留空让系统配平。",
			Parameters: obj(map[string]any{
				"date":  str("日期，格式 2006/01/02，留空表示今天"),
				"payee": str("摘要/收款方，如「午餐」「发工资」"),
				"postings": arr(obj(map[string]any{
					"account":   str("账户全名，从环境里列出的那张账户表里挑；确实没有才新建"),
					"amount":    agent.Num("金额，正数表示该账户增加。付款那条留空，系统会配平成负数"),
					"commodity": str("货币，默认 CNY；留空沿用同笔其他 posting 的货币"),
				}, "account"), "借贷条目，至少两条"),
			}, "payee", "postings"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Date     string `json:"date"`
				Payee    string `json:"payee"`
				Postings []struct {
					Account   string   `json:"account"`
					Amount    *float64 `json:"amount"`
					Commodity string   `json:"commodity"`
				} `json:"postings"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			if strings.TrimSpace(a.Payee) == "" {
				return "", false, fmt.Errorf("必须提供摘要 payee")
			}
			if len(a.Postings) < 2 {
				return "", false, fmt.Errorf("复式记账至少需要两条 posting")
			}
			date := s.now()
			if strings.TrimSpace(a.Date) != "" {
				d, err := parseDate(strings.TrimSpace(a.Date))
				if err != nil {
					return "", false, err
				}
				date = d
			}
			tx := Transaction{Date: date, Status: '*', Payee: strings.TrimSpace(a.Payee)}
			commodity := ""
			for _, p := range a.Postings {
				acc := strings.TrimSpace(p.Account)
				if acc == "" {
					return "", false, fmt.Errorf("posting 缺少 account")
				}
				post := Posting{Account: acc, Commodity: strings.TrimSpace(p.Commodity)}
				if p.Amount != nil {
					post.Amount = *p.Amount
					post.HasAmount = true
					if post.Commodity != "" {
						commodity = post.Commodity
					}
				}
				tx.Postings = append(tx.Postings, post)
			}
			if err := s.Book.Add(tx); err != nil {
				return "", false, err
			}
			// Add 收的是值，配平出来的那条金额只填在它自己那份上。
			// 拿存进去的那笔来显示，否则自动配平的一条金额是 0。
			tx = s.Book.Transactions[len(s.Book.Transactions)-1]
			if commodity != "" {
				for i := range tx.Postings {
					if tx.Postings[i].Commodity == "" {
						tx.Postings[i].Commodity = commodity
					}
				}
			}
			bal := s.Book.Balances()
			msg := fmt.Sprintf("已记账：%s", s.Book.FormatTransactionLine(tx))
			msg += fmt.Sprintf("\n当前共 %d 笔交易。", len(s.Book.Transactions))
			// **前 → 后一起给**。只给新余额的话，模型手里另一个数是系统提示
			// 里那份旧快照，它会把两者之差（也就是刚记的这笔）当成对不上账。
			for _, p := range tx.Postings {
				now := bal[p.Account]
				msg += fmt.Sprintf("\n  %s 余额 %.2f → %.2f（这笔 %+.2f）",
					p.Account, now-p.Amount, now, p.Amount)
			}
			return msg, false, nil
		},
	}
}

func (s *Skill) toolListAccounts() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "list_accounts",
			Description: "查看账户余额。可按账户前缀筛选，用于回答「还剩多少钱」「这个月花了多少」。",
			Parameters: obj(map[string]any{
				"account": str("可选，账户名或前缀，如 Expenses 或 Assets:Cash"),
			}),
		},
		readonly: true,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Account string `json:"account"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			prefix := strings.TrimSpace(a.Account)
			bal := s.Book.Balances()
			names := s.Book.SortedAccounts()
			var b strings.Builder
			shown := 0
			for _, name := range names {
				if prefix != "" && !strings.HasPrefix(name, prefix) {
					continue
				}
				fmt.Fprintf(&b, "%-24s %.2f\n", name, bal[name])
				shown++
			}
			if shown == 0 {
				return "没有符合条件的账户。\n", false, nil
			}
			return fmt.Sprintf("共 %d 个账户：\n%s", shown, b.String()), false, nil
		},
	}
}

func (s *Skill) toolListTransactions() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "list_transactions",
			Description: "查看最近的交易流水。可按账户筛选，用于回答「我最近买了什么」「上月开销明细」。",
			Parameters: obj(map[string]any{
				"account": str("可选，账户名或前缀，如 Expenses:Food"),
				"limit":   agent.Num("最多返回多少笔，默认 20"),
			}),
		},
		readonly: true,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Account string `json:"account"`
				Limit   int    `json:"limit"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			limit := a.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 200 {
				limit = 200
			}
			prefix := strings.TrimSpace(a.Account)

			txs := append([]Transaction(nil), s.Book.Transactions...)
			sort.SliceStable(txs, func(i, j int) bool {
				return txs[i].Date.After(txs[j].Date)
			})

			var b strings.Builder
			shown := 0
			for _, tx := range txs {
				if prefix != "" && !txTouches(tx, prefix) {
					continue
				}
				b.WriteString(s.Book.FormatTransactionLine(tx) + "\n")
				shown++
				if shown >= limit {
					break
				}
			}
			if shown == 0 {
				return "没有符合条件的交易。\n", false, nil
			}
			return fmt.Sprintf("最近 %d 笔：\n%s", shown, b.String()), false, nil
		},
	}
}

func txTouches(tx Transaction, prefix string) bool {
	for _, p := range tx.Postings {
		if strings.HasPrefix(p.Account, prefix) {
			return true
		}
	}
	return false
}
