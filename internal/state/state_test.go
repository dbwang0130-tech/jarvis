package state

import (
	"os"
	"testing"
	"time"
)

// 让状态文件落到临时目录，不碰用户真实的配置。
func useTempDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestSaveLoadRoundTrip(t *testing.T) {
	useTempDir(t)

	now := time.Now().Truncate(time.Second)
	s := Load()
	s.Set("qq", Account{LastUID: 4200, UIDValidity: 7, LastSync: now, SeenTotal: 56})
	if err := s.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got := Load().Get("qq")
	if got.LastUID != 4200 || got.UIDValidity != 7 || got.SeenTotal != 56 {
		t.Errorf("读回来是 %+v", got)
	}
	if !got.LastSync.Equal(now) {
		t.Errorf("时间读回来是 %v，应为 %v", got.LastSync, now)
	}
}

// 没同步过就得全量，不能拿 0 当界限去做增量。
func TestResumeRequiresPriorSync(t *testing.T) {
	var fresh Account
	if _, ok := fresh.Resume(7); ok {
		t.Error("从没同步过的账号不该续传")
	}
}

// 服务器重建索引后 UIDVALIDITY 会变，之前记的 UID 全部作废。
// 这时若还按旧 UID 增量，会漏掉大量邮件。
func TestResumeRejectsChangedUIDValidity(t *testing.T) {
	a := Account{LastUID: 4200, UIDValidity: 7}

	if _, ok := a.Resume(7); !ok {
		t.Error("UIDVALIDITY 没变时应当续传")
	}
	if _, ok := a.Resume(8); ok {
		t.Error("UIDVALIDITY 变了必须重新全量，不能续传")
	}

	// 老状态文件没记过 validity，宽容对待，按能续传处理。
	legacy := Account{LastUID: 4200}
	if uid, ok := legacy.Resume(8); !ok || uid != 4200 {
		t.Error("没记录过 UIDVALIDITY 的旧状态应当照常续传")
	}
}

// 状态文件坏了不能让整个工具用不了，当成首次运行即可。
func TestLoadToleratesCorruptFile(t *testing.T) {
	useTempDir(t)

	s := Load()
	s.Set("qq", Account{LastUID: 1})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte("{ 这不是 json"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Load()
	if got.Accounts == nil {
		t.Fatal("损坏时应返回可用的空状态，而不是 nil map")
	}
	if len(got.Accounts) != 0 {
		t.Errorf("损坏的状态不该被当真，实际读出 %d 个账号", len(got.Accounts))
	}
	// 还得能正常写回去。
	got.Set("qq", Account{LastUID: 2})
	if err := got.Save(); err != nil {
		t.Errorf("损坏后应能重新保存: %v", err)
	}
}

// 文件不存在是首次运行，不是错误。
func TestLoadMissingFileIsFirstRun(t *testing.T) {
	useTempDir(t)
	s := Load()
	if s == nil || s.Accounts == nil {
		t.Fatal("首次运行应返回可用的空状态")
	}
	if _, ok := s.Get("任意账号").Resume(1); ok {
		t.Error("空状态不该续传")
	}
}
