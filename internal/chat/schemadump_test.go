package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
)

const goldenTools = "testdata/tools.json"

// 全部工具声明的金标。
//
// **工具表是常驻开销**：每个工具的声明每一轮都重发一遍，描述写长一句、参数多列
// 一个，都是长期成本。所以让它的任何变化都在 diff 里看得见——改动是有意的就更新
// 金标文件，顺手也就看见了自己让声明长了多少字节。
//
// 它还兜住一类静默事故：`internal/agent/schema.go` 那几个辅助函数原先是每个技能包
// 各写一份的，已经漂出两处不一致（`num` 在两边分别生成 number 和 integer，
// `obj` 在 orgtools 那份会多发一个 `"required": []`）。改动这类共用代码时，
// 编译和别的测试都不会响，只有这儿会。
//
// 更新：go test ./internal/chat/ -run TestToolSchemas -update
func TestToolSchemas(t *testing.T) {
	skills := testSkills()

	type entry struct {
		Skill    string
		Name     string
		Risk     int
		ReadOnly bool
		Spec     agent.ToolSpec
	}
	var all []entry
	for _, sk := range skills {
		for _, tl := range sk.Tools() {
			ro := false
			if r, ok := tl.(agent.ReadOnlyTool); ok {
				ro = r.ReadOnly()
			}
			all = append(all, entry{sk.Name(), tl.Spec().Name, int(tl.Risk()), ro, tl.Spec()})
		}
	}
	got, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenTools), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenTools, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("更新了金标：%d 个工具，声明 %d 字节", len(all), len(got))
		return
	}

	want, err := os.ReadFile(goldenTools)
	if err != nil {
		t.Fatalf("读不到金标（第一次跑加 -update 生成）：%v", err)
	}
	if string(got) != string(want) {
		t.Errorf("工具声明跟金标不一样。改动是有意的就跑一次：\n"+
			"  go test ./internal/chat/ -run TestToolSchemas -update\n"+
			"然后在 diff 里看清自己改了什么、声明长了多少（现在 %d 字节，金标 %d）",
			len(got), len(want))
	}
}
