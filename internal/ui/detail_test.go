package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/org"
)

// withChecklist 往待办文件里加一个带复选框和链接的条目。
func withChecklist(t *testing.T, m *Model) {
	t.Helper()
	doc := m.coll.Doc("work.org")
	doc.Lines = append(doc.Lines,
		"* TODO 出门清单 [1/3] :打包:",
		"SCHEDULED: <2026-09-14 Mon 08:00>",
		"参考 [[https://example.com/list][清单模板]]，别忘了 *充电器*。",
		"- [X] 钥匙",
		"- [ ] 钱包",
		"- [ ] 耳机",
	)
	doc.Reparse()
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
}

func TestDetailView(t *testing.T) {
	m := newModel(t)
	withChecklist(t, m)
	gotoView(t, m, viewTodos)
	selectTitle(t, m, "出门清单")

	press(m, "enter")
	if m.detail == nil {
		t.Fatal("enter 应打开详情")
	}
	out := m.View()
	for _, want := range []string{"出门清单", "计划", "进度", "1/3", "钥匙", "钱包", "链接", "清单模板"} {
		if !strings.Contains(out, want) {
			t.Errorf("详情缺少 %q:\n%s", want, out)
		}
	}
	// 链接和强调标记应被渲染掉
	if strings.Contains(out, "[[https://example.com/list]") || strings.Contains(out, "*充电器*") {
		t.Errorf("行内标记未渲染:\n%s", out)
	}
	if !strings.Contains(out, "充电器") {
		t.Errorf("强调的内容本身应保留:\n%s", out)
	}

	press(m, "esc")
	if m.detail != nil {
		t.Error("esc 应关闭详情")
	}
}

func TestDetailToggleCheckbox(t *testing.T) {
	m := newModel(t)
	withChecklist(t, m)
	gotoView(t, m, viewTodos)
	selectTitle(t, m, "出门清单")
	path := m.current().Doc.Path
	press(m, "enter")

	// 第一个复选框已勾上，切掉它
	press(m, "x")
	if m.statusErr {
		t.Fatalf("勾选失败: %s", m.status)
	}
	if !strings.Contains(m.status, "0/3") {
		t.Errorf("状态栏应显示新进度: %q", m.status)
	}

	// 往下移动再勾一个
	press(m, "j")
	press(m, "x")
	if !strings.Contains(m.status, "1/3") {
		t.Errorf("状态栏 = %q", m.status)
	}

	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "- [ ] 钥匙") || !strings.Contains(text, "- [X] 钱包") {
		t.Errorf("文件内容不对:\n%s", text)
	}
	if !strings.Contains(text, "出门清单 [1/3]") {
		t.Errorf("标题上的进度标记应同步:\n%s", text)
	}
}

func TestDetailOperationsApplyToItem(t *testing.T) {
	m := newModel(t)
	withChecklist(t, m)
	gotoView(t, m, viewTodos)
	selectTitle(t, m, "出门清单")
	press(m, "enter")

	// 详情里也能改状态和循环
	press(m, "R")
	typeText(m, "每天")
	press(m, "enter")
	if m.statusErr {
		t.Fatalf("详情里设置循环失败: %s", m.status)
	}
	if m.detail == nil {
		t.Fatal("操作后不该退出详情")
	}
	if n := m.detailNode(); n == nil || n.Repeater() == nil {
		t.Fatalf("循环未生效: %+v", m.detailNode())
	}
	if !strings.Contains(m.View(), "每天") {
		t.Errorf("详情应显示循环规则:\n%s", m.View())
	}
}

func TestDeleteWithConfirm(t *testing.T) {
	m := newModel(t)
	gotoView(t, m, viewTodos)
	selectTitle(t, m, "写季度汇报")
	path := m.current().Doc.Path

	press(m, "X")
	if m.prompt != promptConfirm {
		t.Fatal("删除应先确认")
	}
	if !strings.Contains(m.View(), "及其 2 个子条目") {
		t.Errorf("确认提示应说明影响范围:\n%s", m.View())
	}

	// 按别的键就是取消
	press(m, "n")
	if m.prompt != promptNone || !strings.Contains(m.status, "已取消") {
		t.Errorf("取消失败: prompt=%v status=%q", m.prompt, m.status)
	}
	reread, _ := org.ParseFile(path)
	var still bool
	for _, n := range reread.Nodes {
		if n.Title == "写季度汇报" {
			still = true
		}
	}
	if !still {
		t.Fatal("取消后条目不该消失")
	}

	// 这次确认删除
	selectTitle(t, m, "写季度汇报")
	press(m, "X")
	press(m, "y")
	if m.statusErr {
		t.Fatalf("删除失败: %s", m.status)
	}
	reread, _ = org.ParseFile(path)
	for _, n := range reread.Nodes {
		if n.Title == "写季度汇报" || n.Title == "收集各组数据" {
			t.Errorf("应已删除: %s", n.Title)
		}
	}
	if !strings.Contains(m.status, "按 u 可恢复") {
		t.Errorf("应提示可以撤销: %q", m.status)
	}

	// 撤销找回来
	press(m, "u")
	reread, _ = org.ParseFile(path)
	var back bool
	for _, n := range reread.Nodes {
		if n.Title == "写季度汇报" {
			back = true
		}
	}
	if !back {
		t.Error("撤销后条目应回来")
	}
}

func TestDeleteFromDetailClosesIt(t *testing.T) {
	m := newModel(t)
	gotoView(t, m, viewTodos)
	selectTitle(t, m, "整理桌面")
	press(m, "enter")
	press(m, "X")
	press(m, "y")
	if m.detail != nil {
		t.Error("删掉之后详情应关闭")
	}
	if m.statusErr {
		t.Fatalf("删除失败: %s", m.status)
	}
}

func TestListShowsProgressAndPlainTitle(t *testing.T) {
	m := newModel(t)
	withChecklist(t, m)
	gotoView(t, m, viewTodos)
	out := m.View()
	if !strings.Contains(out, "1/3") {
		t.Errorf("列表应显示进度:\n%s", out)
	}
	if strings.Contains(out, "[1/3]") {
		t.Errorf("cookie 不该原样出现在标题里:\n%s", out)
	}
}
