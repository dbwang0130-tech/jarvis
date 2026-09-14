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
