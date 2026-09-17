package mobile

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

// 手机上这几个入口是真会同时在跑的：下拉刷新（Snapshot）的同时点开一条
// 详情（Detail），或者刚划完一下完成（Act）。它们共用的全局状态有：
// time.Local（SetTimeZone 写的）、解析缓存、邮件会话。
//
// 这条用 -race 跑才有意义：go test -race ./mobile/
func TestConcurrentEntries(t *testing.T) {
	org := "* TODO 并发测试\n  SCHEDULED: <2026-09-17 四 10:00>\n  正文一行。\n"
	files := map[string]string{"a.org": org}

	snapIn := mustJSON(t, SnapshotInput{
		Files: files, Days: 7, Now: "2026-09-17T09:00:00+08:00", TZ: "Asia/Shanghai"})
	detailIn := mustJSON(t, DetailInput{Files: files, Ref: "a.org:0", TZ: "Asia/Tokyo"})
	actIn := mustJSON(t, ActInput{Files: files, Ref: "a.org:0", Op: "done", TZ: "UTC"})

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); checkJSON(t, Snapshot(snapIn)) }()
		go func() { defer wg.Done(); checkJSON(t, Detail(detailIn)) }()
		go func() { defer wg.Done(); checkJSON(t, Act(actIn)) }()
	}
	wg.Wait()
}

// 同一批文件反复解析要能命中缓存，而且并发下不能把缓存写坏——
// 返回的是共享的 *org.Collection，写坏了所有调用方一起遭殃。
func TestConcurrentParseCache(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// 一半用同一批文件（命中缓存），一半各不相同（不断换掉缓存）。
			name := "same.org"
			body := "* TODO 一样的\n"
			if n%2 == 1 {
				name = "diff.org"
				body = strings.Repeat("* TODO 不一样\n", n)
			}
			c := collectCached(map[string]string{name: body})
			if c == nil {
				t.Error("解析出来是 nil")
			}
		}(i)
	}
	wg.Wait()
}

func checkJSON(t *testing.T, out string) {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("出参不是合法 JSON：%v", err)
	}
}
