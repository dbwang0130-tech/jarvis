package chat

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (s *Session) installRemind() {
	bin, err := os.Executable()
	if err != nil {
		fail("%s", err.Error())
		return
	}
	plist := remindPlist(bin)
	path := remindPlistPath()
	_ = os.WriteFile(path, []byte(plist), 0o644)
	_ = exec.Command("launchctl", "unload", path).Run()
	if out, err := exec.Command("launchctl", "load", path).CombinedOutput(); err != nil {
		fail("launchctl load 失败：%s", strings.TrimSpace(string(out)))
		return
	}
	note("已安装后台提醒（每 5 分钟）。")
}

func (s *Session) uninstallRemind() {
	path := remindPlistPath()
	_ = exec.Command("launchctl", "unload", path).Run()
	_ = os.Remove(path)
	note("已移除后台提醒。")
}

// remindInstalled 报告后台提醒装没装。
func remindInstalled() bool {
	_, err := os.Stat(remindPlistPath())
	return err == nil
}

func remindPlistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.dabao.jarvis-remind.plist")
}

func remindPlist(bin string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.dabao.jarvis-remind</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>remind</string>
    </array>
    <key>StartInterval</key>
    <integer>300</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/jarvis-remind.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/jarvis-remind.log</string>
</dict>
</plist>
`, bin)
}
