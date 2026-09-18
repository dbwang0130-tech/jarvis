package mail

import (
	"strings"
	"testing"
)

// **邮件正文一个字都不能到模型那儿去。**
//
// 靠提示词说「别读正文」是不够的——工具摆在那儿，模型迟早会在某个它觉得
// 「实在拿不准」的时候调一次。这个测试守的是工具表本身：没有任何一个
// 工具的名字或描述沾正文。
//
// 曾经有过一个 read_mail，规则里还写着「拿不准的用它单独看那一封」。
// 已经删了。哪天有人手滑加回来，这儿会红。
func TestNoToolLeaksTheBody(t *testing.T) {
	tools := Tools()

	if _, ok := tools["read_mail"]; ok {
		t.Fatal("read_mail 又回来了——它把整封正文交给模型，正是不该有的那条路")
	}

	// 描述里也不该出现「正文」当成可取的东西。draft_reply 的 body 是
	// **模型写的回信**，不是读来的原文，那个不算。
	for name, tl := range tools {
		d := tl.Spec().Description
		if name == "draft_reply" || name == "draft_new" {
			continue
		}
		for _, bad := range []string{"完整正文", "读取", "原文"} {
			if strings.Contains(d, bad) {
				t.Errorf("%s 的描述里有 %q，像是能取到正文：%s", name, bad, d)
			}
		}
	}
}

// 行为规则里要**明说**看不到正文。
//
// 不说的话模型会以为是数据没加载好，转头去找别的路子拿；或者更糟——
// 装作看过，「我看了这封邮件，里面说……」，那是凭空编的。
func TestInstructionsSayTheBodyIsNotAvailable(t *testing.T) {
	ins := (&Skill{}).Instructions()
	for _, want := range []string{"看不到邮件正文", "没有任何工具能取到正文"} {
		if !strings.Contains(ins, want) {
			t.Errorf("行为规则里少了 %q", want)
		}
	}
}
