// Package ledger 实现 ledger-cli 风格的纯文本记账：解析、余额计算与追加交易。
//
// 格式遵循 https://ledger-cli.org 的约定：
//
//	2026/09/14 * 午餐
//	    Expenses:Food      50.00 CNY
//	    Assets:Cash
package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Posting 是一笔交易里的一条借贷记录。
type Posting struct {
	Account   string
	Amount    float64
	HasAmount bool
	Commodity string
}

// Transaction 是一笔交易。
type Transaction struct {
	Date     time.Time
	Status   byte // ' '、'*' 或 '!'
	Payee    string
	Postings []Posting
}

// Book 是一个账本文件的内存表示。
type Book struct {
	Path         string
	Transactions []Transaction
}

var (
	dateRe   = regexp.MustCompile(`^(\d{4}[/-]\d{1,2}[/-]\d{1,2})\b`)
	numberRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	commodRe = regexp.MustCompile(`^[A-Za-z$€£¥][A-Za-z0-9_.$€£¥]*$`)
)

// Load 读取账本；文件不存在时自动创建，返回空账本。
func Load(path string) (*Book, error) {
	b := &Book{Path: path}
	if path == "" {
		return b, nil
	}
	if err := ensureFile(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取账本 %s 失败: %w", path, err)
	}
	b.parse(string(data))
	return b, nil
}

// ensureFile 确保账本文件存在，必要时连同父目录一起创建。
func ensureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建账本目录失败: %w", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("创建账本文件失败: %w", err)
	}
	return nil
}

func isComment(s string) bool {
	for _, c := range []string{";", "#", "%", "|", "*"} {
		if strings.HasPrefix(s, c) {
			return true
		}
	}
	return false
}

