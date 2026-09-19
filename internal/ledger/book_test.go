package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAndBalance(t *testing.T) {
	text := `2026/09/14 * 午餐
    Expenses:Food      50.00 CNY
    Assets:Cash

2026/09/15 * 工资
    Assets:Cash      10000.00 CNY
    Income:Salary
`
	b := &Book{Path: "/tmp/x.dat"}
	b.parse(text)

	if len(b.Transactions) != 2 {
		t.Fatalf("交易数 = %d, 想 2", len(b.Transactions))
	}
	bal := b.Balances()
	if bal["Expenses:Food"] != 50 {
		t.Errorf("Expenses:Food = %v, 想 50", bal["Expenses:Food"])
	}
	if bal["Assets:Cash"] != 10000-50 {
		t.Errorf("Assets:Cash = %v, 想 %v", bal["Assets:Cash"], 10000-50)
	}
	if bal["Income:Salary"] != -10000 {
		t.Errorf("Income:Salary = %v, 想 -10000", bal["Income:Salary"])
	}
}

func TestAddAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.dat")
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	tx := Transaction{
		Date:   time.Date(2026, 9, 14, 0, 0, 0, 0, time.Local),
		Status: '*',
		Payee:  "午餐",
		Postings: []Posting{
			{Account: "Expenses:Food", Amount: 50, HasAmount: true, Commodity: "CNY"},
			{Account: "Assets:Cash"},
		},
	}
	if err := b.Add(tx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "Expenses:Food") || !strings.Contains(out, "Assets:Cash") {
		t.Fatalf("账本内容不对:\n%s", out)
	}
	if !strings.Contains(out, "-50.00") {
		t.Fatalf("应有配平后的 -50.00:\n%s", out)
	}
	if got := b.Balances()["Assets:Cash"]; got != -50 {
		t.Errorf("Assets:Cash = %v, 想 -50", got)
	}
}

// 小票路径挂在交易上，写得出去也读得回来。
//
// 走注释而不是另建一份索引：账本是纯文本、是用户自己的文件，图片在哪儿
// 这件事就该跟那笔账待在一起——哪天不用这个 app 了，拿记事本打开也看得见。
func TestTransactionNoteRoundTrip(t *testing.T) {
	b := &Book{}
	tx := Transaction{
		Date:   time.Date(2026, 9, 19, 0, 0, 0, 0, time.Local),
		Status: '*',
		Payee:  "沙县小吃",
		Note:   "小票: receipts/2026-09/2026-09-19-1430-沙县小吃.jpg",
		Postings: []Posting{
			{Account: "Expenses:餐饮", Amount: 35, HasAmount: true, Commodity: "CNY"},
			{Account: "Assets:招行"},
		},
	}
	text := b.FormatTransaction(tx)
	if !strings.Contains(text, "; 小票: receipts/") {
		t.Fatalf("注释没写出去：\n%s", text)
	}

	again := ParseBook(text)
	if len(again.Transactions) != 1 {
		t.Fatalf("解析出 %d 笔：\n%s", len(again.Transactions), text)
	}
	got := again.Transactions[0]
	if got.Note != tx.Note {
		t.Errorf("注释读回来是 %q，该是 %q", got.Note, tx.Note)
	}
	// 注释不能被当成一条分录——那会凭空多出一个叫「; 小票: ...」的账户。
	if len(got.Postings) != 2 {
		t.Errorf("分录有 %d 条，该是 2 条：%+v", len(got.Postings), got.Postings)
	}
	for _, p := range got.Postings {
		if strings.Contains(p.Account, "小票") {
			t.Errorf("注释被当成分录了：%q", p.Account)
		}
	}
	// 余额照算不误。
	if bal := again.Balances()["Expenses:餐饮"]; bal != 35 {
		t.Errorf("餐饮余额 %v，该是 35", bal)
	}
}

// 顶格的注释是文件级的，别挂到交易上。
func TestFileLevelCommentNotAttached(t *testing.T) {
	b := ParseBook(`; 我的账本
2026-09-19 * 午饭
    Expenses:餐饮   35.00 CNY
    Assets:招行
`)
	if len(b.Transactions) != 1 {
		t.Fatalf("解析出 %d 笔", len(b.Transactions))
	}
	if b.Transactions[0].Note != "" {
		t.Errorf("把文件头的注释挂到交易上了：%q", b.Transactions[0].Note)
	}
}
