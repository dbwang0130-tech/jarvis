package mobile

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
	// create / check / priority / state / schedule / deadline / note /
	// delete / today。
	Op string `json:"op"`
	// Days 给 postpone 用，推迟几天，零值按 1 天算。
	Days int `json:"days"`
	// Refs 是一次办好几条时的目标清单。给了它就不看 Ref。
	//
	// 为什么要有批量：一堆逾期的事要一起挪到今天，一条条来就是一条条
	// 读盘写盘——十条就是十趟，每趟都要把全部 org 文件解析一遍。
	Refs []string `json:"refs"`
	// Index 给 check 用：勾正文里的第几个复选框，从 1 数起。
	// 跟工具那边对外的说法一致——界面和用户说的都是「第二项」。
	Index int `json:"index"`
	// State 给 state 用：改成哪个 TODO 关键字（TODO / NEXT / WAITING…）。
	State string `json:"state"`
	// Text 给 note 用：往 LOGBOOK 里记的那句话。
	//
	// 不复用 Title：那是 create 用来当标题的。两件事挤一个字段，哪天想在
	// 一次调用里同时给标题和笔记就没地方放了。
	Text string `json:"text"`

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

	var msg string
	if len(in.Refs) > 0 {
		msg, err = runBatch(tools, in)
	} else {
		msg, err = runOp(tools, in)
	}
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

// runBatch 对一串条目做同一件事。
//
// **从后往前办。** ref 是「文件:行号」，而有些动作会让行数变——
// 给一条没排过期的事加 SCHEDULED，它下面所有条目的行号都往后挪一行。
// 从前往后办的话，第二条开始的 ref 就全指错了（轻则报「引用已失效」，
// 重则改到隔壁那条头上）。从行号大的开始，改动只影响它后面，而后面的
// 已经办完了。
//
// 一条失败不影响其余：一起挪十条，其中一条被别处删了，不该让另外九条
// 也白挪。最后把办成几条、哪几条没成一起报出来。
func runBatch(tools map[string]agent.Tool, in ActInput) (string, error) {
	refs := append([]string(nil), in.Refs...)
	sort.Slice(refs, func(i, j int) bool {
		fi, li := splitRefLine(refs[i])
		fj, lj := splitRefLine(refs[j])
		if fi != fj {
			return fi < fj
		}
		return li > lj // 同一个文件里，行号大的先办
	})

	var done int
	var fails []string
	for _, ref := range refs {
		one := in
		one.Ref = ref
		one.Refs = nil
		if _, err := runOp(tools, one); err != nil {
			fails = append(fails, err.Error())
			continue
		}
		done++
	}

	switch {
	case done == 0 && len(fails) > 0:
		return "", fmt.Errorf("一条都没办成：%s", strings.Join(fails, "；"))
	case len(fails) > 0:
		return fmt.Sprintf("办好了 %d 条，%d 条没成：%s",
			done, len(fails), strings.Join(fails, "；")), nil
	default:
		return fmt.Sprintf("办好了 %d 条", done), nil
	}
}

// splitRefLine 把 "work.org:12" 拆成文件名和行号，拆不开就当行号 0。
func splitRefLine(ref string) (string, int) {
	i := strings.LastIndex(ref, ":")
	if i < 0 {
		return ref, 0
	}
	n, err := strconv.Atoi(ref[i+1:])
	if err != nil {
		return ref, 0
	}
	return ref[:i], n
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
	// upd 改这条条目的某一项。**把键放进去本身就是「要改这一项」**，
	// 所以值为空串是「清掉它」（取消排期、去掉优先级），不是「别动」。
	// 下面 create 那支正相反：不想设的字段压根不放进 map——没提才用默认。
	// 两边合起来正好是 update_item / create_item 共用的那套约定。
	upd := func(k string, v any) (string, error) {
		return call("update_item", map[string]any{"ref": in.Ref, k: v})
	}

	switch in.Op {
	case "create":
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return "", fmt.Errorf("没说要记什么")
		}
		// 不想设的字段压根不放进去——**没提**才落默认值（状态默认 TODO）。
		// 放个空串进去是另一个意思：建一条不带那一项的条目。
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
		return upd("state", "DONE")

	case "undone":
		return upd("state", "TODO")

	case "start":
		// NEXT 不是每个文件都有，没有就算了，计时照样开始。
		msg, err := upd("state", "NEXT")
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

	case "delete":
		// **真的删掉**，不是标记完成。界面那边为它挂了确认框。
		//
		// 「做完了」应该标完成而不是删除（提示词里也这么跟秘书说），但
		// 「记错了」「这条本来就不该在这儿」没有别的出路——以前只能到
		// 电脑上去改文件。
		//
		// 要带标题：delete_item 拿它跟 ref 指着的那条核对，对不上就拒绝。
		// 这道保险是防模型删错的，界面这边本来就握着标题，顺手给上。
		if strings.TrimSpace(in.Title) == "" {
			return "", fmt.Errorf("删除要带上标题，好核对删的是哪一条")
		}
		return call("delete_item",
			map[string]any{"ref": in.Ref, "title": in.Title})

	case "note":
		// 详情页把 LOGBOOK 里的记录都列出来了（「做过什么」那一节），却没有
		// 地方往里添一笔——想记「卡在数据那段，问了老张才通」只能开口说。
		if strings.TrimSpace(in.Text) == "" {
			return "", fmt.Errorf("没说要记什么")
		}
		return call("add_note", map[string]any{"ref": in.Ref, "text": in.Text})

	case "schedule", "deadline":
		// 排期和截止一直只能靠 postpone 往后挪整天——挪到「下周三下午三点」
		// 或者干脆取消排期，都只能开口说一句。两个动作共用 Scheduled 字段
		// 传时间，留空是取消。
		key := "scheduled"
		if in.Op == "deadline" {
			key = "deadline"
		}
		return upd(key, in.Scheduled)

	case "priority":
		// 详情页把优先级摆出来了却改不动——「这事更急了」只能开口说一句。
		// 留空是去掉优先级，所以不校验非空。
		return upd("priority", in.Priority)

	case "state":
		// 状态同理。done/undone 只够表示做没做完，而 org 里的状态是每份
		// 文件自己定的（`#+TODO:`）——「等回复」「搁置」这些，界面显示得出来，
		// 却只能靠对话去改。
		if strings.TrimSpace(in.State) == "" {
			return "", fmt.Errorf("没说要改成什么状态")
		}
		return upd("state", in.State)

	case "check":
		// 勾一个复选框。org 那边一直有这个能力，但工具没接出来，于是
		// 手机上那几个框画得出来却点不动——看着像坏了。
		return call("toggle_checkbox",
			map[string]any{"ref": in.Ref, "index": in.Index})

	case "cancel":
		// 有取消态就用取消态；没有就标完成并写明是取消的——总好过删掉。
		if msg, err := upd("state", "CANCELLED"); err == nil {
			return msg, nil
		}
		msg, err := upd("state", "DONE")
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

	case "today":
		// 挪到今天。不能靠 postpone 传 days=0 来表达——那边零值按 1 天算
		//（「推迟」默认就是推一天），再给 0 一个特殊含义就成了魔法值。
		return upd("scheduled", time.Now().Format("2006-01-02"))

	case "postpone":
		days := in.Days
		if days == 0 {
			days = 1
		}
		when := time.Now().AddDate(0, 0, days).Format("2006-01-02")
		return upd("scheduled", when)

	default:
		return "", fmt.Errorf("不认识的动作 %q", in.Op)
	}
}
