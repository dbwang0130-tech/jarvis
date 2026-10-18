package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// 带图片时，**发给模型**的 content 要摊成「文本块 + 图片块」的数组，
// 这是 OpenAI 兼容接口对多模态的规定格式。
func TestWireMessagesWithImages(t *testing.T) {
	raw, err := json.Marshal(wireMessages{{
		Role:    "user",
		Content: "这张小票多少钱",
		Images:  []string{"data:image/jpeg;base64,AAAA"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	parts, ok := got[0]["content"].([]any)
	if !ok {
		t.Fatalf("content 不是数组：%s", raw)
	}
	if len(parts) != 2 {
		t.Fatalf("该有文本和图片两块，实际 %d 块：%s", len(parts), raw)
	}
	first := parts[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "这张小票多少钱" {
		t.Errorf("第一块不是文本：%v", first)
	}
	second := parts[1].(map[string]any)
	if second["type"] != "image_url" {
		t.Errorf("第二块不是图片：%v", second)
	}
}

// **同一条消息单独序列化时，content 必须还是个字符串。**
//
// 这条曾经抓到过图片出参解不开的问题：多模态格式化原先写成了
// Message.MarshalJSON，方法挂在类型上，凡是序列化 Message 的地方都会走它——
// 包括历史存档；历史消息的 content 必须保持字符串。
//
// 现在格式化挂在 wireMessages 上，只有拼请求才经过。
func TestMessageWithImagesStaysAStringWhenStored(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: "这张小票多少钱",
		Images:  []string{"data:image/jpeg;base64,AAAA"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content":"这张小票多少钱"`) {
		t.Fatalf("存档/回传的 content 该是字符串：%s", raw)
	}
	// 图片本身不该跟着存档走——它只服务于发出去那一次。
	if strings.Contains(string(raw), "base64") {
		t.Fatalf("图片不该出现在存档里：%s", raw)
	}
}

// 整份 History 里混着带图和不带图的消息，也得整体解得开——
// 存档读取方收到的就是这个形状。
func TestHistoryWithImagesDecodesAsPlainMessages(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "看看这个", Images: []string{"data:image/png;base64,BBBB"}},
		{Role: "assistant", Content: "是张小票"},
	}
	raw, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	// 会话存档（`jarvis -c`、ACP 的 session/load）就是按 content 为字符串去读的。
	var back []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("History 按字符串 content 解不开，会话存档读不回来：%v\n%s", err, raw)
	}
	if len(back) != 2 || back[0].Content != "看看这个" {
		t.Fatalf("解出来不对：%+v", back)
	}
}

// **没图片时一个字节都不能变。** 绝大多数请求都不带图片，不能让多模态
// 这条路把普通对话的线上格式也改了——有的服务商对数组形式的 content
// 支持得并不好。
func TestMessageWithoutImagesUnchanged(t *testing.T) {
	raw, err := json.Marshal(wireMessages{{Role: "user", Content: "今天干嘛"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content":"今天干嘛"`) {
		t.Fatalf("普通消息的 content 不该变成数组：%s", raw)
	}

	// 工具调用那类消息也照旧。
	raw, _ = json.Marshal(wireMessages{{Role: "tool", Content: "好了", ToolCallID: "call_1"}})
	if !strings.Contains(string(raw), `"tool_call_id":"call_1"`) {
		t.Fatalf("tool_call_id 丢了：%s", raw)
	}
}

// 发给模型的那份**只含 OpenAI 认的字段**。
//
// Message 上迟早会加只给自己人看的东西，跟着漏出去的话，多余字段有的服务商
// 忍了、有的直接 400，而那种错完全指不到这儿。所以发出去的是显式列字段的
// modelMessage，不是整个 Message。
func TestOnlyWireFieldsReachTheModel(t *testing.T) {
	raw, err := json.Marshal(wireMessages{
		{Role: "user", Content: "看看这个", Images: []string{"data:image/png;base64,CCCC"}},
		{Role: "assistant", Content: "好"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	wire := map[string]bool{
		"role": true, "content": true, "tool_calls": true,
		"tool_call_id": true, "name": true, "reasoning_content": true,
	}
	for _, m := range out {
		for k := range m {
			if !wire[k] {
				t.Errorf("发给模型的消息里混进了 %q：%s", k, raw)
			}
		}
	}
}
