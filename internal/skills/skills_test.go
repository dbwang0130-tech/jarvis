package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write 在 dir 下造一份技能。
func write(t *testing.T, dir, name, content string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "对账", "---\nname: 月度对账\ndescription: 每月核对账单。说「对账」时用。\n---\n\n先拉流水，再逐笔核。\n")
	// 没有 frontmatter：整份当正文，名字取目录名。
	write(t, dir, "裸的", "就这么一句话。\n")
	// frontmatter 没闭合：格式不对，跳过——否则 name/description 那几行
	// 会被当成正文念给模型。
	write(t, dir, "没闭合", "---\nname: 坏的\ndescription: 坏的\n\n正文\n")
	// 正文空：跳过。
	write(t, dir, "空的", "---\nname: 空\ndescription: 空\n---\n\n   \n")
	// 点开头的目录不扫。
	write(t, dir, ".隐藏", "---\nname: 隐藏\ndescription: x\n---\n\n正文\n")

	list, warns := Load(dir)

	byName := map[string]Skill{}
	for _, s := range list {
		byName[s.Name] = s
	}
	if len(list) != 2 {
		t.Fatalf("该读到 2 份，实际 %d 份：%v", len(list), byName)
	}
	got, ok := byName["月度对账"]
	if !ok {
		t.Fatal("没读到「月度对账」——frontmatter 里的 name 该盖过目录名")
	}
	if !strings.Contains(got.Description, "对账") {
		t.Errorf("description 没读出来：%q", got.Description)
	}
	if strings.Contains(got.Body, "description:") {
		t.Errorf("frontmatter 漏进正文了：%q", got.Body)
	}
	if got.Body != "先拉流水，再逐笔核。" {
		t.Errorf("正文不对：%q", got.Body)
	}
	if _, ok := byName["裸的"]; !ok {
		t.Error("没有 frontmatter 的也该收，名字取目录名")
	}
	if _, ok := byName["隐藏"]; ok {
		t.Error("点开头的目录不该扫")
	}
	// 坏的那两份要有话说，不能悄悄消失。
	if len(warns) == 0 {
		t.Error("跳过了两份却一句警告都没有")
	}
}

// 缺 description 时不丢掉，拿名字顶上并提醒——没有它模型不知道何时该用。
func TestLoadMissingDescription(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "无描述", "---\nname: 无描述\n---\n\n正文在此。\n")
	list, warns := Load(dir)
	if len(list) != 1 {
		t.Fatalf("该读到 1 份，实际 %d", len(list))
	}
	if list[0].Description != "无描述" {
		t.Errorf("该拿名字顶上，实际 %q", list[0].Description)
	}
	if len(warns) != 1 {
		t.Errorf("该提醒一句，实际 %v", warns)
	}
}

// 目录不存在是常态（没建过），不该报警告。
func TestLoadMissingDir(t *testing.T) {
	list, warns := Load(filepath.Join(t.TempDir(), "没有这个目录"))
	if len(list) != 0 || len(warns) != 0 {
		t.Errorf("该安静地什么都不返回，实际 %v / %v", list, warns)
	}
	if l, w := Load(""); l != nil || w != nil {
		t.Errorf("空路径同理，实际 %v / %v", l, w)
	}
}

// BOM 不能把第一行顶掉，否则整份 frontmatter 会被当成正文。
func TestLoadBOM(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bom", "\xef\xbb\xbf---\nname: 带BOM\ndescription: x\n---\n\n正文。\n")
	list, _ := Load(dir)
	if len(list) != 1 {
		t.Fatalf("该读到 1 份，实际 %d", len(list))
	}
	if list[0].Name != "带BOM" {
		t.Errorf("BOM 把 frontmatter 顶掉了：name=%q", list[0].Name)
	}
}

// 超长正文要截断并说一句：它是模型主动读进上下文的，一份十万字
// 一读就把这一轮的预算吃光。
func TestLoadClampsLongBody(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("很长的一段话。", 20000)
	write(t, dir, "巨长", "---\nname: 巨长\ndescription: x\n---\n\n"+long)
	list, _ := Load(dir)
	if len(list) != 1 {
		t.Fatalf("该读到 1 份，实际 %d", len(list))
	}
	body := list[0].Body
	if len(body) > maxBody+64 {
		t.Errorf("没截住：%d 字节", len(body))
	}
	if !strings.Contains(body, "截掉") {
		t.Error("截了却没说")
	}
	// 别把汉字劈成两半。
	if !utf8Valid(body) {
		t.Error("截断切坏了 UTF-8")
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
