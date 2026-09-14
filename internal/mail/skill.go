package mail

import (
	"fmt"
	"strings"

	"github.com/dabao/jarvis/internal/agent"
)

// Skill 把邮箱能力作为秘书的一项技能接入。
type Skill struct {
	Sess *Session
}

// NewSkill 构造邮件技能。
func NewSkill(sess *Session) *Skill { return &Skill{Sess: sess} }

func (s *Skill) Name() string { return "邮件" }

// Describe 描述当前环境：接入了哪些邮箱。
func (s *Skill) Describe(b *strings.Builder) {
	if !s.Sess.HasAccounts() {
		b.WriteString("尚未接入邮箱（用户可在界面里用 /mail 添加）。\n")
		return
	}
	var names []string
	for _, acc := range s.Sess.cfg.Enabled() {
		names = append(names, fmt.Sprintf("%s（%s）", acc.Name, acc.User))
	}
	b.WriteString("已接入邮箱：" + strings.Join(names, "、") + "\n")
}

// Instructions 是邮件场景的行为规则。
func (s *Skill) Instructions() string {
	return `邮箱管理：
1. 收件箱拉下来后，先用 triage 一次性给每封邮件打标签，然后写中文简报。
2. 简报开头一行总览：总共几封、几封要紧、几封可清理；「要紧」逐条写编号、谁发的、什么事、建议怎么处理；「关注」合并成几行；广告和垃圾只报数量，末尾主动问一句要不要清掉。
3. 从邮件里挑出时间点——会议、截止日期、约定、待办——集中成「日程提醒」一节。如果某件事值得放进日程，用 create_item 或 schedule_item 写进 org 文件，不要只是嘴上提醒。
4. 代笔：用户要回信或写信时，用 draft_reply / draft_new 起草完整正文——必须有称呼、正文、结尾，不留占位符。
5. 拿不准一封邮件是不是广告时判成 normal 留着，别误清；判断"要紧"的依据是用户的处境，不是邮件自己的语气。`
}

// Tools 返回邮件技能的全部工具（绑定到当前会话）。
func (s *Skill) Tools() []agent.Tool {
	m := Tools()
	out := make([]agent.Tool, 0, len(m))
	for _, name := range SortedNames(m) {
		t := m[name]
		t.sess = s.Sess
		out = append(out, t)
	}
	return out
}
