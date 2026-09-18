package mobile

import (
	"encoding/json"
	"strings"
	"testing"
)

func act(t *testing.T, in ActInput) ActOutput {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out ActOutput
	if err := json.Unmarshal([]byte(Act(string(raw))), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	return out
}

const actOrg = `* TODO 写周报
  SCHEDULED: <2026-09-17 四 10:00>
* TODO 改简历
`

// 常用动作不该走模型：界面上已经握着 ref 了，让模型再查一遍要好几秒和一次
// API 调用。这条确认 Act 真的改了文件内容，而且只交出变过的那个。
func TestActDone(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": actOrg, "b.org": "* TODO 别动我\n"},
		Ref:   "a.org:0", Op: "done", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if !strings.Contains(out.Files["a.org"], "DONE 写周报") {
		t.Errorf("没标成完成：\n%s", out.Files["a.org"])
	}
	if _, ok := out.Files["b.org"]; ok {
		t.Error("没动过的文件也交出来了")
	}
}

func TestActStartThenStop(t *testing.T) {
	files := map[string]string{"a.org": actOrg}
	started := act(t, ActInput{Files: files, Ref: "a.org:0", Op: "start"})
	if started.Error != "" {
		t.Fatalf("开始失败：%s", started.Error)
	}
	if !strings.Contains(started.Files["a.org"], "CLOCK:") {
		t.Fatalf("没开始计时：\n%s", started.Files["a.org"])
	}

	// 拿改完的内容接着停——界面就是这么一轮一轮传的。
	stopped := act(t, ActInput{Files: started.Files, Ref: "a.org:0", Op: "stop"})
	if stopped.Error != "" {
		t.Fatalf("停止失败：%s", stopped.Error)
	}
	if !strings.Contains(stopped.Files["a.org"], "=>") {
		t.Errorf("计时没收尾（没有时长）：\n%s", stopped.Files["a.org"])
	}
}

func TestActPostpone(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": actOrg},
		Ref:   "a.org:0", Op: "postpone", Days: 3, TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	if strings.Contains(out.Files["a.org"], "2026-09-17 四 10:00") {
		t.Errorf("时间没改：\n%s", out.Files["a.org"])
	}
	if !strings.Contains(out.Files["a.org"], "SCHEDULED:") {
		t.Errorf("计划时间丢了：\n%s", out.Files["a.org"])
	}
}

// 没有取消态的文件不能因此失败，更不能去删——标完成并记一笔。
func TestActCancelFallsBack(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": actOrg}, Ref: "a.org:0", Op: "cancel",
	})
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	body := out.Files["a.org"]
	if !strings.Contains(body, "DONE 写周报") && !strings.Contains(body, "CANCELLED 写周报") {
		t.Errorf("既没取消也没完成：\n%s", body)
	}
	if !strings.Contains(body, "写周报") {
		t.Error("条目被删掉了")
	}
	// 退回标完成时必须记一笔说明是取消的，否则事后跟真做完的分不出来。
	// 之前 add_note 的参数名写错（note 而不是 text），错误又被吞了，
	// 标成完成了但说明一直没写上。
	if strings.Contains(body, "DONE") && !strings.Contains(body, "取消") {
		t.Errorf("标成完成却没记下是取消的：\n%s", body)
	}
}

// 烂输入要给话说，不能 panic、不能返回空壳。
func TestActBadInput(t *testing.T) {
	var out ActOutput
	if err := json.Unmarshal([]byte(Act("不是 JSON")), &out); err != nil {
		t.Fatalf("出参不是合法 JSON：%v", err)
	}
	if out.Error == "" {
		t.Error("该报错却没报")
	}

	for _, tc := range []ActInput{
		{Files: map[string]string{"a.org": actOrg}, Ref: "a.org:0", Op: "跳舞"},
		{Files: map[string]string{"a.org": actOrg}, Ref: "a.org:999", Op: "done"},
		{Files: map[string]string{"a.org": actOrg}, Ref: "", Op: "done"},
	} {
		got := act(t, tc)
		if got.Error == "" {
			t.Errorf("%+v 该报错却没报", tc)
		}
	}
}

// 「买牛奶」这种一句话的事，打字进对话比直接记一条还慢——得能直接新建。
func TestActCreate(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"inbox.org": "", "work.org": actOrg},
		Op:    "create", Title: "买牛奶", Scheduled: "2026-09-20",
		Priority: "B", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("不该出错：%s", out.Error)
	}
	body := out.Files["inbox.org"]
	if !strings.Contains(body, "买牛奶") {
		t.Fatalf("没记进收件箱：\n%v", out.Files)
	}
	if !strings.Contains(body, "2026-09-20") {
		t.Errorf("计划时间没写上：\n%s", body)
	}
	if !strings.Contains(body, "[#B]") {
		t.Errorf("优先级没写上：\n%s", body)
	}
	if _, ok := out.Files["work.org"]; ok {
		t.Error("没动过的文件也交出来了")
	}

	// 标题是空的要说清楚，不能默默建一条没名字的。
	if e := act(t, ActInput{Files: map[string]string{"inbox.org": ""},
		Op: "create", Title: "  "}); e.Error == "" {
		t.Error("空标题该报错")
	}
}

