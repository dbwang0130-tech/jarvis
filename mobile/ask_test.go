package mobile

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

// Ask 会真去调模型，所以这里把 HTTP 层换成按脚本作答的假 Transport，
// 一个网络包都不发。测的是它前后那两头：文件进得去、改动出得来。

type scripted struct {
	replies []string
	seen    []string
	n       int
}

func (t *scripted) RoundTrip(req *http.Request) (*http.Response, error) {
	if body, err := io.ReadAll(req.Body); err == nil {
		t.seen = append(t.seen, string(body))
	}
	if t.n >= len(t.replies) {
		return nil, io.EOF
	}
	body := t.replies[t.n]
	t.n++
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// withFakeModel 把默认 Transport 换掉，测完还原。
func withFakeModel(t *testing.T, replies ...string) *scripted {
	t.Helper()
	tr := &scripted{replies: replies}
	old := http.DefaultTransport
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = old })
	return tr
}

func say(text string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` +
		mustQuote(text) + `}}],"usage":{"total_tokens":9}}`
}

func callTool(name, args string) string {
	return `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function",` +
		`"function":{"name":` + mustQuote(name) + `,"arguments":` + mustQuote(args) + `}}]}}]}`
}

func mustQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func ask(t *testing.T, in AskInput) AskOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	outJSON := Ask(string(raw))
	var out AskOutput
	if err := json.Unmarshal([]byte(outJSON), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	// **顺手守住「出参里不许有 null」**，每条走这个辅助函数的测试都免费带上。
	// Swift 那边数组字段是非可选的，解到 null 整个出参作废、界面一片空白——
	// TestNoNullArrays 专门钉这件事，但让每一条都顺便验一遍更难漏。
	if strings.Contains(outJSON, ":null") {
		t.Errorf("出参里有 null：%s", outJSON)
	}
	return out
}

func testModel() ModelConfig {
	// BaseURL 指向一个不存在的主机：真发出去就会失败，
	// 能跑通说明确实走的是假 Transport。
	return ModelConfig{Provider: "deepseek", APIKey: "sk-测试", BaseURL: "http://模型不存在/v1"}
}

// 秘书改了条目，改动要原样交回来给 Swift 写回 iCloud。
func TestAskReturnsChangedFiles(t *testing.T) {
	withFakeModel(t,
		callTool("update_item", `{"ref":"work.org:2","title":"每周组会","state":"DONE"}`),
		say("标记完成了。"))

	before := "#+TITLE: 工作\n\n* TODO 每周组会\n  SCHEDULED: <2026-09-17 四 10:00>\n"
	out := ask(t, AskInput{
		Files:    map[string]string{"work.org": before},
		Question: "组会开完了",
		Model:    testModel(),
		Now:      "2026-09-16T09:00:00+08:00",
	})

	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if out.Reply != "标记完成了。" {
		t.Fatalf("答复是 %q", out.Reply)
	}
	got, ok := out.Files["work.org"]
	if !ok {
		t.Fatalf("改过的文件没交回来：%+v", out.Files)
	}
	if !strings.Contains(got, "DONE 每周组会") {
		t.Fatalf("状态没改：\n%s", got)
	}
	if got == before {
		t.Fatal("交回来的内容跟原来一模一样")
	}
	// 过程要能展示给用户看。
	if len(out.Steps) == 0 {
		t.Fatal("没有记下干活过程")
	}
}

// 没动过的文件不该出现在出参里——Swift 那边照着写回，多写一次
// 就多一次 iCloud 同步和冲突的机会。
func TestAskOnlyReturnsWhatChanged(t *testing.T) {
	withFakeModel(t,
		callTool("list_todos", `{}`),
		say("你有两件事。"))

	out := ask(t, AskInput{
		Files: map[string]string{
			"work.org":  "* TODO 甲\n",
			"inbox.org": "* TODO 乙\n",
		},
		Question: "我有什么待办",
		Model:    testModel(),
		Now:      "2026-09-16T09:00:00+08:00",
	})

	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if len(out.Files) != 0 {
		t.Fatalf("只是查询却说改了文件：%+v", out.Files)
	}
	if out.Ledger != "" {
		t.Fatal("没碰账本却把账本交回来了")
	}
}

