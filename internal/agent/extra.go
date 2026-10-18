package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ExtraSkill 是一份由调用方从外部读来的扩展技能。
//
// agent 只认这三个字段，不关心它从哪儿来：桌面端扫 iCloud 的 skills/ 目录，
// 其他客户端从桥那边送进来。跟人设一样，读文件这件事留在各自那一端——手机上
// Go 一个文件都不碰。
//
// 它**不带工具**。一份 markdown 能贡献的是「这件事该怎么办」，动作还是骑在
// 现有工具上跑。想加新动作得写 Go，实现 Skill 接口。
type ExtraSkill struct {
	Name        string
	Description string
	Body        string
}

// SetExtraSkills 换掉扩展技能，并重建工具表（没有扩展技能就不注入 skill 工具）。
func (a *Agent) SetExtraSkills(list []ExtraSkill) {
	a.Extra = list
	a.tools = a.flattenTools()
}

// describeExtra 往系统提示里写扩展技能的目录。
//
// **只写名字和一句话，不写正文。** 正文由模型调 skill 读。全都常驻的话，
// 用户丢十份进来就是每一轮白发几万字节，而其中九份跟这一轮无关——工具表
// 是常驻开销，提示里的正文更是。
func (a *Agent) describeExtra(b *strings.Builder) {
	if len(a.Extra) == 0 {
		return
	}
	b.WriteString("【按需展开的技能】\n")
	b.WriteString("下面这些只给了名字和用途，正文没在这儿。挑到对得上的，先调 skill 把它读进来再照着办：\n")
	for _, s := range a.Extra {
		fmt.Fprintf(b, "- %s：%s\n", s.Name, s.Description)
	}
	b.WriteString("对不上就别硬套，按你原本的办法做。别为了用它而用它。\n\n")
}

// skillTool 把某一份扩展技能的正文读进上下文。
type skillTool struct{ a *Agent }

func (t skillTool) Spec() ToolSpec {
	return ToolSpec{
		Name: "skill",
		Description: "读一份「按需展开的技能」的正文。系统提示里列了有哪些、各管什么；" +
			"要照某一份办事之前先调它把内容读进来。",
		Parameters: Obj(map[string]any{
			"name": Str("技能名，照系统提示里列的那个写"),
		}, "name"),
	}
}

func (t skillTool) Risk() Risk { return RiskNone }

func (t skillTool) Preview(string) (string, string, error) { return "", "", nil }

// ReadOnly 让它能跟别的查询并发：读一份说明不改任何东西。
//
// 这个标记是按批生效的——漏了它，同一轮里本该并发的日程和邮件查询会被
// 一起拉回串行。
func (t skillTool) ReadOnly() bool { return true }

func (t skillTool) Run(raw string) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", err
	}
	want := strings.TrimSpace(a.Name)
	if want == "" {
		return "", fmt.Errorf("要读哪一份？name 不能为空")
	}
	// 先精确后前缀，都不区分大小写：模型常把名字抄短一截或大小写记错，
	// 为这个白费一个来回不值得。
	for _, s := range t.a.Extra {
		if strings.EqualFold(s.Name, want) {
			return s.Body, nil
		}
	}
	var hit []ExtraSkill
	for _, s := range t.a.Extra {
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(want)) {
			hit = append(hit, s)
		}
	}
	if len(hit) == 1 {
		return hit[0].Body, nil
	}
	// 没找到就把有哪些一起说了，省一个来回。
	names := make([]string, 0, len(t.a.Extra))
	for _, s := range t.a.Extra {
		names = append(names, s.Name)
	}
	if len(names) == 0 {
		return "", fmt.Errorf("没有任何扩展技能")
	}
	return "", fmt.Errorf("没有叫「%s」的技能。现有这些：%s",
		want, strings.Join(names, "、"))
}
