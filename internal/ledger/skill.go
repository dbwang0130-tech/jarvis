package ledger

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
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
		b.WriteString("主要账户余额：\n")
		for _, name := range s.Book.SortedAccounts() {
			if len(name) == 0 {
				continue
			}
			fmt.Fprintf(b, "  %-24s %.2f\n", name, bal[name])
		}
	}
}

func (s *Skill) Instructions() string {
	return `记账（ledger-cli 纯文本格式）：
1. 用户说「记一笔」「花了」「收入」「报销」等时，用 add_transaction 记账。
2. 复式记账：每笔至少两条 posting，账户用冒号分层（如 Expenses:Food、Assets:Cash、Liabilities:CreditCard）。
3. 金额写阿拉伯数字，货币写在数字后面（如 50.00 CNY）。支出一条记正数（如 Expenses:Food 50.00），对应的资产/负债条可以留空让系统自动配平。
4. 查询余额、收支用 list_accounts；看流水用 list_transactions。
5. 只追加新交易，不要改写历史记录。
6. 记完账用一两句话说清楚记了什么、当前相关账户余额。`
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

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }
func arr(items map[string]any, desc string) map[string]any {
	return map[string]any{"type": "array", "items": items, "description": desc}
}

func (s *Skill) toolAddTransaction() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "add_transaction",
			Description: "记一笔账。用户说「记一笔」「花了」「收入」时用。至少两条 posting，金额可以留一条空让系统配平。",
			Parameters: obj(map[string]any{
				"date":  str("日期，格式 2006/01/02，留空表示今天"),
				"payee": str("摘要/收款方，如「午餐」「发工资」"),
				"postings": arr(obj(map[string]any{
					"account":   str("账户，冒号分层，如 Expenses:Food"),
					"amount":    num("金额，正数表示该账户增加；可留空让系统配平"),
					"commodity": str("货币，如 CNY、USD；留空沿用同笔其他 posting 的货币"),
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
			for _, p := range tx.Postings {
				msg += fmt.Sprintf("\n  %s 余额 %.2f", p.Account, bal[p.Account])
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
				"limit":   num("最多返回多少笔，默认 20"),
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
