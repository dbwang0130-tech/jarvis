// 命令 gen 把 mobile 包的真实出参写到临时目录，交给 check.swift 用
// iOS 那边真正在用的 Models.swift 解一遍。
//
// 这一步单独存在是因为：Go 改了字段名或让某个数组变成 nil，Go 自己的测试
// 照样绿，装到手机上才发现整个界面空白。字段名就是两端的契约，得有人对。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/mobile"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: gen <输出目录>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}

	// 尽量把边角都占上：有标签的和没标签的、子任务、循环事件、多种 kind。
	org := `#+TITLE: 工作

* NEXT [#A] 写毕业论文  :thesis:work:
  SCHEDULED: <2026-09-16 三 14:00-16:00>
** TODO 补开题报告 [1/3]
   DEADLINE: <2026-09-18 五>
   - [X] 查文献
   - [ ] 写初稿
   - [ ] 改格式
* TODO 每周组会
  SCHEDULED: <2026-09-17 四 10:00 +1w>
* TODO 没有任何标签的待办
`
	ledger := `2026/09/01 房租
    Expenses:Housing:Rent          3000.00
    Assets:Bank:CMB

2026/09/10 发工资
    Assets:Bank:CMB               18000.00
    Income:Salary
`
	in, _ := json.Marshal(map[string]any{
		"files":  map[string]string{"work.org": org},
		"ledger": ledger,
		"days":   7,
		"now":    "2026-09-16T09:00:00+08:00",
	})

	write(dir, "snapshot.json", mobile.Snapshot(string(in)))
	// 空输入也得能解——第一次装上、目录还是空的就是这个形状。
	write(dir, "snapshot_empty.json", mobile.Snapshot(`{}`))
	// 出错那条路同样要能解，不然用户看到的是解码失败而不是真正的原因。
	write(dir, "snapshot_error.json", mobile.Snapshot("不是 JSON"))
	write(dir, "ask_error.json", mobile.Ask("不是 JSON"))

	// Ask 干完活时的形状。history 里那条「只调工具不说话」的 assistant 消息
	// 是关键：agent.Message.Content 带 omitempty，空串会让整个 content 键消失。
	// 真机上第一次让秘书动日程就栽在这儿——Swift 解不出来，整轮作废。
	ok, err := json.Marshal(mobile.AskOutput{
		Reply: "挪好了。",
		Files: map[string]string{"work.org": "改过的内容"},
		History: []agent.Message{
			{Role: "user", Content: "把周会挪到周五"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{
				ID: "call_1", Type: "function",
				Function: agent.FunctionCall{Name: "schedule_item", Arguments: `{"ref":"work.org:9"}`},
			}}},
			{Role: "tool", ToolCallID: "call_1", Name: "schedule_item", Content: "已安排"},
			{Role: "assistant", Content: "挪好了。"},
		},
		Steps: []mobile.Step{{Kind: "tool", Name: "schedule_item", Text: "ref=work.org:9"}},
	})
	if err != nil {
		panic(err)
	}
	write(dir, "ask_ok.json", string(ok))
}

func write(dir, name, content string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		panic(err)
	}
}
