package mobile

import (
	"encoding/json"
	"strings"
	"testing"
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
