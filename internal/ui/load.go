package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
)

// LoadOrg 加载配置里的 org 文件：目录不存在时自动创建，
// 并保证至少有一个可写入的 .org 文件（首次运行无需手动建）。
func LoadOrg(cfg config.Config) (*org.Collection, error) {
	for _, f := range cfg.Files {
		path := config.Expand(f)
		if strings.HasSuffix(path, ".org") {
			_ = os.MkdirAll(filepath.Dir(path), 0o755)
		} else {
			_ = os.MkdirAll(path, 0o755)
		}
	}

	coll, err := org.Load(cfg.Files...)
	if err != nil {
		return nil, err
	}
	if len(coll.Docs) > 0 {
		return coll, nil
	}

	// 没有任何 .org 文件时，建一个默认收件箱，避免卡在启动。
	inbox := cfg.InboxFile()
	if inbox == "" {
		inbox = config.Expand("~/org/inbox.org")
	} else if !strings.HasSuffix(inbox, ".org") {
		inbox = filepath.Join(inbox, "inbox.org")
	}
	if err := os.MkdirAll(filepath.Dir(inbox), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(inbox, []byte("#+TITLE: 收件箱\n\n"), 0o644); err != nil {
		return nil, err
	}
	return org.Load(inbox)
}