// 记账要把改过的账本交回来。
func TestAskCanRecordTransaction(t *testing.T) {
	withFakeModel(t,
		callTool("add_transaction", `{"date":"2026-09-16","payee":"买菜",
			"postings":[{"account":"Expenses:Food","amount":42.5},{"account":"Assets:Bank:CMB"}]}`),
		say("记好了。"))

	before := "2026/09/01 房租\n    Expenses:Housing:Rent  3000.00\n    Assets:Bank:CMB\n"
	out := ask(t, AskInput{
		Files:    map[string]string{"work.org": "* TODO 甲\n"},
		Ledger:   before,
		Question: "买菜花了 42.5",
		Model:    testModel(),
		Now:      "2026-09-16T09:00:00+08:00",
	})

	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if out.Ledger == "" {
		t.Fatal("账本改了却没交回来")
	}
	if !strings.Contains(out.Ledger, "买菜") {
		t.Fatalf("新交易没写进去：\n%s", out.Ledger)
	}
	if !strings.Contains(out.Ledger, "房租") {
		t.Fatal("原有的流水被弄丢了")
	}
}

// 多轮对话：上一轮的历史传回来，秘书得接得上。
func TestAskKeepsHistory(t *testing.T) {
	withFakeModel(t, say("好的。"))

	out := ask(t, AskInput{
		Files:    map[string]string{"work.org": "* TODO 甲\n"},
		Question: "你好",
		Model:    testModel(),
		History: []agent.Message{
			{Role: "user", Content: "上一轮说的话"},
			{Role: "assistant", Content: "上一轮的答复"},
		},
		Now: "2026-09-16T09:00:00+08:00",
	})

	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if len(out.History) < 4 {
		t.Fatalf("历史该接上前面两轮再加这一轮，现在 %d 条", len(out.History))
	}
	if out.History[0].Content != "上一轮说的话" {
		t.Fatalf("前面的历史丢了：%+v", out.History[0])
	}
	if last := out.History[len(out.History)-1]; last.Content != "好的。" {
		t.Fatalf("这一轮的答复没进历史：%+v", last)
	}
}

// 手机上崩一次就是整个 app 挂掉，烂输入都得稳稳接住。
func TestAskNeverPanics(t *testing.T) {
	withFakeModel(t, say("好的。"), say("好的。"), say("好的。"))

	for _, in := range []string{
		"", "不是 JSON", "null", "{}",
		`{"question":"在吗"}`,
		`{"question":"在吗","model":{"provider":"deepseek"}}`,
		`{"question":"","model":{"provider":"deepseek","api_key":"x"}}`,
		`{"question":"在吗","model":{"provider":"没这个服务商","api_key":"x"}}`,
		`{"question":"在吗","model":{"provider":"deepseek","api_key":"x"},"now":"不是时间"}`,
		`{"files":{"../跑出去.org":"* TODO 甲\n"},"question":"在吗",
		  "model":{"provider":"deepseek","api_key":"x","base_url":"http://模型不存在/v1"}}`,
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("输入 %q 让它崩了：%v", in, r)
				}
			}()
			var out AskOutput
			if err := json.Unmarshal([]byte(Ask(in)), &out); err != nil {
				t.Errorf("输入 %q 的出参不是合法 JSON", in)
			}
		}()
	}
}

// 文件名里的 ../ 不能让它写到临时目录外面去。
func TestAskConfinesFileNames(t *testing.T) {
	withFakeModel(t, say("好的。"))

	out := ask(t, AskInput{
		Files:    map[string]string{"../../跑出去.org": "* TODO 甲\n"},
		Question: "在吗",
		Model:    testModel(),
		Now:      "2026-09-16T09:00:00+08:00",
	})
	// 不崩、不报错就行；文件被规整成了基名，留在临时目录里。
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
}

