// Package persona 读写 Jarvis 的人设文件（默认在 iCloud 里的 persona.md）。
//
// agent 不碰文件，人设由调用方读好了交进去、改了经 SavePersona 写回来；
// 「空文件算不算清空」「要不要 TrimSpace」这类判据集中在这里。
package persona

import (
	"os"
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
// 恢复内置写的是一个空文件，不是把 DefaultPersona 的文字抄进去——抄进去那份
// 就不再跟着代码走了，以后改内置人设，所有「恢复过默认」的人都还用着旧的那版，
// 而他们并不知道自己手里有一份副本。
//
// 落盘的规矩（建目录、补结尾换行、原子写）归 config.WriteText——记忆那边是
// 同一份文件形状，两处各写一遍的话权限位和换行判据迟早只改到一边。
func Save(path, text string) error {
	if path == "" {
		return nil
	}
	return config.WriteText(path, text)
}
