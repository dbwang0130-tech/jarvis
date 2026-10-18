package orgtools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
)

// 父条目上的 [n/m] 要跟着子任务走：标完成、加一条、删一条，落盘的文件里都得是对的。
// 原先只有勾复选框会更新它，update_item 把子任务标成 DONE 之后父条目一直是 [0/2]。
func TestCookieFollowsSubtaskTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.org")
	if err := os.WriteFile(path, []byte("* TODO 搬家 [0/2]\n** TODO 打包\n** TODO 叫车\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	coll, err := org.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWorkspace(coll, config.Config{WorkHours: config.WorkHours{Start: 9, End: 22}})
	head := func() string { return strings.SplitN(readBack(t, path), "\n", 2)[0] }

	run(t, ws, "update_item", map[string]any{"ref": refTo(t, ws, "打包"), "title": "打包", "state": "DONE"})
	if got := head(); got != "* TODO 搬家 [1/2]" {
		t.Fatalf("子任务标完成后父条目 = %q", got)
	}
	run(t, ws, "create_item", map[string]any{"title": "退押金", "state": "TODO", "parent": refTo(t, ws, "搬家")})
	if got := head(); got != "* TODO 搬家 [1/3]" {
		t.Fatalf("加了子任务后父条目 = %q", got)
	}
	run(t, ws, "delete_item", map[string]any{"ref": refTo(t, ws, "叫车"), "title": "叫车"})
	if got := head(); got != "* TODO 搬家 [1/2]" {
		t.Fatalf("删了子任务后父条目 = %q", got)
	}
}
