package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// ListModelsInput 只要模型那几项——服务商、接口地址、密钥。模型名本身
// 不用填，正是要问它有哪些。
type ListModelsInput struct {
	Model ModelConfig `json:"model"`
}

// ModelEntry 是列表里的一个模型。
type ModelEntry struct {
	// ID 是要填进配置的那个名字。
	ID string `json:"id"`
	// Name 是给人看的名字，可能跟 ID 一样（服务商没另给的话）。
	Name string `json:"name"`
	// Vision 表示它认得图片。手机这端会拍照记账，这个标记要显眼。
	Vision bool `json:"vision"`
	// Context 是上下文窗口，0 表示服务商没报，界面就别显示。
	Context int `json:"context"`
}

// ListModelsOutput 交出服务商自己报的模型表。
type ListModelsOutput struct {
	Models []ModelEntry `json:"models"`
	Error  string       `json:"error"`
	// Message 是给人看的一句话，出错时照着它处理。
	Message string `json:"message"`
}

// normalize 把 nil 切片补成空数组——Swift 那边 models 是非可选的，
// null 会让整个出参解码失败。见 CLAUDE.md 里那条契约。
func (o *ListModelsOutput) normalize() {
	if o.Models == nil {
		o.Models = []ModelEntry{}
	}
}

// ListModels 问服务商它都有哪些模型，给设置页填选项。
//
// **为的是让模型名从「自己打字」变成「挑一个」。** 打错一个字换来的是一个
// 看不懂的 404，而第一次配的人根本不知道该填什么；预设表里那个默认名也会
// 过时，届时同样是 404。现拉一份，挑就是了。
//
// 拿不到不是错：有的服务商没实现 /models。界面照旧留着手填那条路，
// 别把配置卡在这一步——这跟启动路径「能降级就降级」是同一条规矩。
func ListModels(inputJSON string) string {
	var in ListModelsInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&ListModelsOutput{
			Error:   fmt.Sprintf("入参不是合法 JSON：%v", err),
			Message: "入参不对",
		})
	}
	if strings.TrimSpace(in.Model.Provider) == "" {
		return marshal(&ListModelsOutput{Error: "没选服务商", Message: "先选一个服务商"})
	}

	secs := orDefault(in.Model.TimeoutSec, 20)
	client, err := agent.New(agent.Options{
		Provider: in.Model.Provider,
		Model:    in.Model.Model,
		BaseURL:  in.Model.BaseURL,
		APIKey:   in.Model.APIKey,
		Timeout:  time.Duration(secs) * time.Second,
		Proxy:    "none", // 手机上别去读环境变量里的代理
	})
	if err != nil {
		return marshal(&ListModelsOutput{Error: err.Error(), Message: err.Error()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
	defer cancel()
	list, err := client.Models(ctx)
	if err != nil {
		return marshal(&ListModelsOutput{
			Error: err.Error(), Message: explainModelError(err.Error())})
	}

	out := &ListModelsOutput{Models: make([]ModelEntry, 0, len(list))}
	for _, m := range list {
		name := strings.TrimSpace(m.Name)
		if name == "" {
			name = m.ID
		}
		out.Models = append(out.Models, ModelEntry{
			ID: m.ID, Name: name, Vision: m.Vision(), Context: m.Context,
		})
	}
	if len(out.Models) == 0 {
		out.Message = "这个服务商没报模型列表，自己填一个模型名吧"
	} else {
		out.Message = fmt.Sprintf("拿到 %d 个模型", len(out.Models))
	}
	return marshal(out)
}
