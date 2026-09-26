package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeModels(t *testing.T, out string) ListModelsOutput {
	t.Helper()
	var v ListModelsOutput
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("出参不是合法 JSON：%v\n%s", err, out)
	}
	return v
}

// 烂输入一律填在 error 字段里，不许 panic——手机上崩一次就是整个 app 挂掉。
// 这几条都不碰网络，入参那一层就挡下来了。
func TestListModelsRejectsBadInput(t *testing.T) {
	for name, in := range map[string]string{
		"不是 JSON":  "不是 JSON",
		"空对象":      `{}`,
		"服务商是空白":   `{"model":{"provider":"   "}}`,
		"model 缺失": `{"files":{}}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := decodeModels(t, ListModels(in))
			if got.Error == "" {
				t.Errorf("该报错却没报：%+v", got)
			}
			if got.Message == "" {
				t.Error("没给人看的那句话")
			}
			// 出错那条路上数组也得是 []，Swift 那边 models 是非可选的。
			if got.Models == nil {
				t.Error("models 是 null，Swift 解不了")
			}
		})
	}
}

// 认不出的服务商名要当场说清，别等到发请求才超时——那会儿人已经等了二十秒。
func TestListModelsUnknownProvider(t *testing.T) {
	got := decodeModels(t, ListModels(`{"model":{"provider":"根本没这家"}}`))
	if got.Error == "" {
		t.Fatalf("没报错：%+v", got)
	}
	if strings.Contains(got.Message, "毫秒") {
		t.Error("看起来是真发了请求")
	}
}
