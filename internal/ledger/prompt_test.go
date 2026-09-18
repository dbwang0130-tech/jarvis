package ledger

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 环境快照里那张表就是模型选账户的依据，**账本里有几个账户就要列几个**。
//
// 漏掉一个的后果不是少看见一行：模型在表里找不到「白条」，就会自己新建一个
// Liabilities:Baitiao，跟已有的 Liabilities:JD:Baitiao 各记一半，
// 余额从此永远对不上，而且从账面上看不出是为什么。
func TestDescribeListsEveryAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.dat")
	s := NewSkill(path)
	s.Now = func() time.Time { return time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local) }
	if err := s.Book.Add(Transaction{
		Date: s.now(), Status: '*', Payee: "建账",
		Postings: []Posting{
			{Account: "Liabilities:JD:Baitiao", Amount: -3937.03, HasAmount: true, Commodity: "CNY"},
			{Account: "Equity:Opening-Balances"},
		}}); err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	s.Describe(&b)
	got := b.String()
	for _, want := range []string{"Liabilities:JD:Baitiao", "Equity:Opening-Balances"} {
		if !strings.Contains(got, want) {
			t.Errorf("账户表里少了 %s：\n%s", want, got)
		}
	}
	// 余额是这一轮开始时的，不说清楚模型会拿它跟工具返回的新余额比，
	// 把差的那一笔（正是它刚记的）当成账目异常报出来。
	if !strings.Contains(got, "这一轮对话开始时") {
		t.Errorf("没说清余额是什么时候的：\n%s", got)
	}
}

// 账户表在 Describe 里，用它的规则在 Instructions 里——两处分开写，
// 很容易改着改着就对不上。这个测试钉住「规则确实指向那张表」。
func TestInstructionsPointAtTheAccountTable(t *testing.T) {
	ins := (&Skill{}).Instructions()
	for _, want := range []string{
		"从上面那张表里挑",          // 指回 Describe 那张账户表
		"新开了",               // 新建账户要说出来，用户才有机会纠正
		"list_transactions", // 报账目异常的前提
	} {
		if !strings.Contains(ins, want) {
			t.Errorf("行为规则里少了 %q", want)
		}
	}
}
