package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 配置里的路径会被直接拿去建文件，加载时就得展开成绝对路径，
// 不然会在当前工作目录下造出一个名叫 ~ 的目录。
func TestLoadExpandsPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("拿不到 home")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"files":["~/org"],"inbox":"~/org/inbox.org","ledger_file":"~/finance/ledger.dat"}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"files[0]":    cfg.Files[0],
		"inbox":       cfg.Inbox,
		"ledger_file": cfg.LedgerFile,
		"InboxFile()": cfg.InboxFile(),
	} {
		if strings.HasPrefix(got, "~") {
			t.Errorf("%s 还带着 ~：%q", name, got)
		}
		if !strings.HasPrefix(got, home) {
			t.Errorf("%s 没展开到 home 底下：%q", name, got)
		}
	}
}

// 连配置文件都没有时，默认路径同样不能漏出 ~。
func TestLoadMissingFileStillExpands(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "没有这个文件.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(cfg.LedgerFile, "~") {
		t.Fatalf("默认账本路径没展开：%q", cfg.LedgerFile)
	}
	for i, f := range cfg.Files {
		if strings.HasPrefix(f, "~") {
			t.Fatalf("默认 files[%d] 没展开：%q", i, f)
		}
	}
}

// 配置文件坏了不能让人连界面都进不去：返回默认值 + 一个说得清的错误，
// 而且那份默认值必须是直接能用的（路径都展开过）。
func TestLoadBrokenFileStillUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{这不是合法 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("文件坏了该报错")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("错误里该说明是哪个文件：%v", err)
	}

	// 拿到的配置要能直接用。
	if len(cfg.Files) == 0 {
		t.Fatal("该退回默认的日程文件")
	}
	for _, f := range cfg.Files {
		if strings.HasPrefix(f, "~") {
			t.Fatalf("路径没展开：%q", f)
		}
	}
	if strings.HasPrefix(cfg.LedgerFile, "~") {
		t.Fatalf("账本路径没展开：%q", cfg.LedgerFile)
	}
	if cfg.AgendaDays == 0 || cfg.WorkHours.End == 0 {
		t.Fatal("默认值没填上")
	}
}

// 半截的 JSON 不能让一部分字段漏进来，那样配置是"半坏"的。
func TestLoadPartialJSONFallsBackClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// agenda_days 合法，但后面截断了
	if err := os.WriteFile(path, []byte(`{"agenda_days": 99, "files": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("截断的 JSON 该报错")
	}
	if cfg.AgendaDays == 99 {
		t.Fatal("坏文件里的值不该漏进来，该整份退回默认")
	}
}

// 记忆本来只在电脑上，放配置目录没问题；现在两端共用得放 iCloud。
// 老用户升上来那份记忆还在旧地方，不搬的话秘书就把之前记住的全忘了。
func TestMigrateMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JARVIS_CONFIG", filepath.Join(dir, "cfg", "config.json"))
	if err := os.MkdirAll(filepath.Join(dir, "cfg"), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "cfg", "memory.md")
	if err := os.WriteFile(legacy, []byte("记着：他讨厌开会"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Config{MemoryFile: filepath.Join(dir, "icloud", "memory.md")}
	moved, err := MigrateMemory(cfg)
	if err != nil || !moved {
		t.Fatalf("该搬过去：moved=%v err=%v", moved, err)
	}
	raw, err := os.ReadFile(cfg.MemoryPath())
	if err != nil || string(raw) != "记着：他讨厌开会" {
		t.Fatalf("内容不对：%q %v", raw, err)
	}
	// 原件留着当备份。
	if _, err := os.Stat(legacy); err != nil {
		t.Error("原件不该删")
	}

	// 再跑一次不该出事，也不该覆盖已经改过的目标。
	if err := os.WriteFile(cfg.MemoryPath(), []byte("后来改过的"), 0o600); err != nil {
		t.Fatal(err)
	}
	if moved, err := MigrateMemory(cfg); moved || err != nil {
		t.Fatalf("目标已存在就该什么都不做：moved=%v err=%v", moved, err)
	}
	raw, _ = os.ReadFile(cfg.MemoryPath())
	if string(raw) != "后来改过的" {
		t.Errorf("把已有的覆盖了：%q", raw)
	}
}
