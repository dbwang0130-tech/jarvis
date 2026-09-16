package remind

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 已提醒记录的时间戳必须是首次提醒的时刻。要是每次存都刷成当下，
// 24 小时的过期清理就永远不生效，记录只进不出。
func TestSaveKeepsOriginalTimestamps(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Path: filepath.Join(dir, "reminded.json")}

	old := time.Now().Add(-2 * time.Hour).Unix()
	if err := st.Save(map[string]int64{"甲": old}); err != nil {
		t.Fatal(err)
	}
	// 再存一次（模拟下一次 launchd 跑），旧记录的时间戳不该被刷新。
	got := st.Load()
	if got["甲"] != old {
		t.Fatalf("时间戳被改了：%d → %d", old, got["甲"])
	}
	if err := st.Save(got); err != nil {
		t.Fatal(err)
	}
	if again := st.Load(); again["甲"] != old {
		t.Fatalf("存过一轮之后时间戳变成了 %d，原本是 %d", again["甲"], old)
	}
}

// 超过一天的记录读出来时要被清掉，否则同一条日程再也不会响第二次。
func TestLoadDropsStaleEntries(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Path: filepath.Join(dir, "reminded.json")}

	if err := st.Save(map[string]int64{
		"旧的": time.Now().Add(-25 * time.Hour).Unix(),
		"新的": time.Now().Add(-1 * time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	got := st.Load()
	if _, ok := got["旧的"]; ok {
		t.Fatal("超过 24 小时的记录该被清掉")
	}
	if _, ok := got["新的"]; !ok {
		t.Fatal("一小时前的记录不该被清掉")
	}
}

func TestLoadMissingFile(t *testing.T) {
	st := &Store{Path: filepath.Join(t.TempDir(), "没有这个文件.json")}
	if got := st.Load(); len(got) != 0 {
		t.Fatalf("文件不存在时该返回空表，得到 %v", got)
	}
}

func TestSaveCreatesDir(t *testing.T) {
	st := &Store{Path: filepath.Join(t.TempDir(), "还没有的目录", "reminded.json")}
	if err := st.Save(map[string]int64{"甲": time.Now().Unix()}); err != nil {
		t.Fatalf("目录不存在时该自己建出来：%v", err)
	}
	if _, err := os.Stat(st.Path); err != nil {
		t.Fatal(err)
	}
}
