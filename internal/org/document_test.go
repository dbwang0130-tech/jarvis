package org

import (
	"strings"
	"testing"
)

func TestNodeNotes(t *testing.T) {
	doc := Parse(`* TODO 写论文
  SCHEDULED: <2026-09-17 四 14:00>
  DEADLINE: <2026-09-20 日>
  :PROPERTIES:
  :EFFORT:   2:00
  :END:
  :LOGBOOK:
  CLOCK: [2026-09-16 三 09:00]--[2026-09-16 三 10:00] =>  1:00
  :END:
  先把提纲列出来。
  注意：老师说 DEADLINE: <2026-09-25 五> 那版不算数。

  - [ ] 写初稿
  - [X] 查文献

  #+BEGIN_SRC sh
  :NOT_A_DRAWER:
  #+END_SRC
** TODO 子任务
   这行属于子任务，不该出现在父节点的正文里。
`)
	n := doc.Nodes[0]
	got := strings.Join(n.Notes(), "\n")

	for _, want := range []string{"先把提纲列出来。", ":NOT_A_DRAWER:", "#+BEGIN_SRC sh"} {
		if !strings.Contains(got, want) {
			t.Errorf("正文里少了 %q：\n%s", want, got)
		}
	}
	// 正文里提到的 DEADLINE 不是 planning 行，得留着。
	if !strings.Contains(got, "老师说") {
		t.Errorf("提到 DEADLINE 的那行被当成 planning 丢了：\n%s", got)
	}
	for _, bad := range []string{"SCHEDULED:", ":EFFORT:", "CLOCK:", "写初稿", "子任务"} {
		if strings.Contains(got, bad) {
			t.Errorf("正文里不该有 %q：\n%s", bad, got)
		}
	}
	if strings.HasPrefix(got, "\n") || strings.HasSuffix(got, "\n") {
		t.Errorf("首尾空行没去掉：%q", got)
	}
}
