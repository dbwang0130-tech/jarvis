package mailstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func rec(uid uint32, subject string, day int) Record {
	return Record{
		UID: uid, Subject: subject, From: "老张", Addr: "zhang@example.com",
		Date:    time.Date(2026, 9, day, 10, 0, 0, 0, time.UTC),
		Snippet: "开头几句",
	}
}

// 第一次用的时候索引文件还不存在，不能当成错误——那是最常见的情况。
func TestLoadMissingIsEmpty(t *testing.T) {
	s := newStore(t)
	recs, err := s.Load("工作")
	if err != nil {
		t.Fatalf("没有索引不该报错：%v", err)
	}
	if len(recs) != 0 {
		t.Errorf("该是空的，实际 %d 条", len(recs))
	}
}

// 合并的要害：**已经判过的分拣结论不能被冲掉**。
// 冲掉的话每次同步都要把判过的邮件重新分拣一遍，白花模型调用。
func TestMergeKeepsTriage(t *testing.T) {
	s := newStore(t)
	if _, err := s.Merge("工作", []Record{rec(1, "季度汇报", 10)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTriages("工作", map[uint32]Triage{
		1: {Verdict: "important", Reason: "老板催的"},
	}); err != nil {
		t.Fatal(err)
	}

	// 服务器又把同一封拉下来一次（标题没变，未读状态变了）。
	again := rec(1, "季度汇报", 10)
	again.Unread = false
	recs, err := s.Merge("工作", []Record{again, rec(2, "新的一封", 11)})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("该有 2 条，实际 %d", len(recs))
	}

	var old Record
	for _, r := range recs {
		if r.UID == 1 {
			old = r
		}
	}
	if old.Verdict != "important" || old.Reason != "老板催的" {
		t.Errorf("分拣结论被冲掉了：%+v", old)
	}
	// 服务器说了算的字段要跟着更新。
	if old.Unread {
		t.Error("未读状态没更新")
	}
}

// 新的排前面——列表页一打开看到的该是最新的。
func TestMergeSortsNewestFirst(t *testing.T) {
	s := newStore(t)
	recs, err := s.Merge("工作", []Record{rec(1, "早的", 10), rec(3, "最新", 15), rec(2, "中间", 12)})
	if err != nil {
		t.Fatal(err)
	}
	if recs[0].Subject != "最新" || recs[2].Subject != "早的" {
		t.Errorf("顺序不对：%v", []string{recs[0].Subject, recs[1].Subject, recs[2].Subject})
	}
}

// 清理过的要留墓碑。真删记录的话，下次同步服务器又把它拉回来，
// 用户会觉得「我明明清掉了怎么又出现了」。
func TestDeleteLeavesTombstone(t *testing.T) {
	s := newStore(t)
	if _, err := s.Merge("工作", []Record{rec(1, "垃圾推广", 10)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBody("工作", 1, "一大段广告"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDeleted("工作", []uint32{1}); err != nil {
		t.Fatal(err)
	}

	recs, _ := s.Load("工作")
	if len(recs) != 1 || !recs[0].Deleted {
		t.Fatalf("墓碑没留下：%+v", recs)
	}
	// 正文不必留着占地方。
	if _, ok := s.Body("工作", 1); ok {
		t.Error("清理之后正文还留着")
	}
}

// 正文单独存，索引里只标一个有没有——正文占体积的九成以上，
// 塞进索引会让每次开列表都把几 MB 读进来。
func TestBodyStoredSeparately(t *testing.T) {
	s := newStore(t)
	if _, err := s.Merge("工作", []Record{rec(1, "长信", 10)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Body("工作", 1); ok {
		t.Error("还没存正文却说有")
	}

	body := strings.Repeat("正文很长。", 500)
	if err := s.SetBody("工作", 1, body); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Body("工作", 1)
	if !ok || got != body {
		t.Error("正文存取不一致")
	}
	// 索引本身不该被正文撑大。
	raw, _ := os.ReadFile(s.indexPath("工作"))
	if len(raw) > 2000 {
		t.Errorf("正文漏进索引了，索引有 %d 字节", len(raw))
	}
}

// 索引坏了不能把人挡在门外——邮件在服务器上，本地这份只是缓存。
func TestBrokenIndexFallsBackToEmpty(t *testing.T) {
	s := newStore(t)
	if err := os.WriteFile(s.indexPath("工作"), []byte("这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	recs, err := s.Load("工作")
	if err != nil {
		t.Errorf("坏索引不该报错，当空的重来就行：%v", err)
	}
	if len(recs) != 0 {
		t.Errorf("该当成空的，实际 %d 条", len(recs))
	}
}

// 账号名是用户起的，里面可能有斜杠冒号空格，不能直接当文件名。
func TestAccountNameIsSafeAsFilename(t *testing.T) {
	s := newStore(t)
	for _, name := range []string{"work/mail", "a:b", "带 空格", "..", ""} {
		if _, err := s.Merge(name, []Record{rec(1, "x", 10)}); err != nil {
			t.Fatalf("账号名 %q 存不了：%v", name, err)
		}
		p := s.indexPath(name)
		if filepath.Dir(p) != s.indexDir {
			t.Errorf("账号名 %q 把文件写到了别处：%s", name, p)
		}
	}
}

// 键的格式只有这一个定义处，拼和拆必须对得上——写成两份的话，哪天换了
// 分隔符就会出现一半索引查不到的怪事。
func TestKeyRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		account string
		uid     uint32
	}{{"工作", 1234}, {"a:b", 99}, {"", 0}, {"带 空格", 4294967295}} {
		key := Key(tc.account, tc.uid)
		a, u, ok := SplitKey(key)
		if !ok || a != tc.account || u != tc.uid {
			t.Errorf("Key(%q,%d)=%q 拆回来是 (%q,%d,%v)", tc.account, tc.uid, key, a, u, ok)
		}
	}

	// 拆不出来的要说不行，不能猜。
	for _, bad := range []string{"没有冒号", "工作:", "工作:不是数字", "工作:-1", ""} {
		if _, _, ok := SplitKey(bad); ok {
			t.Errorf("SplitKey(%q) 不该成功", bad)
		}
	}
}

// Record.Key 必须跟 Key() 一致，否则存进去的键跟查出来的对不上。
func TestRecordKeyUsesSharedFormat(t *testing.T) {
	r := Record{Account: "工作", UID: 7}
	if r.Key() != Key("工作", 7) {
		t.Errorf("Record.Key()=%q，Key()=%q", r.Key(), Key("工作", 7))
	}
}

// 读完一封信要真的变成已读。以前没有这条路：正文取出来了，记录还是未读，
// 界面上那个角标一直挂着——人已经读过了，数字却不降。
func TestMarkSeen(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Merge("me", []Record{
		{UID: 1, Subject: "一", Unread: true},
		{UID: 2, Subject: "二", Unread: true},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.MarkSeen("me", []uint32{1}, true); err != nil {
		t.Fatal(err)
	}
	recs, err := s.Load("me")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		switch r.UID {
		case 1:
			if r.Unread {
				t.Error("标了已读还是未读")
			}
		case 2:
			if !r.Unread {
				t.Error("动了不该动的那封")
			}
		}
	}
}