// 没配模型要说清楚，而不是发一个必然失败的请求。
func TestAskWithoutModel(t *testing.T) {
	out := ask(t, AskInput{
		Files:    map[string]string{"work.org": "* TODO 甲\n"},
		Question: "在吗",
	})
	if out.Error == "" {
		t.Fatal("没配模型却没报错")
	}
	if !strings.Contains(out.Error, "服务商") {
		t.Fatalf("该说清楚缺什么：%q", out.Error)
	}
}

// 被叫停时**不能当成失败**：工具已经改过的文件就在临时目录里，
// 直接返回错误那些改动就丢了，用户看到「什么都没发生」，实际上一半的事
// 已经做完。这条钉住取消开关本身的行为——没跑起来时喊停不该炸。
func TestCancelIsSafe(t *testing.T) {
	Cancel() // 什么都没跑的时候喊停
	Cancel() // 连喊两次

	ctx := startCancelable()
	Cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("喊停了但 ctx 没结束")
	}
	endCancelable()

	// 收工之后再喊停，不该影响下一轮。
	Cancel()
	next := startCancelable()
	select {
	case <-next.Done():
		t.Fatal("上一轮的喊停波及了下一轮")
	default:
	}
	endCancelable()
}

// 新一轮开始时要把上一轮还挂着的取消函数收掉，不然越积越多。
func TestStartCancelsPrevious(t *testing.T) {
	first := startCancelable()
	second := startCancelable()
	select {
	case <-first.Done():
	default:
		t.Error("开新一轮时没把上一轮收掉")
	}
	select {
	case <-second.Done():
		t.Error("新一轮不该一开始就是结束的")
	default:
	}
	endCancelable()
}

// 带图片问一句，整份出参必须还能被「content 是 String」的那种结构解开。
//
// 这是手机上发图必报「没办成：出参解不开」的那个 bug。多模态格式化原先
// 写成了 agent.Message 的 MarshalJSON，而方法是挂在类型上的——AskOutput
// 里的 History 用的是同一个类型，于是回传给 Swift 的那条用户消息 content
// 变成了数组。Swift 那边声明的是 String，整个出参就解不动了，界面只说
// 一句「格式不正确」，看不出是图片引起的。
//
// 图片在调模型**之前**就进了 History（agent.AskWith 第一件事就是 append），
// 所以这条路跟模型认不认得图片无关：发了图就必错。
func TestAskWithImageReturnsDecodableOutput(t *testing.T) {
	withFakeModel(t, say("看到了。"))

	// 走原始出参，照着 Swift 那边的形状解：content 是 String，不是数组。
	raw := Ask(mustJSON(t, AskInput{
		Files:    map[string]string{"work.org": "#+TITLE: 工作\n"},
		Question: "这张小票多少钱",
		Images:   []string{"data:image/png;base64,iVBORw0KGgo="},
		Model:    testModel(),
		Now:      "2026-09-16T09:00:00+08:00",
	}))
	var swiftSide struct {
		History []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"history"`
	}
	if err := json.Unmarshal([]byte(raw), &swiftSide); err != nil {
		t.Fatalf("Swift 那边就是这么解的，解不开就是界面上那句「出参解不开」：%v", err)
	}
	if len(swiftSide.History) == 0 {
		t.Fatal("History 空了")
	}
	if got := swiftSide.History[0].Content; got != "这张小票多少钱" {
		t.Fatalf("用户那条消息没原样回来：%q", got)
	}
	// 图片不该跟着存档回去——它只服务于发出去那一次，塞回来白白撑大存档。
	if strings.Contains(raw, "iVBORw0KGgo") {
		t.Fatal("图片跟着出参回来了")
	}
}
