package mobile

import (
	"sort"

	"github.com/dabao/jarvis/internal/agent"
)

// Providers 交出预设服务商表，给 iOS 的设置界面填选项。
//
// 早先 Swift 那边把服务商名字硬编码成一个数组，注释写着「跟桌面端那份
// 预设表一致」——而它并不一致：桌面这边删掉 baichuan 之后，手机上那条
// 还在，选中了就是一个连不上的地址。这跟工具中文标签当年那份两处各写
// 各的表是同一个毛病，所以按同样的办法治：**只留一份，从 Go 这边过桥**。
//
// 带上 vision，界面就能直接标出哪个认得图片——手机是能发图、能拍照
// 记账的那一端，选了纯文本模型，图发出去石沉大海，最难查。
func Providers() string {
	type entry struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		BaseURL string `json:"base_url"`
		Vision  bool   `json:"vision"`
		Local   bool   `json:"local"`
	}
	out := struct {
		Providers []entry `json:"providers"`
		Error     string  `json:"error"`
	}{Providers: []entry{}}

	names := make([]string, 0, len(agent.Presets))
	for k := range agent.Presets {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		p := agent.Presets[n]
		out.Providers = append(out.Providers, entry{
			Name:    n,
			Model:   p.Model,
			BaseURL: p.BaseURL,
			Vision:  p.Vision,
			// 本地跑的那个不用填密钥，界面上可以把密钥框收起来。
			Local: p.KeyEnv == "",
		})
	}
	return marshal(&out)
}
