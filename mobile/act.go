package mobile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	// Op 是要做的事：done / start / stop / postpone / cancel / undone / create。
	Op string `json:"op"`
	// Days 给 postpone 用，推迟几天，零值按 1 天算。
	Days int `json:"days"`

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
	applyTimeZone(in.TZ)

	dir, err := os.MkdirTemp("", "jarvis-act-")
	if err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("建不了临时目录：%v", err)})
	}
	defer os.RemoveAll(dir)

	orgDir := filepath.Join(dir, "org")
	if err := os.MkdirAll(orgDir, 0o700); err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("建不了临时目录：%v", err)})
	}
	for name, content := range in.Files {
		p := filepath.Join(orgDir, filepath.Base(name))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			return marshal(&ActOutput{Error: fmt.Sprintf("写临时文件失败：%v", err)})
		}
	}

	coll, err := org.Load(orgDir)
	if err != nil {
		return marshal(&ActOutput{Error: fmt.Sprintf("日程读不出来：%v", err)})
	}
	cfg := config.Config{
		Files:     []string{orgDir},
		Inbox:     filepath.Join(orgDir, defaultInbox(in.Files)),
		WorkHours: config.WorkHours{Start: 9, End: 22},
	}
	ws := orgtools.NewWorkspace(coll, cfg)
	tools := toolsByName(orgtools.NewSkill(ws))

	msg, err := runOp(tools, in)
	if err != nil {
		return marshal(&ActOutput{Error: err.Error()})
	}

	out := ActOutput{Message: msg}
	out.Files, err = changedFiles(orgDir, in.Files)
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
		_, _ = call("add_note", map[string]any{"ref": in.Ref, "note": "取消，不做了"})
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
