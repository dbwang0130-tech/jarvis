package orgtools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
)

// 复选框那一摊：org 那边早有 ToggleCheckbox，但一直没有工具接出来，
// 于是「第二项做完了」这句话谁也办不了——模型没有工具可调。
//
// 这几条钉的是接出来之后的行为：勾了要落盘、标题上的 [n/m] 要跟着变、
// 说错了数要有句人话。

const boxSample = `#+TITLE: 测试

* TODO 出门前检查 [1/3]
  - [X] 关窗
  - [ ] 带钥匙
  - [ ] 倒垃圾
* TODO 没有清单的事
`

func newBoxWS(t *testing.T) (*Workspace, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "box.org")
	if err := os.WriteFile(path, []byte(boxSample), 0o644); err != nil {
		t.Fatal(err)
	}
	coll, err := org.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewWorkspace(coll, config.Config{WorkHours: config.WorkHours{Start: 9, End: 22}}), path
}

func TestToggleCheckbox(t *testing.T) {
	ws, path := newBoxWS(t)

	msg := run(t, ws, "toggle_checkbox", map[string]any{
		"ref": refTo(t, ws, "出门前检查"), "index": 2,
	})
	if !strings.Contains(msg, "带钥匙") {
		t.Fatalf("没说清勾的是哪一项：%s", msg)
	}

	got := readBack(t, path)
	if !strings.Contains(got, "- [X] 带钥匙") {
		t.Fatalf("第二项没被勾上：\n%s", got)
	}
	// 标题上的进度得跟着走。不同步的话，列表上还显示 1/3，
	// 而正文里已经勾了两个——两处各说各的。
	if !strings.Contains(got, "[2/3]") {
		t.Fatalf("标题上的进度没跟着变：\n%s", got)
	}
	// 别的行不许动。
	if !strings.Contains(got, "- [X] 关窗") || !strings.Contains(got, "- [ ] 倒垃圾") {
		t.Fatalf("动了不该动的行：\n%s", got)
	}
}

// 再点一次要能取消。只能勾不能取消的话，点错了就没救了。
func TestToggleCheckboxTwiceComesBack(t *testing.T) {
	ws, path := newBoxWS(t)
	before := readBack(t, path)

	run(t, ws, "toggle_checkbox", map[string]any{
		"ref": refTo(t, ws, "出门前检查"), "index": 1,
	})
	msg := run(t, ws, "toggle_checkbox", map[string]any{
		"ref": refTo(t, ws, "出门前检查"), "index": 1,
	})
	if !strings.Contains(msg, "勾上了") {
		t.Fatalf("取消之后再点该是「勾上了」：%s", msg)
	}
	if got := readBack(t, path); got != before {
		t.Fatalf("来回各一次没回到原样：\n%s\n---\n%s", before, got)
	}
}

// 说错了数、或者那条根本没有清单，得有句听得懂的话，而不是下标越界。
func TestToggleCheckboxBadIndex(t *testing.T) {
	ws, _ := newBoxWS(t)

	for _, tc := range []struct {
		name  string
		title string
		index int
		want  string
	}{
		{"超出范围", "出门前检查", 9, "只有 3 个"},
		{"从零数起", "出门前检查", 0, "从 1 数起"},
		{"根本没有清单", "没有清单的事", 1, "没有复选框"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"ref": refTo(t, ws, tc.title), "index": tc.index,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = toolByName(t, ws, "toggle_checkbox").Run(string(raw))
			if err == nil {
				t.Fatal("这种情况该报错")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("话没说到点上：%v", err)
			}
		})
	}
}
