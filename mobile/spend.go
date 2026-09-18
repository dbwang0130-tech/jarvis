package mobile

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/ledger"
)

// SpendInput 记一笔账，不经过模型。
//
// 财务页一直是只读的：余额、账户、流水都看得见，可「刚花了 35 块」这件事
// 只能开口跟秘书说一句——一个记账 app 在手机上记不了账。
//
// 跟 Act 一个路子：界面已经知道钱从哪儿出、花在什么上了，让模型再跑一趟
// 是好几秒加一次 API 调用，而它做的无非是拼一笔交易追加到文件末尾。
type SpendInput struct {
	// Ledger 是账本全文。
	Ledger string `json:"ledger"`
	// Date 形如 2006-01-02，留空按今天算。
	Date string `json:"date"`
	// Payee 是这笔钱给了谁 / 因为什么。
	Payee string `json:"payee"`
	// Amount 是金额，正数。方向由 From/To 决定，不靠符号——
	// 界面上让人填负数是自找麻烦。
	Amount float64 `json:"amount"`
	// From 是钱从哪个账户出（Assets:现金），To 是记到哪个科目
	// （Expenses:餐饮）。两边都给全名。
	From string `json:"from"`
	To   string `json:"to"`
	// Commodity 是货币符号，留空按账本里最常见的那个来。
	Commodity string `json:"commodity"`

	TZ string `json:"tz"`
}

// SpendOutput 只在真写进去之后才带 Ledger，跟 Act 一个规矩。
type SpendOutput struct {
	Message string `json:"message"`
	Ledger  string `json:"ledger,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (o *SpendOutput) normalize() {}

// Spend 往账本里追加一笔。
func Spend(inputJSON string) string {
	var in SpendInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&SpendOutput{Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	switch {
	case strings.TrimSpace(in.From) == "":
		return marshal(&SpendOutput{Error: "没说钱从哪儿出"})
	case strings.TrimSpace(in.To) == "":
		return marshal(&SpendOutput{Error: "没说记到哪个科目"})
	case in.Amount <= 0:
		// 方向由 From/To 决定，金额只管大小。负数多半是界面填错了，
		// 照着记会得到一笔反向的账，比报错难查得多。
		return marshal(&SpendOutput{Error: "金额要是正数"})
	}

	when := time.Now()
	if s := strings.TrimSpace(in.Date); s != "" {
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			return marshal(&SpendOutput{Error: fmt.Sprintf("日期 %q 看不懂，要 2006-01-02 这样", s)})
		}
		when = t
	}

	// 跟 Act 一样借临时目录：ledger.Book.Add 是按路径落盘的，而这儿拿到的
	// 是一份文本。改完读回来交出去，iOS 那边负责写回 iCloud。
	wd, err := newWorkdir(nil, "jarvis-spend-")
	if err != nil {
		return marshal(&SpendOutput{Error: err.Error()})
	}
	defer wd.close()

	path := wd.path("ledger.dat")
	if err := wd.write("ledger.dat", in.Ledger); err != nil {
		return marshal(&SpendOutput{Error: err.Error()})
	}
	book, err := ledger.Load(path)
	if err != nil {
		return marshal(&SpendOutput{Error: "账本读不开：" + err.Error()})
	}

	commodity := strings.TrimSpace(in.Commodity)
	if commodity == "" {
		commodity = mainCommodity(book)
	}
	tx := ledger.Transaction{
		Date:   when,
		Status: '*',
		Payee:  strings.TrimSpace(in.Payee),
		Postings: []ledger.Posting{
			{Account: strings.TrimSpace(in.To), Amount: in.Amount,
				HasAmount: true, Commodity: commodity},
			// 第二条不写金额，交给 Book.Add 去配平——金额算两遍迟早对不上。
			{Account: strings.TrimSpace(in.From)},
		},
	}
	if tx.Payee == "" {
		tx.Payee = strings.TrimSpace(in.To)
	}
	if err := book.Add(tx); err != nil {
		return marshal(&SpendOutput{Error: err.Error()})
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return marshal(&SpendOutput{Error: "写完读不回来：" + err.Error()})
	}
	return marshal(&SpendOutput{
		Message: fmt.Sprintf("记下了：%s %.2f", tx.Payee, in.Amount),
		Ledger:  string(raw),
	})
}

// mainCommodity 看账本里已经在用什么货币符号，用得最多的那个。
//
// 不写死 CNY：这份账本是用户自己的，他一直写 ¥ 还是 CNY 还是干脆不写，
// 新记的这笔就该跟着来——混着写的话，ledger-cli 会把它们当成两种货币，
// 余额再也合不到一起。
func mainCommodity(b *ledger.Book) string {
	count := map[string]int{}
	for _, tx := range b.Transactions {
		for _, p := range tx.Postings {
			if p.Commodity != "" {
				count[p.Commodity]++
			}
		}
	}
	best, most := "", 0
	for c, n := range count {
		// 一样多的时候按字典序定下来，免得同一份账本每次记出来不一样。
		if n > most || (n == most && c < best) {
			best, most = c, n
		}
	}
	return best
}
