package chat

import (
	"regexp"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

// 每个工具都得有中文标签，否则界面上直接露出英文工具名。
//
// 这条以前只靠 CLAUDE.md 里一句「新增工具别忘了补」，而表还有两份
// （桌面端一份、iOS 一份），谁也不守谁：iOS 那份留着早就删掉的 read_mail、
// 挂着五个根本不存在的工具，新加的健康那四个一个都没补。
// 现在表只有一份（agent/label.go），再加这个测试——注册了工具却忘了
// 登记标签，这儿就红。
func TestEveryToolHasALabel(t *testing.T) {
	skills := testSkills()
	// 内置的那两个不在任何 skill 里，单独点名。
	// 内置那两个是按条件注入的（人设可写才有 persona、有扩展技能才有 skill），
	// 不在技能清单里，单独点名。
	names := []string{"persona", "skill"}
	for _, sk := range skills {
		for _, tl := range sk.Tools() {
			names = append(names, tl.Spec().Name)
		}
	}
	if len(names) < 30 {
		t.Fatalf("只收集到 %d 个工具，skill 的构造大概出错了", len(names))
	}
	for _, n := range names {
		if agent.ToolLabel(n) == n {
			t.Errorf("工具 %q 没有中文标签，界面上会直接显示英文——"+
				"去 internal/agent/label.go 补一行", n)
		}
	}
}

// 能查东西的技能必须至少留一个标了只读的工具。
//
// `executeParallel` 要求**整批**工具调用都只读才并发，所以「只读」是按调用
// 批次生效的：一个技能把只读标记丢光，代价不止是它自己变慢——同一轮里
// 本来能并发的日程、邮件查询会被一起拖回串行。
//
// 这条以前只靠 CLAUDE.md 一句「新工具别忘了设 readonly」。浏览器技能做
// 九合五时，四个 readonly 工具全并进了 browser_exec_js，而 exec_js 能改
// DOM、标不了只读，于是整个技能的只读覆盖率归零——编译、测试全绿，
// 没有任何地方会响。
// **只有「记忆」是豁免的，写在下面那一行里。** 它只有 remember 一个纯写入工具，
// 没有「只读覆盖率」可言。豁免写成代码里的一个判断、而不是从清单里悄悄漏掉它：
// 漏掉的话，下一个人看到的是一份不知为何少一项的手抄清单，而看到这一行的人
// 知道为什么。哪天记忆有了查询工具，删掉这三行就是。
func TestEverySkillKeepsAReadOnlyTool(t *testing.T) {
	for _, sk := range testSkills() {
		if sk.Name() == "记忆" {
			continue
		}
		found := false
		for _, tl := range sk.Tools() {
			if ro, ok := tl.(interface{ ReadOnly() bool }); ok && ro.ReadOnly() {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("技能 %q 一个只读工具都没有——它的查询会把同一批里"+
				"别的只读调用一起拖成串行", sk.Name())
		}
	}
}

// 各技能的行为规则不许用方向指路（「上面那张表」）。
//
// 环境快照（余额表、未读数、最近一次体重）为了服务端的前缀缓存，整段挪到了
// 系统提示的**最末尾**——见 agent.systemPrompt。于是「上面那张表」当场成了
// 错话：那张表在下面。统一改成用名字指（「【当下】里那张账户表」），
// 再调顺序也不会说错。
//
// 放在 chat 这儿是因为只有这个包看得见全部真技能；agent 反向 import 不了它们。
// 跟 TestEveryToolHasALabel 同一个道理：跨技能的约定，只在一处守。
func TestNoSkillPointsUpwards(t *testing.T) {
	// **按规则拦，不按字面拦。** 原先这儿是四条写死的说法（「上面那张表」
	// 「上面给的今天」…），而新技能写「前面那张余额表」照样过——这条不变量的
	// 覆盖面等于当初写下那四句话的人的想象力。
	//
	// 现在的规则是绝对的：**方向词不许挨着「表 / 快照 / 清单」**。方向词本身没
	// 被禁（「写在金额后面」「下面这几种都是正常的」指的是紧跟着的同段内容，
	// 那是对的），禁的是拿方向去指那几份数据——它们在系统提示的最末尾，而且
	// 以后还可能再挪。指路一律用名字：「【当下】里那张账户表」。
	bad := regexp.MustCompile(`(上面|下面|前面|后面)[^。\n]{0,6}(表|快照|清单)`)
	for _, sk := range testSkills() {
		if m := bad.FindString(sk.Instructions()); m != "" {
			t.Errorf("%s 的规则里有 %q——环境快照在系统提示的最后，而且还会再挪；"+
				"指路请用名字（「【当下】里那张…」）", sk.Name(), m)
		}
	}
}
