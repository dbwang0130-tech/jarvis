package org

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Collection 是一组 org 文件，agenda 在它之上计算。
type Collection struct {
	Docs  []*Document
	Roots []string // 传入的文件或目录路径，用于重新加载
}

// Load 加载若干 org 文件或目录（目录会递归收集其中的 .org 文件）。
func Load(paths ...string) (*Collection, error) {
	c := &Collection{Roots: paths}
	return c, c.Reload()
}

// Reload 按 Roots 重新读取全部文件。
func (c *Collection) Reload() error {
	files, err := expandOrgPaths(c.Roots)
	if err != nil {
		return err
	}
	docs := make([]*Document, 0, len(files))
	var failed []string
	for _, f := range files {
		doc, err := ParseFile(f)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		docs = append(docs, doc)
	}
	c.Docs = docs
	if len(failed) > 0 {
		return fmt.Errorf("部分文件加载失败: %s", strings.Join(failed, "; "))
	}
	return nil
}

// expandOrgPaths 把文件与目录混合的路径列表展开成 .org 文件列表。
func expandOrgPaths(paths []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		abs, err := filepath.Abs(expandHome(p))
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			if !seen[abs] {
				seen[abs] = true
				out = append(out, abs)
			}
			continue
		}
		err = filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if name := d.Name(); name != "." && strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".org") && !seen[path] {
				seen[path] = true
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// SetLogStates 设置整个集合的状态变更记录开关。
// 文件内用 #+STARTUP: nologdone 显式关掉的，不受影响。
func (c *Collection) SetLogStates(on bool) {
	for _, d := range c.Docs {
		if d.hasStartupLogSetting() {
			continue
		}
		d.LogStates = on
	}
}

// Doc 按路径返回文档。
func (c *Collection) Doc(path string) *Document {
	abs, _ := filepath.Abs(expandHome(path))
	for _, d := range c.Docs {
		if d.Path == abs || d.Path == path || filepath.Base(d.Path) == path {
			return d
		}
	}
	return nil
}
