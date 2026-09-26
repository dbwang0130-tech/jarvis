package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultPersona 是内置人设，也是人设文件缺失、读不出来、或者被清空时的兜底。
//
// **它必须一直留在代码里。** 人设挪到 iCloud 之后多了一堆失手的方式：
// 文件还没同步下来、占位符没下载、被人编辑成空文件。这些都不该让秘书
// 变成一个没有身份的模型——那一轮它会用默认腔调称呼用户「您」、
// 开口讲一堆套话，用户看见的是「助理忽然换了个人」，而真正的原因在
// 文件系统那头，最难猜。跟 config.Load 出错时返回一份能直接用的默认
// 配置是同一条规矩：能降级就降级。
const DefaultPersona = `你是 Jarvis，用户的私人 AI 管家——就像钢铁侠身边的那一位。
性格：沉稳、干练、忠诚，带一点英式冷幽默。说话简洁，不奉承。
玩笑让位于准确——让人分不清你是真办了还是在说俏皮话，那就不好笑了。
称呼用户为「先生」。全程中文，短句为主，不说空话套话。
`

// SetPersona 装上这一轮的人设，以及改人设时往哪儿落盘。
//
// save 为 nil 表示这一端不让改人设，`persona` 那个工具就不注入——
// 注入了却存不下来更糟：模型会回一句「已经改好了」，下一轮人设原样，
// 而这正是最不该骗人的地方（见系统提示里「绝不说自己做了没做的事」）。
func (a *Agent) SetPersona(text string, save func(string) error) {
	a.Persona = text
	a.SavePersona = save
	// 工具表跟着人设的可写性变，所以要重建一次。
	a.tools = a.flattenTools()
}

// personaText 是这一轮真正写进系统提示的那段人设。
func (a *Agent) personaText() string {
	if s := strings.TrimSpace(a.Persona); s != "" {
		return s + "\n"
	}
	return DefaultPersona
}

// personaTool 读写人设。
//
// **一个工具兼读写**，靠 text 给没给来分：没给是读，给了是写。摊成
// get_persona / set_persona 两个的话，工具声明每一轮都多发一份，而读那
// 一路要的东西——当前人设——本来就摆在系统提示的第一段，模型手上就有。
// 这跟 update_item 五合一是同一笔账：工具表是常驻开销。
//
// 「没给」和「给了空串」是两回事，这条跟 orgtools/args.go 一个道理：
// 给了空串是「把自定义人设清掉、回到内置那套」，不是「我想看看」。
// 所以参数得是 *string，用 json 的 nil 区分——收成 string 的话两种
// 意思挤在同一个零值上，「恢复默认」就没法表达了。
type personaTool struct{ a *Agent }

func (t personaTool) Spec() ToolSpec {
	return ToolSpec{
		Name: "persona",
		Description: "读或改 Jarvis 自己的人设（性格、称呼、说话风格）。" +
			"不带 text 是读出当前人设；带 text 是整段覆盖；text 给空串是恢复内置人设。" +
			"用户说「以后正式一点」「别叫我先生」「说话短一些」这类要改你自己的话时用。",
		Parameters: Obj(map[string]any{
			"text": Str("新的人设全文，整段覆盖。只想读就别带这个参数"),
		}),
	}
}

func (t personaTool) Risk() Risk { return RiskNone }

func (t personaTool) Preview(string) (string, string, error) { return "", "", nil }

func (t personaTool) Run(raw string) (string, bool, error) {
	var a struct {
		Text *string `json:"text"`
	}
	// 读那一路模型常常连 {} 都不给，空参数不算错。
	if s := strings.TrimSpace(raw); s != "" && s != "{}" {
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			return "", false, err
		}
	}

	if a.Text == nil {
		// 读。说清是自定义的还是内置的——用户问「你的人设是什么」，
		// 「这是内置的默认人设」和「这是你上次让我改的」不是一回事。
		if strings.TrimSpace(t.a.Persona) == "" {
			return "当前用的是内置人设：\n\n" + DefaultPersona, false, nil
		}
		return "当前人设（自定义）：\n\n" + t.a.personaText(), false, nil
	}

	if t.a.SavePersona == nil {
		return "", false, fmt.Errorf("这一端不支持改人设")
	}

	old := t.a.personaText()
	text := strings.TrimSpace(*a.Text)
	if err := t.a.SavePersona(text); err != nil {
		return "", false, err
	}
	t.a.Persona = text

	// **把换下来的那份一起交出去。** 人设是整段覆盖，旧的那份写完就没了；
	// 放在工具结果里，用户说「算了还是刚才那样」时模型手上还有原文，
	// 不必让他自己回忆。这是这个动作之所以能算 RiskNone、不弹确认框的
	// 前提——代价低到可以直接做。
	if text == "" {
		return "已清掉自定义人设，回到内置那套：\n\n" + DefaultPersona +
			"\n换下来的是：\n\n" + old, false, nil
	}
	return "人设已更新为：\n\n" + text + "\n\n换下来的是：\n\n" + old, false, nil
}
