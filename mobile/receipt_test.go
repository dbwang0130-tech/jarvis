package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

// 模型常在 JSON 外面裹一层 ```json 围栏，或者先说两句客套话。
// 直接 Unmarshal 会失败，而那会让人以为是小票拍糊了。
func TestParseReceiptReply(t *testing.T) {
	for _, tc := range []struct{ name, reply string }{
		{"干净的 JSON", `{"amount":35.5,"payee":"沙县小吃","date":"2026-09-18","account":"Expenses:餐饮"}`},
		{"裹了围栏", "```json\n{\"amount\":35.5,\"payee\":\"沙县小吃\",\"date\":\"2026-09-18\",\"account\":\"Expenses:餐饮\"}\n```"},
		{"前面有客套话", "好的，这张小票是：\n{\"amount\":35.5,\"payee\":\"沙县小吃\",\"date\":\"2026-09-18\",\"account\":\"Expenses:餐饮\"}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseReceiptReply(tc.reply)
			if err != nil {
				t.Fatal(err)
			}
			if out.Amount != 35.5 || out.Payee != "沙县小吃" {
				t.Errorf("认出来的是 %+v", out)
			}
			if out.Date != "2026-09-18" {
				t.Errorf("日期是 %q", out.Date)
			}
		})
	}
}

// 认出来的怪值别让它流进表单：日期格式不对就当没认出来，金额取绝对值
// （小票上的「-35.00」是折扣行，不是负的消费）。
func TestParseReceiptSanitizes(t *testing.T) {
	out, err := parseReceiptReply(`{"amount":-35,"payee":" 星巴克 ","date":"九月十八","account":""}`)
	if err != nil {
		t.Fatal(err)
	}
	if out.Amount != 35 {
		t.Errorf("金额该取正：%v", out.Amount)
	}
	if out.Payee != "星巴克" {
		t.Errorf("没去空白：%q", out.Payee)
	}
	if out.Date != "" {
		t.Errorf("认不出的日期该留空，实际 %q", out.Date)
	}
}

// 看不懂就说看不懂，别拿一堆零糊弄——那会记出一笔 0 元的账。
func TestParseReceiptRefusesJunk(t *testing.T) {
	for _, reply := range []string{"", "这张图我看不清", "```\n不是 JSON\n```"} {
		if _, err := parseReceiptReply(reply); err == nil {
			t.Errorf("%q 该报错", reply)
		}
	}
}

// 最常见的失败是模型不支持看图，而接口返回的多半是一句语焉不详的 400——
// 原样甩给用户，他会以为是小票拍糊了。
func TestExplainReceiptError(t *testing.T) {
	got := explainReceiptError("400 invalid_request: this model does not support image input")
	if !strings.Contains(got, "多模态") {
		t.Errorf("没提示换模型：%s", got)
	}
}

// 烂入参不许 panic。
func TestReadReceiptBadInput(t *testing.T) {
	var out ReceiptOutput
	if err := json.Unmarshal([]byte(ReadReceipt("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}

	// 没给图片、没配模型，都该说清楚而不是发一趟空请求。
	raw, _ := json.Marshal(ReceiptInput{})
	json.Unmarshal([]byte(ReadReceipt(string(raw))), &out)
	if !strings.Contains(out.Error, "没给图片") {
		t.Errorf("没说清缺什么：%s", out.Error)
	}
}
