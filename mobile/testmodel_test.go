package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func runModelTest(t *testing.T, in TestModelInput) TestModelOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out TestModelOutput
	if err := json.Unmarshal([]byte(TestModel(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

// 缺东西要当场说清楚，不能等真去连一趟才报错。
func TestModelChecksConfigFirst(t *testing.T) {
	cases := []struct {
		name string
		in   TestModelInput
		want string
	}{
		{"没选服务商", TestModelInput{}, "没选服务商"},
		{"没填密钥", TestModelInput{Model: ModelConfig{Provider: "deepseek"}}, "没填密钥"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runModelTest(t, tc.in)
			if out.OK {
				t.Error("不该说通了")
			}
			if out.Message != tc.want {
				t.Errorf("提示是 %q，该是 %q", out.Message, tc.want)
			}
		})
	}

	var out TestModelOutput
	if err := json.Unmarshal([]byte(TestModel("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("烂入参该报错")
	}
}

// 服务商甩回来的英文错误要翻成能照着动手的话——原样给个 401，
// 用户也不知道该改哪一项。
func TestExplainModelError(t *testing.T) {
	cases := map[string]string{
		"status 401: Unauthorized":          "密钥",
		"error 402 insufficient balance":    "余额",
		"model_not_found: no such model":    "模型名",
		"429 Too Many Requests: rate limit": "限流",
		"context deadline exceeded":         "超时",
		"dial tcp: no such host api.x.com":  "连不上",
		"某个谁也没见过的错":                         "某个谁也没见过的错",
	}
	for raw, want := range cases {
		if got := explainModelError(raw); !strings.Contains(got, want) {
			t.Errorf("explainModelError(%q) = %q，该提到 %q", raw, got, want)
		}
	}
}
