package org

import (
	"strings"
	"testing"
)

const richDoc = `#+TITLE: 增强语法
#+FILETAGS: :项目:
#+CATEGORY: demo

* TODO 写文档 :写作:
SCHEDULED: <2026-09-14 Mon>
参考 [[https://orgmode.org][org 官网]]。
- [X] 列大纲
- [ ] 写正文
- [ ] 校对

#+BEGIN_SRC emacs-lisp
(setq deadline "<2026-01-01 Thu>")
#+END_SRC

#+BEGIN_EXAMPLE
这里也有 <2026-02-02 Mon> 但不算日程
#+END_EXAMPLE

* COMMENT 草稿区
** TODO 这条不该出现在日程里
SCHEDULED: <2026-09-14 Mon>

* 旧项目 :ARCHIVE:
** TODO 归档后就不管了
SCHEDULED: <2026-09-14 Mon>

* TODO 带进度的任务 [1/3]
** DONE 子任务甲
** TODO 子任务乙
** TODO 子任务丙
`

func TestBlocksAreNotTimestamps(t *testing.T) {
	d := Parse(richDoc)
	n := d.Nodes[0]
	if n.Title != "写文档" {
		t.Fatalf("首节点 = %q", n.Title)
	}
	for _, ts := range n.Timestamps {
		t.Errorf("代码块和示例块里的时间戳不该被当成事件: %s", ts.String())
	}
	if n.Scheduled == nil {
		t.Error("正常的 SCHEDULED 应仍然解析")
	}
}

func TestCheckboxProgress(t *testing.T) {
	d := Parse(richDoc)
	n := d.Nodes[0]
	if len(n.Boxes) != 3 {
		t.Fatalf("复选框数 = %d: %+v", len(n.Boxes), n.Boxes)
	}
	if !n.Boxes[0].Done || n.Boxes[1].Done {
		t.Errorf("勾选状态不对: %+v", n.Boxes)
	}
	if n.Boxes[0].Text != "列大纲" {
		t.Errorf("复选框文本 = %q", n.Boxes[0].Text)
	}
	done, total := n.Progress()
	if done != 1 || total != 3 {
		t.Errorf("进度 = %d/%d", done, total)
	}
}

func TestSubtaskProgress(t *testing.T) {
	d := Parse(richDoc)
	var n *Node
	for _, cand := range d.Nodes {
		if cand.Title == "带进度的任务" {
			n = cand
		}
	}
	if n == nil {
		t.Fatal("找不到带 cookie 的标题")
	}
	if n.Cookie != "[1/3]" {
		t.Errorf("cookie = %q", n.Cookie)
	}
	done, total := n.Progress()
	if done != 1 || total != 3 {
		t.Errorf("按子条目算的进度 = %d/%d", done, total)
	}
	// cookie 不该混进标题，但写回时要还原
	if strings.Contains(n.Title, "[1/3]") {
		t.Errorf("标题里不该留着 cookie: %q", n.Title)
	}
	if got := n.String(); got != "* TODO 带进度的任务 [1/3]" {
		t.Errorf("标题行重建 = %q", got)
	}
}

func TestCommentAndArchiveExcluded(t *testing.T) {
	d := Parse(richDoc)
	var comment, archived, inComment, inArchive *Node
	for _, n := range d.Nodes {
		switch n.Title {
		case "草稿区":
			comment = n
		case "旧项目":
			archived = n
		case "这条不该出现在日程里":
			inComment = n
		case "归档后就不管了":
			inArchive = n
		}
	}
	if comment == nil || !comment.Commented {
		t.Fatalf("COMMENT 未识别: %+v", comment)
	}
	if comment.Title != "草稿区" {
		t.Errorf("COMMENT 不该留在标题里: %q", comment.Title)
	}
	if got := comment.String(); got != "* COMMENT 草稿区" {
		t.Errorf("标题行重建 = %q", got)
	}
	if inComment == nil || inComment.Agendable() {
		t.Error("COMMENT 子树里的条目不该进 agenda")
	}
	if archived == nil || !archived.Archived() {
		t.Error("ARCHIVE 标签未识别")
	}
	if inArchive == nil || inArchive.Agendable() {
		t.Error("归档子树里的条目不该进 agenda")
	}
	if !d.Nodes[0].Agendable() {
		t.Error("普通条目应参与 agenda")
	}
}

func TestFileTags(t *testing.T) {
	d := Parse(richDoc)
	n := d.Nodes[0]
	tags := n.AllTags()
	var hasFile, hasOwn bool
	for _, tg := range tags {
		if tg == "项目" {
			hasFile = true
		}
		if tg == "写作" {
			hasOwn = true
		}
	}
	if !hasFile || !hasOwn {
		t.Errorf("标签 = %v，应同时包含文件级和自身标签", tags)
	}
	if !n.HasTag("项目") {
		t.Error("HasTag 应认得文件级标签")
	}
}
