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
