package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// ReceiptInput 认一张小票。
//
// 图片走 data URL（`data:image/jpeg;base64,...`），不传路径：Go 这头够不着
// iCloud 里的文件（那是主 app 拿安全作用域书签才能碰的），而且识别完就
// 不需要它了——**图片本身由 Swift 那边存进 iCloud**，这儿只负责认。
type ReceiptInput struct {
	Image string      `json:"image"`
	Model ModelConfig `json:"model"`
	// Accounts 是账本里已有的科目名，让模型从里头挑而不是自己编。
	//
	// 不给的话它会编出 `Expenses:Food` 这种——名字对不上已有的科目，
	// 账本里就凭空多一个账户，而且要过很久才被发现。
	Accounts []string `json:"accounts"`
	TZ       string   `json:"tz"`
}

// ReceiptOutput 是认出来的几项，**都要拿给人过目**，不直接落账。
type ReceiptOutput struct {
	Amount float64 `json:"amount"`
	Payee  string  `json:"payee"`
	Date   string  `json:"date"`
	// Account 是猜的支出科目，只会是 Accounts 里给过的那些之一；
	// 拿不准就空着，让人自己挑。
	Account string `json:"account"`
	Error   string `json:"error,omitempty"`
}

func (o *ReceiptOutput) normalize() {}

// 认小票的提示词。
//
// 要点都写死在这儿而不是留给模型自由发挥：
//   - 只认**实付金额**。小票上数字最多的往往是「原价」「小计」「优惠前」，
//     认错了账就记多了。
//   - 日期认小票上印的那个，不是今天——补记前几天的小票是常事。
//   - 科目只能从给定清单里挑，挑不出就空着。编一个出来比空着糟得多。
const receiptPrompt = `你在读一张消费小票，把它变成一条记账。

只认**实付金额**（实收、应付、合计中最后真正付掉的那个数）。
小票上常有原价、小计、优惠前金额，那些都不要。

日期认小票上印的那个，格式 yyyy-MM-dd。印得不清楚或者没有就留空，
不要拿今天顶上。

payee 写商家名字，简短即可。

account 只能从下面这些科目里挑一个最贴切的，挑不出就留空——
不要自己造新科目名：
%s

只回 JSON，不要别的话：
{"amount": 数字, "payee": "商家", "date": "yyyy-MM-dd", "account": "科目名"}`

// ReadReceipt 让模型看一眼小票，认出金额、商家、日期、科目。
func ReadReceipt(inputJSON string) string {
	var in ReceiptInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&ReceiptOutput{
			Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	if strings.TrimSpace(in.Image) == "" {
		return marshal(&ReceiptOutput{Error: "没给图片"})
	}
	if strings.TrimSpace(in.Model.Provider) == "" || strings.TrimSpace(in.Model.APIKey) == "" {
		return marshal(&ReceiptOutput{Error: "还没配模型，去设置里填一下"})
	}

	client, err := agent.New(agent.Options{
		Provider:    in.Model.Provider,
		Model:       in.Model.Model,
		BaseURL:     in.Model.BaseURL,
		APIKey:      in.Model.APIKey,
		Timeout:     time.Duration(orDefault(in.Model.TimeoutSec, 60)) * time.Second,
		Proxy:       "none", // 手机上别去读环境变量里的代理
		Temperature: 0,      // 认数字这件事不需要创造力
	})
	if err != nil {
		return marshal(&ReceiptOutput{Error: err.Error()})
	}

	accounts := "（账本里还没有支出科目）"
	if len(in.Accounts) > 0 {
		accounts = strings.Join(in.Accounts, "\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(orDefault(in.Model.TimeoutSec, 60))*time.Second)
	defer cancel()

	msg := agent.Message{
		Role:    "user",
		Content: fmt.Sprintf(receiptPrompt, accounts),
		Images:  []string{in.Image},
	}
	// 不给工具：这一趟只要它看图回话，给了工具反而可能绕进工具调用里。
	reply, _, err := client.Chat(ctx, []agent.Message{msg}, nil)
	if err != nil {
		return marshal(&ReceiptOutput{Error: explainReceiptError(err.Error())})
	}

	out, perr := parseReceiptReply(reply.Content)
	if perr != nil {
		return marshal(&ReceiptOutput{Error: perr.Error()})
	}
	// 科目对不上已有的就丢掉——宁可让人自己挑，也不能往账本里塞一个
	// 编出来的科目名。
	if out.Account != "" && !contains(in.Accounts, out.Account) {
		out.Account = ""
	}
	return marshal(out)
}

// parseReceiptReply 从模型的回话里抠出那段 JSON。
//
// 模型常会在 JSON 外面裹一层 ```json 围栏或者说两句客套话，直接
// Unmarshal 会失败。从第一个 { 到最后一个 } 之间取。
func parseReceiptReply(reply string) (*ReceiptOutput, error) {
	i := strings.Index(reply, "{")
	j := strings.LastIndex(reply, "}")
	if i < 0 || j <= i {
		return nil, fmt.Errorf("没看懂这张小票：%s", cut(strings.TrimSpace(reply), 80))
	}
	var out ReceiptOutput
	if err := json.Unmarshal([]byte(reply[i:j+1]), &out); err != nil {
		return nil, fmt.Errorf("没看懂这张小票：%s", cut(strings.TrimSpace(reply), 80))
	}
	out.Payee = strings.TrimSpace(out.Payee)
	out.Date = strings.TrimSpace(out.Date)
	out.Account = strings.TrimSpace(out.Account)
	// 认出来的日期不合格式就当没认出来，别让一个怪值流进表单。
	if out.Date != "" {
		if _, err := time.Parse("2006-01-02", out.Date); err != nil {
			out.Date = ""
		}
	}
	if out.Amount < 0 {
		out.Amount = -out.Amount
	}
	return &out, nil
}

// explainReceiptError 把服务商的英文错误翻成能照着动手的话。
//
// 认小票最常见的失败是**模型不支持看图**，而接口返回的多半是一句
// 语焉不详的 400——原样甩给用户，他会以为是小票拍糊了。
func explainReceiptError(raw string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "image"), strings.Contains(l, "vision"),
		strings.Contains(l, "multimodal"), strings.Contains(l, "not support"):
		return "这个模型看不了图。换一个多模态的（比如 qwen-vl-max、doubao-vision、glm-4v），" +
			"在设置里改模型名。"
	case strings.Contains(l, "401"), strings.Contains(l, "unauthorized"):
		return "密钥不对，去设置里检查一下。"
	case strings.Contains(l, "timeout"), strings.Contains(l, "deadline"):
		return "等太久了。图片有点大的话，换个网络再试。"
	case strings.Contains(l, "413"), strings.Contains(l, "too large"):
		return "图片太大了，服务商收不下。"
	default:
		return raw
	}
}

// cut 把太长的原话截短，只为在错误里带个样子。
//
// agent 包里有个同名的，但那个没导出——为一句错误提示去导出它，
// 等于把内部细节变成公开接口。这儿几行就够。
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
