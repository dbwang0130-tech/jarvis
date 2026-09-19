package mobile

import (
	"fmt"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/ledger"
)

// 记账最先想知道的就是「这个月花了多少」，而财务页以前只给净资产和余额
// ——那是「还剩多少」，回答不了「花得凶不凶」。
func TestMonthFlow(t *testing.T) {
	book := ledger.ParseBook(`
2026-08-05 * 上月上旬
    Expenses:餐饮      100.00 CNY
    Assets:招行

2026-08-25 * 上月下旬
    Expenses:餐饮      900.00 CNY
    Assets:招行

2026-09-03 * 这月
    Expenses:餐饮      200.00 CNY
    Assets:招行

2026-09-10 * 发工资
    Assets:招行      12000.00 CNY
    Income:工资
`)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.Local)
	got := monthFlow(book, now)

	if got.Expense != 200 {
		t.Errorf("这个月支出 %.2f，该是 200", got.Expense)
	}
	if got.Income != 12000 {
		t.Errorf("这个月收入 %.2f，该是 12000", got.Income)
	}
	// **同期**：到 9 月 18 号，比的是 8 月 1–18 号那 100，不是上月全月的 1000。
	// 跟全月比的话，每个月初都会显得「比上月少花很多」，到月底又追平。
	if got.LastExpense != 100 {
		t.Errorf("上月同期支出 %.2f，该是 100（8 月 25 号那笔不算）", got.LastExpense)
	}
	if got.Days != 18 {
		t.Errorf("这个月过了 %d 天，该是 18", got.Days)
	}
}

// 月末那天要把整个上月都算进来，不能因为上个月短一天就漏掉最后那笔。
func TestMonthFlowMonthEnd(t *testing.T) {
	book := ledger.ParseBook(`
2026-04-30 * 上月最后一天
    Expenses:餐饮      50.00 CNY
    Assets:招行
`)
	// 5 月 31 号：上个月（4 月）只有 30 天，同期应当覆盖整个 4 月。
	got := monthFlow(book, time.Date(2026, 5, 31, 12, 0, 0, 0, time.Local))
	if got.LastExpense != 50 {
		t.Errorf("上月同期 %.2f，该是 50——4 月 30 号那笔不该被漏掉", got.LastExpense)
	}
}

// 空账本不该算出奇怪的数，也不该 panic。
func TestMonthFlowEmpty(t *testing.T) {
	got := monthFlow(ledger.ParseBook(""), time.Now())
	if got.Income != 0 || got.Expense != 0 || got.LastExpense != 0 {
		t.Errorf("空账本算出了 %+v", got)
	}
	if got.Days != time.Now().Day() {
		t.Errorf("天数不对：%d", got.Days)
	}
	fmt.Sprint(got) // 只是确认不炸
}
