package orgtools

import (
	"strings"
	"testing"
)

// update_item 是五个 set_* 工具合并来的，合并本身引入了两条新语义，
// 这两条错了都不会报错，只会悄悄改坏用户的文件：
//
//   - 「字段没传」和「字段传了空串」是两回事。前者不动，后者清除。
//     分不清的话，改个优先级就会把计划时间一起抹掉。
//   - 几项改动有先后。状态必须最后改——把循环任务标成 DONE 会顺手把计划
//     推到下一个周期，先改状态的话，推出来的是按旧时间算的那一天。

// 只传一项的时候，别的项必须原封不动。
func TestUpdateItemLeavesUntouchedFields(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "update_item", map[string]any{
		"ref": refTo(t, ws, "写毕业论文"), "priority": "C",
	})

	got := readBack(t, path)
	if !strings.Contains(got, "[#C] 写毕业论文") {
		t.Fatalf("优先级没改：\n%s", got)
	}
	// 只说了改优先级，计划时间和标签不该受牵连。
	if !strings.Contains(got, "SCHEDULED: <2026-09-16 三 14:00-16:00>") {
		t.Fatalf("没传 scheduled，计划时间却被动了：\n%s", got)
	}
	if !strings.Contains(got, ":thesis:") {
		t.Fatalf("标签丢了：\n%s", got)
	}
}

// 传空串才是「清除」。跟上面那条合起来，把两种语义都钉住。
func TestUpdateItemEmptyStringClears(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "update_item", map[string]any{
		"ref": refTo(t, ws, "补开题报告"), "deadline": "",
	})

	if got := readBack(t, path); strings.Contains(got, "DEADLINE:") {
		t.Fatalf("deadline 传了空串，截止时间该被清掉：\n%s", got)
	}
}

// 一次改好几项：五个工具合一的正事。
func TestUpdateItemChangesSeveralAtOnce(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "update_item", map[string]any{
		"ref":       refTo(t, ws, "补开题报告"),
		"priority":  "A",
		"scheduled": "2026-09-25 14:00",
		"state":     "NEXT",
	})

	got := readBack(t, path)
	for _, want := range []string{"NEXT [#A] 补开题报告", "SCHEDULED: <2026-09-25", "14:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("少改了 %q：\n%s", want, got)
		}
	}
}

// 状态得最后改。同时改计划时间和标完成时，推进出来的下一次要从**新**的
// 计划时间起算——顺序反了就会从旧时间推，差出整整一个周期。
func TestUpdateItemAppliesStateLast(t *testing.T) {
	ws, path := newWS(t)

	// 每周组会原本是 <2026-09-17 四 10:00 +1w>。挪到 10-01 再标完成，
	// 循环应该把它推到 10-08；先标完成的话会变成 09-24。
	run(t, ws, "update_item", map[string]any{
		"ref":       refTo(t, ws, "每周组会"),
		"scheduled": "2026-10-01 10:00",
		"state":     "DONE",
	})

	got := readBack(t, path)
	if !strings.Contains(got, "2026-10-08") {
		t.Fatalf("循环该从新的计划时间往后推一周（10-08）：\n%s", got)
	}
	if strings.Contains(got, "2026-09-24") {
		t.Fatalf("从旧时间推的，说明状态改早了：\n%s", got)
	}
}

// 什么都没传就该直说，别悄悄写一遍文件。
func TestUpdateItemNeedsSomethingToDo(t *testing.T) {
	ws, _ := newWS(t)

	_, _, err := toolByName(t, ws, "update_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `"}`)
	if err == nil {
		t.Fatal("一项都没给，该报错")
	}
}

// move_item 合并了升降级和挂靠，两种用法各走一条分支，都不给就该报错。
func TestMoveItemNeedsOneOfTwo(t *testing.T) {
	ws, _ := newWS(t)

	_, _, err := toolByName(t, ws, "move_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `"}`)
	if err == nil {
		t.Fatal("parent_ref 和 level_delta 都没给，该报错")
	}
}

// 升到顶就该拒绝，而不是先把文档改花一半再说。
func TestMoveItemRefusesToPromotePastTop(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)

	_, _, err := toolByName(t, ws, "move_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `","level_delta":-1}`)
	if err == nil {
		t.Fatal("顶层条目还能再升，该报错")
	}
	if got := readBack(t, path); got != before {
		t.Fatalf("升级失败了却动了文件：\n%s", got)
	}
}

// create_item 的 parent：底层一直支持，工具原先没接出来，于是建不出子条目。
func TestCreateItemUnderParent(t *testing.T) {
	ws, path := newWS(t)

	run(t, ws, "create_item", map[string]any{
		"title":  "查文献",
		"parent": refTo(t, ws, "写毕业论文"),
	})

	got := readBack(t, path)
	if !strings.Contains(got, "** TODO 查文献") {
		t.Fatalf("没挂到父条目下成为二级标题：\n%s", got)
	}
}