func (b *Book) parse(text string) {
	var cur *Transaction
	flush := func() {
		if cur != nil {
			b.Transactions = append(b.Transactions, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isComment(trimmed) {
			continue
		}
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if !indented && dateRe.MatchString(line) {
			flush()
			tx := parseTransaction(line)
			cur = &tx
			continue
		}
		if indented && cur != nil {
			cur.Postings = append(cur.Postings, parsePosting(trimmed))
		}
	}
	flush()
}

func parseTransaction(line string) Transaction {
	fields := strings.Fields(line)
	tx := Transaction{Status: ' '}
	if len(fields) > 0 {
		if d, err := parseDate(fields[0]); err == nil {
			tx.Date = d
		}
	}
	rest := fields[1:]
	if len(rest) > 0 && (rest[0] == "*" || rest[0] == "!") {
		tx.Status = rest[0][0]
		rest = rest[1:]
	}
	tx.Payee = strings.Join(rest, " ")
	return tx
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006/01/02", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("日期格式不对: %s", s)
}

func parsePosting(s string) Posting {
	fields := strings.Fields(s)
	p := Posting{}
	if len(fields) == 0 {
		return p
	}
	n := len(fields)
	last := fields[n-1]
	switch {
	case numberRe.MatchString(last):
		p.Amount, _ = strconv.ParseFloat(last, 64)
		p.HasAmount = true
		if n >= 2 && commodRe.MatchString(fields[n-2]) {
			p.Commodity = fields[n-2]
			p.Account = strings.Join(fields[:n-2], " ")
		} else {
			p.Account = strings.Join(fields[:n-1], " ")
		}
	case n >= 2 && numberRe.MatchString(fields[n-2]):
		p.Amount, _ = strconv.ParseFloat(fields[n-2], 64)
		p.HasAmount = true
		p.Commodity = last
		p.Account = strings.Join(fields[:n-2], " ")
	default:
		p.Account = s
	}
	return p
}

// Balances 汇总各账户余额（带符号：支出为正、资产相应为负）。
func (b *Book) Balances() map[string]float64 {
	out := map[string]float64{}
	for _, tx := range b.Transactions {
		var net float64
		nullIdx := -1
		for i, p := range tx.Postings {
			if p.HasAmount {
				net += p.Amount
			} else if nullIdx == -1 {
				nullIdx = i
			}
		}
		for i, p := range tx.Postings {
			amt := p.Amount
			if !p.HasAmount {
				if i == nullIdx {
					amt = -net
				} else {
					amt = 0
				}
			}
			out[p.Account] += amt
		}
	}
	return out
}

// SortedAccounts 返回按字母序排列的账户名。
func (b *Book) SortedAccounts() []string {
	bal := b.Balances()
	names := make([]string, 0, len(bal))
	for k := range bal {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// SumByPrefix 汇总指定前缀账户的余额。
func (b *Book) SumByPrefix(prefix string) float64 {
	var sum float64
	for k, v := range b.Balances() {
		if strings.HasPrefix(k, prefix) {
			sum += v
		}
	}
	return sum
}

// IncomeExpense 返回自 since 以来的收入与支出（正数）。
func (b *Book) IncomeExpense(since time.Time) (income, expense float64) {
	for _, tx := range b.Transactions {
		if tx.Date.Before(since) {
			continue
		}
		for _, p := range b.effective(tx) {
			switch {
			case strings.HasPrefix(p.Account, "Income:"):
				income += -p.Amount
			case strings.HasPrefix(p.Account, "Expenses:"):
				expense += p.Amount
			}
		}
	}
	return income, expense
}

// effective 返回把未指定金额的 posting 配平后的副本。
func (b *Book) effective(tx Transaction) []Posting {
	out := make([]Posting, len(tx.Postings))
	copy(out, tx.Postings)
	var net float64
	nullIdx := -1
	for i, p := range out {
		if p.HasAmount {
			net += p.Amount
		} else if nullIdx == -1 {
			nullIdx = i
		}
	}
	if nullIdx >= 0 {
		out[nullIdx].Amount = -net
		out[nullIdx].HasAmount = true
	}
	return out
}

// Add 追加一笔交易：先给没有金额的 posting 配平，再落盘。
func (b *Book) Add(tx Transaction) error {
	if b.Path == "" {
		return fmt.Errorf("未配置账本文件，输入 /ledger <路径> 设置")
	}
	b.balance(&tx)

	if err := os.MkdirAll(filepath.Dir(b.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(b.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("打开账本失败: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(b.FormatTransaction(tx)); err != nil {
		return fmt.Errorf("写入账本失败: %w", err)
	}
	b.Transactions = append(b.Transactions, tx)
	return nil
}

func (b *Book) balance(tx *Transaction) {
	var net float64
	nullIdx := -1
	for i, p := range tx.Postings {
		if p.HasAmount {
			net += p.Amount
		} else if nullIdx == -1 {
			nullIdx = i
		}
	}
	if nullIdx < 0 {
		return
	}
	tx.Postings[nullIdx].Amount = -net
	tx.Postings[nullIdx].HasAmount = true
	if tx.Postings[nullIdx].Commodity == "" {
		for _, p := range tx.Postings {
			if p.Commodity != "" {
				tx.Postings[nullIdx].Commodity = p.Commodity
				break
			}
		}
	}
}

// FormatTransaction 把一笔交易渲染成 ledger 文本。
func (b *Book) FormatTransaction(tx Transaction) string {
	var sb strings.Builder
	date := tx.Date.Format("2006/01/02")
	if tx.Status == ' ' || tx.Status == 0 {
		tx.Status = '*'
	}
	fmt.Fprintf(&sb, "%s %c %s\n", date, tx.Status, tx.Payee)
	for _, p := range tx.Postings {
		if !p.HasAmount {
			fmt.Fprintf(&sb, "    %s\n", p.Account)
			continue
		}
		amt := strconv.FormatFloat(p.Amount, 'f', 2, 64)
		if p.Commodity != "" {
			amt += " " + p.Commodity
		}
		fmt.Fprintf(&sb, "    %-24s  %s\n", p.Account, amt)
	}
	sb.WriteString("\n")
	return sb.String()
}

// FormatTransactionLine 把交易压成给模型看的一行。
func (b *Book) FormatTransactionLine(tx Transaction) string {
	var parts []string
	for _, p := range tx.Postings {
		if p.HasAmount {
			parts = append(parts, fmt.Sprintf("%s %s", p.Account, strconv.FormatFloat(p.Amount, 'f', 2, 64)))
		} else {
			parts = append(parts, p.Account)
		}
	}
	return fmt.Sprintf("%s  %s  [%s]", tx.Date.Format("2006/01/02"), tx.Payee, strings.Join(parts, " | "))
}
