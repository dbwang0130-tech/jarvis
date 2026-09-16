package org

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMoveDemotePromote(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.org")

	reload := func() *Document {
		d, err := ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	if err := os.WriteFile(path, []byte("* A\n** A1\n* B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := reload()

	a := d.NodeAt(0)
	a1 := d.NodeAt(1)
	if a1 == nil || a1.Parent != a {
		t.Fatalf("A1 应该在 A 下: %+v", a1)
	}

	// 把 A1 移到 B 下
	b := d.NodeAt(2)
	moved, err := d.MoveNode(a1, b)
	if err != nil {
		t.Fatal(err)
	}
	if moved == nil || moved.Level != 2 || moved.Parent == nil || moved.Parent.Title != "B" {
		t.Fatalf("移动失败: %+v", moved)
	}
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}

	d = reload()
	if d.Lines[0] != "* A" || d.Lines[1] != "* B" || d.Lines[2] != "** A1" {
		t.Fatalf("移动后行内容不对: %v", d.Lines)
	}

	// 把 A1 降一级（2 -> 3），仍是 B 的子节点
	a1 = d.NodeAt(2)
	d.Demote(a1)
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	d = reload()
	if d.Lines[2] != "*** A1" {
		t.Fatalf("降级后应为 *** A1: %v", d.Lines)
	}

	// 再升回一级
	a1 = d.NodeAt(2)
	d.Promote(a1)
	if err := d.Save(); err != nil {
		t.Fatal(err)
	}
	d = reload()
	if d.Lines[2] != "** A1" {
		t.Fatalf("升级后应为 ** A1: %v", d.Lines)
	}
}
