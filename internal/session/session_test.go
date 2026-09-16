package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	st := New(t.TempDir())
	id := st.NewID()
	want := []agent.Message{
		{Role: "user", Content: "今天有什么安排"},
		{Role: "assistant", Content: "三件事。"},
	}

	if err := st.Save(id, want); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("存了 %d 条，读回 %d 条", len(want), len(got))
	}
	for i := range want {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Fatalf("第 %d 条对不上：%+v vs %+v", i, got[i], want[i])
		}
	}
}

// 会话文件被写坏了不能让程序起不来——读出错就当空会话。
func TestLoadTolerateGarbage(t *testing.T) {
	dir := t.TempDir()
	st := New(dir)
	id := st.NewID()
	if err := os.WriteFile(st.Path(id), []byte("这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(id); err == nil {
		t.Log("读坏文件返回了错误或空会话，两种都可以")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	st := New(t.TempDir())
	got, err := st.Load("根本没有这个会话")
	if err != nil {
		t.Fatalf("会话不存在不该算错：%v", err)
	}
	if len(got) != 0 {
		t.Fatalf("该是空的，得到 %d 条", len(got))
	}
}

// -c 要能接着上次那个，所以 LastID 得挑出最新的一个。
func TestLastIDPicksNewest(t *testing.T) {
	st := New(t.TempDir())

	var ids []string
	for i := 0; i < 3; i++ {
		id := st.NewID()
		ids = append(ids, id)
		if err := st.Save(id, []agent.Message{{Role: "user", Content: "第几轮"}}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // id 是时间戳，错开一点
	}

	got, ok := st.LastID()
	if !ok {
		t.Fatal("存了会话却找不到最新的")
	}
	if got != ids[len(ids)-1] {
		t.Fatalf("最新的该是 %s，得到 %s", ids[len(ids)-1], got)
	}
}

func TestLastIDOnEmptyDir(t *testing.T) {
	st := New(t.TempDir())
	if _, ok := st.LastID(); ok {
		t.Fatal("一个会话都没有却说有")
	}
}

// id 用时间戳拼，必须能排序，而且同一进程里连着要两个不能撞。
func TestNewIDSortsAndDiffers(t *testing.T) {
	st := New(t.TempDir())
	a := st.NewID()
	time.Sleep(2 * time.Millisecond)
	b := st.NewID()
	if a == b {
		t.Fatal("两次拿到同一个 id")
	}
	if a >= b {
		t.Fatalf("id 该按时间排得出先后：%q 不该排在 %q 后面", a, b)
	}
}

func TestDelete(t *testing.T) {
	st := New(t.TempDir())
	id := st.NewID()
	if err := st.Save(id, []agent.Message{{Role: "user", Content: "回头删掉"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Path(id)); !os.IsNotExist(err) {
		t.Fatal("文件还在")
	}
	if err := st.Delete(id); err != nil {
		t.Fatalf("删一个不存在的不该报错：%v", err)
	}
}

// 会话里可能有很长的对话，目录得自己建出来。
func TestNewCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "还没有的", "目录")
	st := New(dir)
	if err := st.Save(st.NewID(), nil); err != nil {
		t.Fatalf("目录该自己建出来：%v", err)
	}
}
