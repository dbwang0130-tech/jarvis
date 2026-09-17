package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
)

// TestModelInput 只要模型那几项，不碰文件也不碰日程。
type TestModelInput struct {
	Model ModelConfig `json:"model"`
}

// TestModelOutput 说得清楚点：成没成、对面是谁、多久回的。
type TestModelOutput struct {
	OK      bool   `json:"ok"`
	Model   string `json:"model"`
	Reply   string `json:"reply"`
	Millis  int    `json:"millis"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message"`
}

func (o *TestModelOutput) normalize() {}

// TestModel 拿当前配置真发一次请求，看通不通。
//
// 不做这个的话，密钥填错要等到第一次认真提问才暴露——那会儿人已经打完
// 一整句话、等了十几秒，才被告诉「密钥不对」。设置页里点一下就知道。
func TestModel(inputJSON string) string {
	var in TestModelInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&TestModelOutput{
			Error:   fmt.Sprintf("入参不是合法 JSON：%v", err),
			Message: "入参不对",
		})
	}
	if strings.TrimSpace(in.Model.Provider) == "" {
		return marshal(&TestModelOutput{Error: "没选服务商", Message: "没选服务商"})
	}
	if strings.TrimSpace(in.Model.APIKey) == "" {
		return marshal(&TestModelOutput{Error: "没填密钥", Message: "没填密钥"})
	}

	cfg := config.Config{Agent: config.Agent{
		Provider:   in.Model.Provider,
		Model:      in.Model.Model,
		BaseURL:    in.Model.BaseURL,
		APIKey:     in.Model.APIKey,
		MaxSteps:   1,
		TimeoutSec: orDefault(in.Model.TimeoutSec, 20),
		Proxy:      "none", // 手机上别去读环境变量里的代理
	}}
	ag, err := agent.Build(cfg)
	if err != nil {
		return marshal(&TestModelOutput{
			Error: err.Error(), Message: "配置不完整：" + err.Error()})
	}
	ag.Stream = false

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(orDefault(in.Model.TimeoutSec, 20))*time.Second)
	defer cancel()

	// 问一句最短的话。要的是「连得上、密钥对、模型名没写错」这三件事，
	// 不是答案本身，所以让它只回一个字，别烧 token。
	reply, err := ag.Ask(ctx, "回复一个字：好", nil)
	ms := int(time.Since(start).Milliseconds())
	if err != nil {
		return marshal(&TestModelOutput{
			Millis: ms, Error: err.Error(), Message: explainModelError(err.Error())})
	}
	return marshal(&TestModelOutput{
		OK: true, Model: cfg.Agent.Model, Reply: strings.TrimSpace(reply), Millis: ms,
		Message: fmt.Sprintf("通了，%d 毫秒", ms)})
}

// explainModelError 把服务商返回的英文错误翻成能照着处理的话。
// 原样把 401 之类甩给用户，他也不知道该改哪一项。
func explainModelError(raw string) string {
	l := strings.ToLower(raw)
	switch {
	case strings.Contains(l, "401"), strings.Contains(l, "unauthorized"),
		strings.Contains(l, "invalid api key"), strings.Contains(l, "authentication"):
		return "密钥不对，检查一下有没有复制全"
	case strings.Contains(l, "402"), strings.Contains(l, "insufficient"),
		strings.Contains(l, "balance"), strings.Contains(l, "quota"):
		return "账户余额或额度不够了"
	case strings.Contains(l, "404"), strings.Contains(l, "model_not_found"),
		strings.Contains(l, "does not exist"):
		return "模型名不对，或者这个服务商没有这个模型"
	case strings.Contains(l, "429"), strings.Contains(l, "rate limit"):
		return "被限流了，等一会儿再试"
	case strings.Contains(l, "timeout"), strings.Contains(l, "deadline exceeded"):
		return "超时了，网络不通或者接口地址不对"
	case strings.Contains(l, "no such host"), strings.Contains(l, "connection refused"),
		strings.Contains(l, "network is unreachable"):
		return "连不上，检查接口地址和网络"
	default:
		return raw
	}
}
