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

// 一次传好几项是合并的正事；而且状态得最后改——同时改计划时间和标完成
// 时，推进出来的下一次要从**新**的计划时间起算，顺序反了就从旧时间推，
// 差出整整一个周期。
func TestUpdateItemAppliesSeveralWithStateLast(t *testing.T) {
	ws, path := newWS(t)

	// 每周组会原本是 <2026-09-17 四 10:00 +1w>。挪到 10-01 再标完成，
	// 循环应该把它推到 10-08；先标完成的话会变成 09-24。
	run(t, ws, "update_item", map[string]any{
		"ref":       refTo(t, ws, "每周组会"),
		"priority":  "A",
		"scheduled": "2026-10-01 10:00",
		"state":     "DONE",
	})

	got := readBack(t, path)
	if !strings.Contains(got, "[#A] 每周组会") {
		t.Fatalf("优先级这一项没跟着改：\n%s", got)
	}
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

	_, err := toolByName(t, ws, "update_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `"}`)
	if err == nil {
		t.Fatal("一项都没给，该报错")
	}
}

// move_item 合并了升降级和挂靠，两种用法各走一条分支，都不给就该报错。
func TestMoveItemNeedsOneOfTwo(t *testing.T) {
	ws, _ := newWS(t)

	_, err := toolByName(t, ws, "move_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `"}`)
	if err == nil {
		t.Fatal("parent_ref 和 level_delta 都没给，该报错")
	}
}

// level_delta 给了 0 也是「没说要往哪挪」。分发是按「键在不在」走的，
// 所以零值会真的走进来，得在里面拦住。
func TestMoveItemRejectsZeroDelta(t *testing.T) {
	ws, _ := newWS(t)

	_, err := toolByName(t, ws, "move_item").
		Run(`{"ref":"` + refTo(t, ws, "每周组会") + `","level_delta":0}`)
	if err == nil {
		t.Fatal("level_delta 是 0，该报错")
	}
}

// 升到顶就该拒绝，而不是先把文档改花一半再说。
func TestMoveItemRefusesToPromotePastTop(t *testing.T) {
	ws, path := newWS(t)
	before := readBack(t, path)

	_, err := toolByName(t, ws, "move_item").
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

// create_item 和 update_item 现在读同一份 itemPatch，「没给」和「给了空串」
// 在两边是同一个意思。以前这两个工具对空串的处理是相反的：新建时空串落成
// 默认 TODO，修改时空串是清除——同一个字段名两个意思，调用方全凭记性。
func TestCreateAndUpdateAgreeOnEmptyString(t *testing.T) {
	ws, path := newWS(t)

	// 没提 state：落默认的 TODO。
	run(t, ws, "create_item", map[string]any{"title": "没提状态的"})
	if got := readBack(t, path); !strings.Contains(got, "* TODO 没提状态的") {
		t.Fatalf("没提状态该落成 TODO：\n%s", got)
	}

	// 明确给空串：建一条不带状态关键字的普通条目。
	run(t, ws, "create_item", map[string]any{"title": "明说不要状态的", "state": ""})
	got := readBack(t, path)
	if !strings.Contains(got, "* 明说不要状态的") || strings.Contains(got, "TODO 明说不要状态的") {
		t.Fatalf("明确传空串该建一条没有状态的条目：\n%s", got)
	}
}

// 参数层：「键在不在」和「值是不是空」必须是两件事，这是整组语义的地基。
func TestArgsDistinguishesMissingFromEmpty(t *testing.T) {
	a, err := parseArgs(`{"scheduled":"","title":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if o := a.OptStr("scheduled"); !o.ok || o.v != "" {
		t.Fatalf("传了空串该是「给了，值为空」，得到 %+v", o)
	}
	if o := a.OptStr("deadline"); o.ok {
		t.Fatal("根本没提的字段不该报告成给过")
	}
	if got := a.OptStr("deadline").Or("兜底"); got != "兜底" {
		t.Fatalf("没给的该走兜底值，得到 %q", got)
	}
	if got := a.OptStr("scheduled").Or("兜底"); got != "" {
		t.Fatalf("给了空串的不该走兜底值，得到 %q", got)
	}
}

// 模型有时候把数字写成字符串，两种都得认——level_delta 和 index 都靠它。
func TestArgsIntAcceptsStringNumbers(t *testing.T) {
	a, _ := parseArgs(`{"n":"3","m":4,"bad":"abc"}`)
	if got := a.Int("n", 0); got != 3 {
		t.Fatalf(`"3" 该认成 3，得到 %d`, got)
	}
	if got := a.Int("m", 0); got != 4 {
		t.Fatalf("4 该认成 4，得到 %d", got)
	}
	if got := a.Int("bad", 9); got != 9 {
		t.Fatalf("认不出来的该走默认值，得到 %d", got)
	}
}

// move_item 的「二选一」得在工具声明里说出来，不能只写在描述的散文里——
// 模型读的是 schema，靠运行期报错纠正要白费一个来回。
func TestMoveItemDeclaresTheEitherOrInSchema(t *testing.T) {
	var spec map[string]any
	for _, d := range wsToolDefs() {
		if d.Name == "move_item" {
			spec = d.Parameters
		}
	}
	if spec == nil {
		t.Fatal("没有 move_item 这个工具")
	}
	alts, ok := spec["anyOf"].([]map[string]any)
	if !ok || len(alts) != 2 {
		t.Fatalf("schema 里该有两个 anyOf 分支，得到 %+v", spec["anyOf"])
	}
	seen := map[string]bool{}
	for _, alt := range alts {
		for _, k := range alt["required"].([]string) {
			seen[k] = true
		}
	}
	if !seen["parent_ref"] || !seen["level_delta"] {
		t.Fatalf("两个分支该分别要 parent_ref 和 level_delta，得到 %v", seen)
	}
}
