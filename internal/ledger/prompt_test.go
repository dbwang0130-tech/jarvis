package ledger

import (
	"fmt"
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
//
// **指路要用名字，不能用方向。** 这条规则原先写的是「从上面那张表里挑」，
// 而为了服务端缓存，环境快照后来整段挪到了系统提示的最末尾（见
// agent.systemPrompt）——那张表从此在下面，「上面」当场成了错话。现在统一说
// 「【当下】里那张账户表」，再调顺序也不会说错。
func TestInstructionsPointAtTheAccountTable(t *testing.T) {
	ins := (&Skill{}).Instructions()
	for _, want := range []string{
		"【当下】里那张账户表",        // 指回 Describe 那张账户表
		"新开了",               // 新建账户要说出来，用户才有机会纠正
		"list_transactions", // 报账目异常的前提
		"这次会话里动过的账户",        // 多笔时报余额用哪一份
		"list_accounts",     // 核的手段
	} {
		if !strings.Contains(ins, want) {
			t.Errorf("行为规则里少了 %q", want)
		}
	}
	// **这类字面断言只能证明「句子在」，证明不了「模型照做」。**
	//
	// 「多笔记完核一遍」这条第一次写进去时就是：提示里有、这个测试绿的，而模型
	// 根本没调 list_accounts——因为紧接着的下一条写着「别顺带去查别的账户」，
	// 两句话互相打架，它按后一条办了。**发现它的是 make eval**（真模型跑一遍、
	// 断言实际调了哪些工具），不是这儿。所以这两层都要有。
	//
	// 快照挪到末尾之后，用方向指路一律是错的。这条**连同别的技能一起**
	// 由 agent 那边的 TestPromptPutsVolatilePartsLast 兜着。
	if strings.Contains(ins, "上面那张") {
		t.Error("还在用「上面那张」指路——环境快照现在在系统提示的最后")
	}
}

// 一次记好几笔，第二笔起的返回要带上**这次会话里动过的全部账户**的当前余额。
//
// 这是「报出来的余额对得上」那件事的真正保证。原先它是一条提示规则（「最后一步
// 调 list_accounts 核一遍」），而真模型不照做——`make eval` 那条场景连着两次都
// 红。它判断得对：add_transaction 的返回里已经有权威余额，再查一遍是同一个数，
// 而提示里另有「答案短就短」。逼模型做空动作是错的方向，所以把保证搬进工具：
// 递给它的数据本身完整，引错数的机会就没有了。
//
// 钉的是**同一个账户被动两次**那种情况——那才是前一笔的「后」会变旧的时候。
func TestSecondTransactionSummarizesTouchedAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.dat")
	s := NewSkill(path)
	s.Now = func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.Local) }

	add := func(payee, expense string, amount float64) string {
		t.Helper()
		args := fmt.Sprintf(`{"payee":%q,"postings":[
			{"account":%q,"amount":%v,"commodity":"CNY"},
			{"account":"Assets:WeChat:Balance"}]}`, payee, expense, amount)
		var out string
		for _, tl := range s.Tools() {
			if tl.Spec().Name != "add_transaction" {
				continue
			}
			got, err := tl.Run(strings.ReplaceAll(args, "\n", ""))
			if err != nil {
				t.Fatalf("记账失败：%v", err)
			}
			out = got
		}
		return out
	}

	// 第一笔：只给逐笔那几行，不给汇总——那会儿逐笔本身就是全部。
	first := add("早餐", "Expenses:Food", 12)
	if strings.Contains(first, "动过的账户") {
		t.Errorf("只记一笔就给汇总，白占上下文：\n%s", first)
	}

	// 第二笔动的是同一个付款账户，它的余额该是 -30，而不是第一笔报的 -12。
	second := add("打车", "Expenses:Transport", 18)
	if !strings.Contains(second, "这次会话里动过的账户") {
		t.Fatalf("第二笔没给汇总：\n%s", second)
	}
	for _, want := range []string{
		"Assets:WeChat:Balance -30.00", // 两笔叠加之后的真值
		"Expenses:Food 12.00",          // 第一笔那个账户也要在场
		"Expenses:Transport 18.00",
	} {
		if !strings.Contains(strings.Join(strings.Fields(second), " "), want) {
			t.Errorf("汇总里少了 %q：\n%s", want, second)
		}
	}
}