// 勾一个复选框。org 那边一直有这个能力，但一直没接出来——手机上那几个框
// 画得出来却点不动，看着像坏了。
func TestActCheck(t *testing.T) {
	const withBoxes = `* TODO 出门前检查 [1/3]
  - [X] 关窗
  - [ ] 带钥匙
  - [ ] 倒垃圾
`
	out := act(t, ActInput{
		Files: map[string]string{"a.org": withBoxes},
		Ref:   "a.org:0", Op: "check", Index: 2, TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("勾不上：%s", out.Error)
	}
	got := out.Files["a.org"]
	if !strings.Contains(got, "- [X] 带钥匙") {
		t.Fatalf("第二项没勾上：\n%s", got)
	}
	// 标题上的进度要跟着走，否则列表显示 1/3 而正文里勾了两个。
	if !strings.Contains(got, "[2/3]") {
		t.Fatalf("标题进度没同步：\n%s", got)
	}
}

// 说错了数不该让整个 app 出错，得给句人话。
func TestActCheckBadIndex(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": actOrg},
		Ref:   "a.org:0", Op: "check", Index: 1, TZ: "Asia/Shanghai",
	})
	if out.Error == "" {
		t.Fatal("这条根本没有复选框，该报错")
	}
	if !strings.Contains(out.Error, "复选框") {
		t.Fatalf("话没说到点上：%s", out.Error)
	}
}

// 详情页把状态和优先级摆出来了却改不动——「这事更急了」只能开口说一句。
func TestActPriorityAndState(t *testing.T) {
	const org = "* TODO 写周报\n"

	out := act(t, ActInput{
		Files: map[string]string{"a.org": org},
		Ref:   "a.org:0", Op: "priority", Priority: "A", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("改不了优先级：%s", out.Error)
	}
	if !strings.Contains(out.Files["a.org"], "[#A]") {
		t.Fatalf("优先级没写上：\n%s", out.Files["a.org"])
	}

	// 留空是去掉优先级，不是「没说」。
	out = act(t, ActInput{
		Files: map[string]string{"a.org": "* TODO [#A] 写周报\n"},
		Ref:   "a.org:0", Op: "priority", Priority: "", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("去不掉优先级：%s", out.Error)
	}
	if strings.Contains(out.Files["a.org"], "[#A]") {
		t.Fatalf("优先级还在：\n%s", out.Files["a.org"])
	}

	out = act(t, ActInput{
		Files: map[string]string{"a.org": org},
		Ref:   "a.org:0", Op: "state", State: "NEXT", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("改不了状态：%s", out.Error)
	}
	if !strings.Contains(out.Files["a.org"], "* NEXT 写周报") {
		t.Fatalf("状态没换：\n%s", out.Files["a.org"])
	}
}

// 状态不能不说就改。留空在优先级那边是「去掉」，在状态这边是「没说」——
// 两个意思，别混着来。
func TestActStateNeedsValue(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": actOrg},
		Ref:   "a.org:0", Op: "state", State: "", TZ: "Asia/Shanghai",
	})
	if out.Error == "" {
		t.Fatal("没说改成什么状态，该报错")
	}
}

// 排期和截止一直只能靠 postpone 往后挪整天——挪到具体某天某点、
// 或者干脆取消，都只能开口说一句。
func TestActScheduleAndDeadline(t *testing.T) {
	out := act(t, ActInput{
		Files: map[string]string{"a.org": "* TODO 写周报\n"},
		Ref:   "a.org:0", Op: "schedule", Scheduled: "2026-10-01 15:00",
		TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("排不上：%s", out.Error)
	}
	if !strings.Contains(out.Files["a.org"], "SCHEDULED: <2026-10-01") {
		t.Fatalf("计划时间没写上：\n%s", out.Files["a.org"])
	}

	out = act(t, ActInput{
		Files: map[string]string{"a.org": "* TODO 写周报\n"},
		Ref:   "a.org:0", Op: "deadline", Scheduled: "2026-10-05", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("设不上截止：%s", out.Error)
	}
	if !strings.Contains(out.Files["a.org"], "DEADLINE: <2026-10-05") {
		t.Fatalf("截止没写上：\n%s", out.Files["a.org"])
	}

	// 留空是取消，不是报错——「这事不用排期了」得有地方说。
	out = act(t, ActInput{
		Files: map[string]string{"a.org": "* TODO 写周报\n  SCHEDULED: <2026-10-01 三>\n"},
		Ref:   "a.org:0", Op: "schedule", Scheduled: "", TZ: "Asia/Shanghai",
	})
	if out.Error != "" {
		t.Fatalf("取消不了排期：%s", out.Error)
	}
	if strings.Contains(out.Files["a.org"], "SCHEDULED") {
		t.Fatalf("排期还在：\n%s", out.Files["a.org"])
	}
}
