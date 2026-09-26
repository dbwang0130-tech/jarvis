// Package persona 读写 Jarvis 的人设文件。
//
// 单独成一个包是因为**两端都要用同一份读写规则**：桌面端直接读 iCloud 里的
// persona.md，手机端由 Swift 把全文送进 mobile.Ask 再把改动写回去。各写一遍
// 的话，「空文件算不算清空」「要不要 TrimSpace」这类判据迟早在两边走散，
// 而表现出来是「手机上改了人设，电脑上没生效」，看着像同步问题。
package persona

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dabao/jarvis/internal/config"
)

// Load 读出人设全文。
//
// **读不出来一律当成没配**，返回空串，由 agent 退回 DefaultPersona：
// 文件不存在、iCloud 占位符还没下载、权限不对，这几种都不该让秘书那一轮
// 变成一个没身份的模型。启动路径不许把人挡在门外，这条跟 config.Load
// 出错也要交出一份能用的默认配置是同一条规矩。
func Load(path string) string {
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// Save 整段覆盖人设文件。text 为空串表示恢复内置人设。
//
// 恢复内置写的是一个空文件，不是把 DefaultPersona 的文字抄进去——抄进去
// 那份就不再跟着代码走了，以后改内置人设，所有「恢复过默认」的人都还用着
// 旧的那版，而他们并不知道自己手里有一份副本。
//
// 原子写：人设是整份覆盖，写到一半被杀掉就等于把人设清空了。
func Save(path, text string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text != "" {
		text += "\n"
	}
	return config.WriteAtomic(path, []byte(text), 0o600)
}
