package ledger

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 记完账要把「记账前 → 记账后」一起说出来。
//
// 只给新余额的话，模型手上另一个数是系统提示里那份这一轮开始时的旧快照，
// 它会把两者之差——也就是刚记的这笔——当成账目对不上，然后报一个假警。
func TestAddTransactionShowsBeforeAndAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.dat")
	s := NewSkill(path)
	s.Now = func() time.Time { return time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local) }

	add := func(args string) string {
		for _, tl := range s.Tools() {
			if tl.Spec().Name == "add_transaction" {
				out, _, err := tl.Run(args)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
		}
		t.Fatal("没有 add_transaction")
		return ""
	}

	add(`{"payee":"建账","postings":[
		{"account":"Liabilities:Baitiao","amount":-3937.03,"commodity":"CNY"},
		{"account":"Equity:Opening"}]}`)
	out := add(`{"payee":"咖啡豆","postings":[
		{"account":"Expenses:Food","amount":198.80,"commodity":"CNY"},
		{"account":"Liabilities:Baitiao"}]}`)

	for _, want := range []string{"-3937.03 → -4135.83", "-198.80"} {
		if !strings.Contains(out, want) {
			t.Errorf("回话里该有 %q：\n%s", want, out)
		}
	}
	// 自动配平那条不能显示成没动过
	if strings.Contains(out, "Liabilities:Baitiao 余额 -4135.83 → -4135.83") {
		t.Errorf("配平那条的金额丢了：\n%s", out)
	}
}
