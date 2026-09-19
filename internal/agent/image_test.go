package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

// 带图片时 content 要摊成「文本块 + 图片块」的数组，这是 OpenAI 兼容接口
// 对多模态的规定格式。
func TestMessageWithImages(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: "这张小票多少钱",
		Images:  []string{"data:image/jpeg;base64,AAAA"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	parts, ok := got["content"].([]any)
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

// **没图片时一个字节都不能变。** 绝大多数请求都不带图片，不能让多模态
// 这条路把普通对话的线上格式也改了——有的服务商对数组形式的 content
// 支持得并不好。
func TestMessageWithoutImagesUnchanged(t *testing.T) {
	m := Message{Role: "user", Content: "今天干嘛"}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"content":"今天干嘛"`) {
		t.Fatalf("普通消息的 content 不该变成数组：%s", raw)
	}

	// 工具调用那类消息也照旧。
	tool := Message{Role: "tool", Content: "好了", ToolCallID: "call_1"}
	raw, _ = json.Marshal(tool)
	if !strings.Contains(string(raw), `"tool_call_id":"call_1"`) {
		t.Fatalf("tool_call_id 丢了：%s", raw)
	}
}
