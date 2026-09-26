package agent

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// modelsTransport 只答 GET /models，并记下收到的地址和 Authorization。
type modelsTransport struct {
	status int
	body   string
	url    string
	auth   string
}

func (t *modelsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.url = req.URL.String()
	t.auth = req.Header.Get("Authorization")
	st := t.status
	if st == 0 {
		st = 200
	}
	return &http.Response{
		StatusCode: st,
		Body:       io.NopCloser(strings.NewReader(t.body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func modelsClient(tr *modelsTransport) *Client {
	return &Client{
		BaseURL: "https://例子/v1/", // 末尾那个斜杠是故意的，拼地址不能拼出 //models
		APIKey:  "key-1",
		HTTP:    &http.Client{Transport: tr},
	}
}

// 服务商报的那几项要如实带出来，**尤其是「认不认图片」**：它原先硬编码在
// Presets 里，而厂商自己就说了。手机那端会拍照记账，选了纯文本模型，图发出去
// 石沉大海，是最难查的一类错。
func TestModelsReadsVendorList(t *testing.T) {
	tr := &modelsTransport{body: `{"data":[
		{"id":"deepseek-flash","name":"DeepSeek-V4.1-Flash",
		 "input_modalities":["text","image"],"context_window":1048576},
		{"id":"deepseek-v4-pro","name":"DeepSeek-V4-Pro",
		 "input_modalities":["text"],"context_window":1048576}]}`}
	got, err := modelsClient(tr).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tr.url != "https://%E4%BE%8B%E5%AD%90/v1/models" {
		t.Errorf("地址拼错了：%s", tr.url)
	}
	if tr.auth != "Bearer key-1" {
		t.Errorf("没带密钥：%q", tr.auth)
	}
	if len(got) != 2 {
		t.Fatalf("要 2 个，拿到 %d 个：%+v", len(got), got)
	}
	if !got[0].Vision() {
		t.Error("input_modalities 里有 image，该认得图片")
	}
	if got[1].Vision() {
		t.Error("只有 text 却报认得图片")
	}
	if got[0].Name != "DeepSeek-V4.1-Flash" || got[0].Context != 1048576 {
		t.Errorf("元数据没带出来：%+v", got[0])
	}
}

// 只有 id 是协议规定的，其余全是各家的扩展。缺了它们**不能让整张列表作废**
// ——挑模型这件事本身只需要 id，少显示一行窗口大小无所谓。
func TestModelsToleratesMissingExtras(t *testing.T) {
	tr := &modelsTransport{body: `{"data":[
		{"id":"gpt-x","object":"model","owned_by":"某家"}]}`}
	got, err := modelsClient(tr).Models(context.Background())
	if err != nil {
		t.Fatalf("只给 id 就解不开了：%v", err)
	}
	if len(got) != 1 || got[0].ID != "gpt-x" {
		t.Fatalf("没拿到那一个：%+v", got)
	}
	if got[0].Name != "" || got[0].Context != 0 || got[0].Vision() {
		t.Errorf("没报的东西被猜出来了：%+v", got[0])
	}
}

// 列表里混着嵌入、重排、语音那些，挑的人看见一堆用不上的名字，
// 反而比自己打字更难。
func TestModelsDropsNonChat(t *testing.T) {
	tr := &modelsTransport{body: `{"data":[
		{"id":"chat-v1"},{"id":"text-embedding-3"},{"id":"bge-reranker"},
		{"id":"whisper-1"},{"id":"tts-1-hd"},{"id":"moderation-latest"},
		{"id":"chat-v2"}]}`}
	got, err := modelsClient(tr).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "chat-v1,chat-v2" {
		t.Errorf("过滤不对，剩下：%v", ids)
	}
}

// 密钥不对要说成「密钥」，不能只甩一个状态码——这是设置页里唯一能自己纠正的错。
func TestModelsExplainsRefusedKey(t *testing.T) {
	tr := &modelsTransport{status: 401, body: `{"error":{"message":"bad key"}}`}
	_, err := modelsClient(tr).Models(context.Background())
	if err == nil {
		t.Fatal("401 也当成功")
	}
	if !strings.Contains(err.Error(), "密钥") {
		t.Errorf("错误没说清是密钥问题：%v", err)
	}
}

// 有的服务商压根没实现 /models（404），也有的返回一段 HTML。
// 两种都要是「错误」而不是「空列表」——调用方靠 err 决定要不要退回手填，
// 静悄悄给个空列表的话，界面会显示成「这家一个模型都没有」。
func TestModelsFailsLoudly(t *testing.T) {
	for name, tr := range map[string]*modelsTransport{
		"没这个接口":   {status: 404, body: `not found`},
		"不是 JSON": {body: `<html>登录一下</html>`},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := modelsClient(tr).Models(context.Background())
			if err == nil {
				t.Fatalf("该报错却给了 %d 个模型", len(got))
			}
		})
	}
}

// Verify 和 Models 走同一次请求。分成两趟的话，设置页里「测试连接」
// 和「拉模型列表」就是两趟网络，而它们问的是同一个问题。
func TestVerifySharesTheModelsRequest(t *testing.T) {
	tr := &modelsTransport{body: `{"data":[{"id":"x"}]}`}
	if err := modelsClient(tr).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tr.url, "/models") {
		t.Errorf("Verify 没去 /models：%s", tr.url)
	}
	// Verify 只看通不通，所以内容解不开也不该算失败——
	// 这是它跟 Models 唯一的分别。
	tr2 := &modelsTransport{body: `<html>行吧</html>`}
	if err := modelsClient(tr2).Verify(context.Background()); err != nil {
		t.Errorf("状态码是 200，Verify 该算通：%v", err)
	}
}
