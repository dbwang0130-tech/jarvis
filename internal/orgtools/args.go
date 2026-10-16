package orgtools

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// args 是一次工具调用解开的参数。
//
// 单独抽出来是因为「**给没给**」这件事本身就是语义的一部分，而不是取值的
// 副产品：update_item 靠它区分「没提这一项，别动」和「提了但给空串，清掉」。
// 早先每个业务函数各自在 map 上手搓一份 getStr/has 闭包（一度有三份同形的），
// 谁也不知道别处是按「键在不在」还是按「值是不是零」判断的——move_item 的
// 两条分支就曾经一条一个样。
type args map[string]any

// parseArgs 把模型给的 JSON 参数解开。空参数是合法的（有些工具不收参数）。
func parseArgs(raw string) (args, error) {
	a := args{}
	if strings.TrimSpace(raw) == "" {
		return a, nil
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return nil, fmt.Errorf("参数不是合法 JSON: %w", err)
	}
	return a, nil
}

// Has 报告这个字段在不在参数里。**跟「值是不是空」是两回事。**
func (a args) Has(k string) bool { _, ok := a[k]; return ok }

// Str 取一个字符串，缺了就是空串。
func (a args) Str(k string) string {
	v, _ := a[k].(string)
	return strings.TrimSpace(v)
}

// OptStr 取一个字符串，并如实交出这个字段在不在。
func (a args) OptStr(k string) optStr {
	if !a.Has(k) {
		return optStr{}
	}
	return optStr{v: a.Str(k), ok: true}
}

// Int 取一个整数。模型有时候把数字写成字符串，两种都认。
func (a args) Int(k string, def int) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return def
}

// Bool 取一个布尔值，缺了就是 false。
func (a args) Bool(k string) bool {
	b, _ := a[k].(bool)
	return b
}

// Strings 取一个字符串数组，跳过空项。
func (a args) Strings(k string) []string {
	raw, ok := a[k].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range raw {
		if s, ok := item.(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// optStr 是一个「可能没给」的字符串。给了空串和根本没给是两回事。
type optStr struct {
	v  string
	ok bool
}

// Or 在没给的时候返回兜底值。
func (o optStr) Or(def string) string {
	if o.ok {
		return o.v
	}
	return def
}

// itemPatch 是 create_item 和 update_item 共用的那组字段。
//
// **两个工具对空串的处理是相反的**：新建时没说状态就落成默认的 TODO，
// 修改时给了空串却是要把状态去掉。早先用 "" 同时兼任「没给」和「给了空」，
// 于是两个工具各记各的约定，调用方也跟着分裂——调用方 create 分支靠
// 「键不放进 map」表示不设，update 分支靠「键放进去但值为空」表示清除，
// 全凭写的人记得。现在「给没给」由 optStr 带着走，两处读同一份结构。
type itemPatch struct {
	State     optStr
	Priority  optStr
	Scheduled optStr
	Deadline  optStr
	Repeat    optStr
}

// patchFrom 从参数里挑出这组字段。
func patchFrom(a args) itemPatch {
	return itemPatch{
		State:     a.OptStr("state"),
		Priority:  a.OptStr("priority"),
		Scheduled: a.OptStr("scheduled"),
		Deadline:  a.OptStr("deadline"),
		Repeat:    a.OptStr("repeat"),
	}
}

// empty 报告这份 patch 一项都没提。
func (p itemPatch) empty() bool {
	return !p.State.ok && !p.Priority.ok && !p.Scheduled.ok &&
		!p.Deadline.ok && !p.Repeat.ok
}

// checkPriority 校验优先级的写法。空串是合法的——那是「去掉优先级」。
func checkPriority(prio string) (string, error) {
	prio = strings.ToUpper(strings.TrimSpace(prio))
	if prio != "" && (len(prio) != 1 || prio[0] < 'A' || prio[0] > 'Z') {
		return "", fmt.Errorf("优先级 %q 不合法，应为单个字母，如 A", prio)
	}
	return prio, nil
}
