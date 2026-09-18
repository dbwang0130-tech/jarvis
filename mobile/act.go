package mobile

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
)

// Act 直接对一个条目做一件事，不经过模型。
//
// 「标完成」这种事让模型跑一趟要好几秒、一次 API 调用，而它做的无非是查 ref
// 再调一个工具——界面上明明已经握着 ref 了。常用动作走这条路，点一下就完事；
// 需要判断的才交给 Ask。
//
// 用的是跟秘书同一套 orgtools 工具，不是另写一遍写回逻辑——两套迟早走散。
type ActInput struct {
	Files map[string]string `json:"files"`
	Ref   string            `json:"ref"`
	// Op 是要做的事：done / start / stop / postpone / cancel / undone /
	// create / check / priority / state。
	Op string `json:"op"`
	// Days 给 postpone 用，推迟几天，零值按 1 天算。
	Days int `json:"days"`
	// Index 给 check 用：勾正文里的第几个复选框，从 1 数起。
	// 跟工具那边对外的说法一致——界面和用户说的都是「第二项」。
	Index int `json:"index"`
	// State 给 state 用：改成哪个 TODO 关键字（TODO / NEXT / WAITING…）。
	State string `json:"state"`

	// 下面几项给 create 用。
	Title     string `json:"title"`
	Scheduled string `json:"scheduled"` // 2006-01-02 或 2006-01-02 15:04
	Priority  string `json:"priority"`
	File      string `json:"file"` // 写进哪个文件，留空写收件箱

	TZ string `json:"tz"`
}

// ActOutput 里的 Files 只含真变过的，跟 Ask 一个路子。
type ActOutput struct {
	Message string            `json:"message"`
	Files   map[string]string `json:"files,omitempty"`
	Error   string            `json:"error,omitempty"`
}

func (o *ActOutput) normalize() {}

// Act 执行一个动作，返回做了什么和要写回的文件。
func Act(inputJSON string) string {
	var in ActInput
	if err := json.Unmarshal([]byte(inputJSON), &in); err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("入参不是合法 JSON：%v", err)})
	}
	SetTimeZone(in.TZ)

	wd, err := newWorkdir(in.Files, "jarvis-act-")
	if err != nil {
		return marshal(&ActOutput{Error: err.Error()})
	}
	defer wd.close()
	orgDir := wd.orgDir

	coll, err := org.Load(orgDir)
	if err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("日程读不出来：%v", err)})
	}
	cfg := config.Config{
		Files:     []string{orgDir},
		Inbox:     wd.inboxPath(),
		WorkHours: config.WorkHours{Start: 9, End: 22},
	}
	ws := orgtools.NewWorkspace(coll, cfg)
	tools := toolsByName(orgtools.NewSkill(ws))

	msg, err := runOp(tools, in)
	if err != nil {
		return marshal(&ActOutput{Error: err.Error()})
	}

	out := ActOutput{Message: msg}
	out.Files, err = wd.changed()
	if err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("读回改动失败：%v", err)})
	}
	return marshal(&out)
}

func toolsByName(s agent.Skill) map[string]agent.Tool {
	m := map[string]agent.Tool{}
	for _, t := range s.Tools() {
		m[t.Spec().Name] = t
	}
	return m
}

func runOp(tools map[string]agent.Tool, in ActInput) (string, error) {
	call := func(name string, args map[string]any) (string, error) {
		t, ok := tools[name]
		if !ok {
			return "", fmt.Errorf("没有 %s 这个动作", name)
		}
		raw, _ := json.Marshal(args)
		out, _, err := t.Run(string(raw))
		return out, err
	}
	ref := map[string]any{"ref": in.Ref}

	switch in.Op {
	case "create":
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return "", fmt.Errorf("没说要记什么")
		}
		args := map[string]any{"title": title}
		if in.Scheduled != "" {
			args["scheduled"] = in.Scheduled
		}
		if in.Priority != "" {
			args["priority"] = in.Priority
		}
		if in.File != "" {
			args["file"] = in.File
		}
		return call("create_item", args)

	case "done":
		// 还在计时就先收尾，不然那条 CLOCK 一直挂着。收不掉不算事。
		_, _ = call("clock_out", ref)
		return call("set_todo_state", map[string]any{"ref": in.Ref, "state": "DONE"})

	case "undone":
		return call("set_todo_state", map[string]any{"ref": in.Ref, "state": "TODO"})

	case "start":
		// NEXT 不是每个文件都有，没有就算了，计时照样开始。
		msg, err := call("set_todo_state", map[string]any{"ref": in.Ref, "state": "NEXT"})
		clocked, cerr := call("clock_in", ref)
		if cerr != nil {
			if err != nil {
				return "", cerr
			}
			return msg, nil
		}
		return clocked, nil

	case "stop":
		return call("clock_out", ref)

	case "priority":
		// 详情页把优先级摆出来了却改不动——「这事更急了」只能开口说一句。
		// 留空是去掉优先级，所以不校验非空。
		return call("set_priority",
			map[string]any{"ref": in.Ref, "priority": in.Priority})

	case "state":
		// 状态同理。done/undone 只够表示做没做完，而 org 里的状态是每份
		// 文件自己定的（`#+TODO:`）——「等回复」「搁置」这些，界面显示得出来，
		// 却只能靠对话去改。
		if strings.TrimSpace(in.State) == "" {
			return "", fmt.Errorf("没说要改成什么状态")
		}
		return call("set_todo_state",
			map[string]any{"ref": in.Ref, "state": in.State})

	case "check":
		// 勾一个复选框。org 那边一直有这个能力，但工具没接出来，于是
		// 手机上那几个框画得出来却点不动——看着像坏了。
		return call("toggle_checkbox",
			map[string]any{"ref": in.Ref, "index": in.Index})

	case "cancel":
		// 有取消态就用取消态；没有就标完成并写明是取消的——总好过删掉。
		if msg, err := call("set_todo_state",
			map[string]any{"ref": in.Ref, "state": "CANCELLED"}); err == nil {
			return msg, nil
		}
		msg, err := call("set_todo_state", map[string]any{"ref": in.Ref, "state": "DONE"})
		if err != nil {
			return "", err
		}
		// 参数叫 text。写错名字的话工具会报「笔记内容不能为空」，而这里
		// 又把错误吞了——标成完成了，说明却没记上，事后完全看不出区别。
		if _, err := call("add_note",
			map[string]any{"ref": in.Ref, "text": "取消，不做了"}); err != nil {
			return msg + "（这个文件没有取消态，标成了完成；说明没记上：" +
				err.Error() + "）", nil
		}
		return msg + "（这个文件没有取消态，标成了完成并记了一笔）", nil

	case "postpone":
		days := in.Days
		if days == 0 {
			days = 1
		}
		when := time.Now().AddDate(0, 0, days).Format("2006-01-02")
		return call("schedule_item", map[string]any{"ref": in.Ref, "time": when})

	default:
		return "", fmt.Errorf("不认识的动作 %q", in.Op)
	}
}
