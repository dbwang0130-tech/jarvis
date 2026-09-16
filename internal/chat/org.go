package chat

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/org"
)

// loadOrg 加载配置里的 org 文件；目录不存在时自动创建，并保证至少有一个可写文件。
// LoadOrg 按配置加载 org 文件集合，必要时把目录和默认收件箱建出来。
// 导出是给 jarvis remind 那条路用的——它不进界面，但要读同一批文件。
func LoadOrg(cfg config.Config) (*org.Collection, error) {
	for _, path := range cfg.Files {
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
	inbox := cfg.InboxFile()
	if inbox == "" {
		inbox = config.Expand("~/org/inbox.org")
	} else if !strings.HasSuffix(inbox, ".org") {
		inbox = filepath.Join(inbox, "inbox.org")
	}
	_ = os.MkdirAll(filepath.Dir(inbox), 0o755)
	_ = os.WriteFile(inbox, []byte("#+TITLE: 收件箱\n\n"), 0o644)
	return org.Load(inbox)
}
