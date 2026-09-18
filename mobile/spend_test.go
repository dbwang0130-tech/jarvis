package mobile

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/ledger"
)

const spendBook = `2026-09-01 * 开户
    Assets:招行      10000.00 CNY
    Equity:期初

2026-09-10 * 午饭
    Expenses:餐饮       35.00 CNY
    Assets:招行
`

func spend(t *testing.T, in SpendInput) SpendOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out SpendOutput
	if err := json.Unmarshal([]byte(Spend(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

// 财务页一直是只读的：余额、流水都看得见，可「刚花了 35 块」只能开口说。
func TestSpendAppends(t *testing.T) {
	out := spend(t, SpendInput{
		Ledger: spendBook, Date: "2026-09-18", Payee: "咖啡",
		Amount: 28, From: "Assets:招行", To: "Expenses:餐饮",
		TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("记不上：%s", out.Error)
	}
	if !strings.Contains(out.Ledger, "咖啡") {
		t.Fatalf("没写进去：\n%s", out.Ledger)
	}
	// 原有的内容一行都不能少——追加不是重写。
	if !strings.Contains(out.Ledger, "2026-09-01 * 开户") ||
		!strings.Contains(out.Ledger, "2026-09-10 * 午饭") {
		t.Fatalf("把原来的账弄丢了：\n%s", out.Ledger)
	}
	// 货币符号跟着账本里已有的来，不写死。混着写的话 ledger-cli 会把它们
	// 当成两种货币，余额再也合不到一起。
	if !strings.Contains(out.Ledger, "28.00 CNY") {
		t.Fatalf("货币符号没跟上：\n%s", out.Ledger)
	}
	// 第二条 posting 由 Book.Add 配平，不该自己算一遍。
	if !strings.Contains(out.Ledger, "Assets:招行") {
		t.Fatalf("对方账户没写上：\n%s", out.Ledger)
	}
}

// 方向由 From/To 决定，金额只管大小。负数多半是界面填错了，照着记会得到
// 一笔反向的账，比报错难查得多。
func TestSpendRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   SpendInput
		want string
	}{
		{"没说从哪儿出", SpendInput{To: "Expenses:餐饮", Amount: 10}, "从哪儿出"},
		{"没说记到哪儿", SpendInput{From: "Assets:招行", Amount: 10}, "哪个科目"},
		{"金额是负的", SpendInput{From: "Assets:招行", To: "Expenses:餐饮", Amount: -10}, "正数"},
		{"金额是零", SpendInput{From: "Assets:招行", To: "Expenses:餐饮"}, "正数"},
		{"日期看不懂", SpendInput{From: "Assets:招行", To: "Expenses:餐饮",
			Amount: 10, Date: "九月十八"}, "看不懂"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := spend(t, tc.in)
			if out.Error == "" {
				t.Fatal("该报错")
			}
			if !strings.Contains(out.Error, tc.want) {
				t.Fatalf("话没说到点上：%s", out.Error)
			}
			if out.Ledger != "" {
				t.Error("没记成却把账本交回去了")
			}
		})
	}

	// 烂入参不许 panic——手机上崩一次就是整个 app 挂掉。
	var out SpendOutput
	if err := json.Unmarshal([]byte(Spend("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}
}

// 空账本也得能记第一笔——第一次用的时候账本就是空的。
func TestSpendOnEmptyBook(t *testing.T) {
	out := spend(t, SpendInput{
		Ledger: "", Payee: "第一笔", Amount: 5,
		From: "Assets:现金", To: "Expenses:杂项", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("空账本记不上：%s", out.Error)
	}
	if !strings.Contains(out.Ledger, "第一笔") {
		t.Fatalf("没写进去：\n%s", out.Ledger)
	}
}

func reverse(t *testing.T, in ReverseInput) SpendOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out SpendOutput
	if err := json.Unmarshal([]byte(Reverse(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

// 记错了不是把那几行删掉，而是再记一笔方向相反的。
//
// 账本是一条流水，每一笔都是「当时确实这么记过」的事实：回头改历史的账
// 没法对，也说不清当时到底发生过什么。冲账之后两笔都留着，一抵，余额就
// 回去了——这是 ledger 的规矩。
func TestReverse(t *testing.T) {
	out := reverse(t, ReverseInput{
		Ledger: spendBook, Index: 1, Date: "2026-09-10", Payee: "午饭",
		TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("冲不了：%s", out.Error)
	}
	// **原来那笔必须还在。**
	if !strings.Contains(out.Ledger, "2026-09-10 * 午饭") {
		t.Fatalf("把原来那笔改掉了：\n%s", out.Ledger)
	}
	if !strings.Contains(out.Ledger, "冲账：午饭（2026-09-10）") {
		t.Fatalf("没记上冲账：\n%s", out.Ledger)
	}

	// 两笔一抵，账户余额得回到冲账之前。
	before := ledgerBalance(t, spendBook, "Expenses:餐饮")
	after := ledgerBalance(t, out.Ledger, "Expenses:餐饮")
	if after != before-35 {
		t.Fatalf("冲完餐饮还剩 %.2f，原来 %.2f，该少 35", after, before)
	}
	if b := ledgerBalance(t, out.Ledger, "Assets:招行"); b != 10000 {
		t.Fatalf("冲完招行余额 %.2f，该回到 10000", b)
	}
}

// 冲账的冲账 = 重复记账。两笔反向的抵完，再来一笔就把钱又花出去了。
func TestReverseTwiceRefused(t *testing.T) {
	out := reverse(t, ReverseInput{
		Ledger: spendBook, Index: 1, Date: "2026-09-10", Payee: "午饭",
		TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatal(out.Error)
	}
	book := ledgerParsed(t, out.Ledger)
	last := len(book) - 1
	again := reverse(t, ReverseInput{
		Ledger: out.Ledger, Index: last,
		Date: book[last].date, Payee: book[last].payee, TZ: "Asia/Shanghai",
	})
	if again.Error == "" {
		t.Fatal("冲账的冲账该被拦住")
	}
	if !strings.Contains(again.Error, "本来就是一笔冲账") {
		t.Fatalf("话没说到点上：%s", again.Error)
	}
}

// 光有下标不够：账本在别处被改过之后，同一个下标指的可能已经是另一笔了。
// 冲错一笔账，人要过很久才发现。
func TestReverseChecksIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   ReverseInput
		want string
	}{
		{"日期对不上", ReverseInput{Ledger: spendBook, Index: 1,
			Date: "2026-01-01", Payee: "午饭"}, "对不上"},
		{"收款方对不上", ReverseInput{Ledger: spendBook, Index: 1,
			Date: "2026-09-10", Payee: "晚饭"}, "对不上"},
		{"下标越界", ReverseInput{Ledger: spendBook, Index: 99,
			Date: "2026-09-10", Payee: "午饭"}, "找不到"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.TZ = "Asia/Shanghai"
			out := reverse(t, tc.in)
			if out.Error == "" {
				t.Fatal("该拒绝")
			}
			if !strings.Contains(out.Error, tc.want) {
				t.Fatalf("话没说到点上：%s", out.Error)
			}
			if out.Ledger != "" {
				t.Error("拒绝了却把账本交回去了")
			}
		})
	}

	var out SpendOutput
	if err := json.Unmarshal([]byte(Reverse("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}
}

// 下面两个是读账本的小帮手，只在这个文件里用。

func ledgerBalance(t *testing.T, text, account string) float64 {
	t.Helper()
	var sum float64
	for _, tx := range ledger.ParseBook(text).Transactions {
		for _, p := range tx.Resolved() {
			if p.Account == account {
				sum += p.Amount
			}
		}
	}
	return sum
}

type txLine struct{ date, payee string }

func ledgerParsed(t *testing.T, text string) []txLine {
	t.Helper()
	var out []txLine
	for _, tx := range ledger.ParseBook(text).Transactions {
		out = append(out, txLine{tx.Date.Format("2006-01-02"), tx.Payee})
	}
	return out
}
