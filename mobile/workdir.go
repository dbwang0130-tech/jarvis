package mobile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// workdir 是一次调用的临时工作目录。
//
// Ask 和 Act 都要同一套动作：把界面传来的文件落到磁盘上、让现成的 orgtools
// 去改、改完读回来只交出真变过的那几个。各写一份的话，哪天给其中一处加了
// 道防护（比如挡住 ../）或者改了收件箱的挑法，另一处就漏了——这跟当初
// 决定复用 orgtools 而不是另写写回逻辑是同一个理由。
type workdir struct {
	root   string
	orgDir string
	before map[string]string
}

func newWorkdir(files map[string]string, prefix string) (*workdir, error) {
	root, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, fmt.Errorf("建不了临时目录：%w", err)
	}
	w := &workdir{root: root, orgDir: filepath.Join(root, "org"), before: files}
	if err := os.MkdirAll(w.orgDir, 0o700); err != nil {
		w.close()
		return nil, fmt.Errorf("建不了临时目录：%w", err)
	}
	for name, content := range files {
		// 只取文件名，不让 ../ 这类东西跑出临时目录。
		p := filepath.Join(w.orgDir, filepath.Base(name))
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			w.close()
			return nil, fmt.Errorf("写临时文件失败：%w", err)
		}
	}
	return w, nil
}

func (w *workdir) close() { _ = os.RemoveAll(w.root) }

// path 返回临时根目录下的一个文件路径（账本、记忆这类单文件用）。
func (w *workdir) path(name string) string { return filepath.Join(w.root, name) }

// write 往临时根目录里放一个文件。
func (w *workdir) write(name, content string) error {
	if err := os.WriteFile(w.path(name), []byte(content), 0o600); err != nil {
		return fmt.Errorf("写临时文件 %s 失败：%w", name, err)
	}
	return nil
}

// inbox 挑一个收件箱：优先叫 inbox.org 的，没有就按名字取第一个。
// 一个都没有时给个默认名——新建的条目总得有地方去。
func (w *workdir) inbox() string {
	names := make([]string, 0, len(w.before))
	for n := range w.before {
		base := filepath.Base(n)
		if strings.EqualFold(base, "inbox.org") {
			return base
		}
		names = append(names, base)
	}
	if len(names) == 0 {
		return "inbox.org"
	}
	sort.Strings(names)
	return names[0]
}

// inboxPath 是收件箱的完整路径。
func (w *workdir) inboxPath() string { return filepath.Join(w.orgDir, w.inbox()) }

// changed 把 org 目录读回来，只挑出内容变过的和新建的。
//
// 全部交回去的话，没动过的文件也会被写回 iCloud——多一次写就多一次同步、
// 多一次跟电脑那头撞车的机会。
func (w *workdir) changed() (map[string]string, error) {
	entries, err := os.ReadDir(w.orgDir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".org") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(w.orgDir, e.Name()))
		if err != nil {
			return nil, err
		}
		if string(raw) != w.before[e.Name()] {
			out[e.Name()] = string(raw)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
