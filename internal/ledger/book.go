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
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	// Note 是挂在这笔账上的一行注释，写成 `; 小票: receipts/...`。
	//
	// **相对路径**：这份账本在电脑和手机上都会被读，而两边的容器路径
	// 完全不同，存绝对路径等于只有存的那一端找得到。
	//
	// 走注释而不是另建一份索引：账本是纯文本、是用户自己的文件，
	// 图片在哪儿这件事就该跟那笔账待在一起——哪天不用这个 app 了，
	// 拿记事本打开也看得见。ledger-cli 会把 `;` 开头的行当注释跳过，
	// 不影响它算账。
	Note string
	// Line 是这笔交易在文件里的起始行（0 起），End 是它的下一行。
	//
	// 用来定位原始那几行——**不是为了删掉它**。账本是流水账，记错了
	// 要冲账（记一笔反向的），不能回头改历史：改过的账没法对，也说不清
	// 当时到底发生过什么。这是 ledger 的规矩，不是这个项目的偏好。
	Line int
	End  int
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
// ParseBook 从文本解析账本，不碰文件系统。
// 其他客户端 那边文件要经沙箱协调读写，Go 这头只当纯函数用。
func ParseBook(text string) *Book {
	b := &Book{}
	b.parse(text)
	return b
}

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
	// 收尾时 End 停在最后一条 posting 的下一行，不含后面那些空行——
	// 删除时把空行留给上一笔，不然连着删几笔会把段落之间的空行也吃掉。
	end := 0
	flush := func() {
		if cur != nil {
			cur.End = end
			b.Transactions = append(b.Transactions, *cur)
			cur = nil
		}
	}
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		// 顶格的注释是文件级的（`; 我的账本`），跳过；缩进的注释挂在
		// 当前这笔账上（`; 小票: ...`），下面认。**这个判断必须在
		// indented 算出来之后**——早先这儿不分青红皂白跳过所有注释行，
		// 挂在交易上的那条根本轮不到被读。
		if isComment(trimmed) && !indented {
			continue
		}
		if !indented && dateRe.MatchString(line) {
			flush()
			tx := parseTransaction(line)
			tx.Line = i
			cur = &tx
			end = i + 1
			continue
		}
		if indented && cur != nil {
			// 缩进的注释行是挂在这笔账上的（`; 小票: ...`），不是分录。
			// 不认出来的话它会被当成一条科目名叫「; 小票: ...」的分录。
			if isComment(trimmed) {
				cur.Note = strings.TrimSpace(strings.TrimLeft(trimmed, ";#%|*"))
				end = i + 1
				continue
			}
			cur.Postings = append(cur.Postings, parsePosting(trimmed))
			end = i + 1
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
		for _, p := range tx.Resolved() {
			out[p.Account] += p.Amount
		}
	}
	return out
}

// Resolved 返回补齐了金额的分录。
//
// ledger 允许留一行不写金额，由其余各行倒推——这是这个格式最常见的写法：
//
//	2026/09/12 午饭
//	    Expenses:Food     35.00
//	    Assets:Bank                 ← 这行是 -35.00
//
// 补齐的规则只该有一份定义。算余额、看流水、将来做报表都要用它，
// 各自照着推一遍的话，迟早在「两行都没写金额」这类边角上走散。
// 多于一行没写金额时，只补第一行，其余按 0 算——ledger 本身也不接受
// 两个空缺，硬猜出来的数更容易骗人。
func (t Transaction) Resolved() []Posting {
	var net float64
	blank := -1
	for i, p := range t.Postings {
		if p.HasAmount {
			net += p.Amount
		} else if blank == -1 {
			blank = i
		}
	}
	out := make([]Posting, len(t.Postings))
	for i, p := range t.Postings {
		if !p.HasAmount {
			p.Amount = 0
			if i == blank {
				p.Amount = -net
			}
			p.HasAmount = true
		}
		out[i] = p
	}
	return out
}

// SortedAccounts 返回按字母序排列的账户名。
func (b *Book) SortedAccounts() []string { return SortedFrom(b.Balances()) }

// SumPrefix 把某一类账户的余额加起来。传算好的余额表进来，
// 免得为每个前缀重扫一遍账本。前缀按账户层级卡边界：
// "Assets" 只匹配 Assets 和 Assets:xxx，不会连上 AssetsFoo。
func SumPrefix(bal map[string]float64, prefix string) float64 {
	prefix = strings.TrimSuffix(prefix, ":") // "Assets" 和 "Assets:" 都当成同一类
	var sum float64
	for name, v := range bal {
		if name == prefix || strings.HasPrefix(name, prefix+":") {
			sum += v
		}
	}
	return sum
}

// SortedFrom 把算好的余额表里的账户名排好序。
func SortedFrom(bal map[string]float64) []string { return slices.Sorted(maps.Keys(bal)) }

// SumByPrefix 汇总指定前缀账户的余额，每次都重算一遍余额表。
// 一次要问好几个前缀的话，自己 Balances() 一次再调 SumPrefix 更划算。
func (b *Book) SumByPrefix(prefix string) float64 { return SumPrefix(b.Balances(), prefix) }

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
	if n := strings.TrimSpace(tx.Note); n != "" {
		// 注释跟在标题行后面、分录前面：ledger-cli 认这个位置，
		// 人翻账本时也是先看见「这笔有小票」再看见金额。
		fmt.Fprintf(&sb, "    ; %s\n", n)
	}
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
