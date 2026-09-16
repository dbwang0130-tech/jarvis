package configskill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
)

// 这个技能让秘书自己改用户的配置文件，所以要盯紧两件事：
// 改的字段确实落了盘，没让改的字段一根毫毛都不能动。

func newSkill(t *testing.T, cfg config.Config) (*Skill, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	return New(path), path
}

func toolNamed(t *testing.T, s *Skill, name string) agent.Tool {
	t.Helper()
	for _, tl := range s.Tools() {
		if tl.Spec().Name == name {
			return tl
		}
	}
	t.Fatalf("没有名为 %s 的工具", name)
	return nil
}

func call(t *testing.T, s *Skill, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, _, err := toolNamed(t, s, name).Run(string(raw))
	return out, err
}

func reload(t *testing.T, path string) config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSetConfigWritesBack(t *testing.T) {
	s, path := newSkill(t, config.Config{
		WorkHours:  config.WorkHours{Start: 9, End: 22},
		AgendaDays: 7,
	})

	if _, err := call(t, s, "set_config", map[string]any{
		"work_hours_start": 8,
		"agenda_days":      14,
	}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, path)
	if got.WorkHours.Start != 8 {
		t.Fatalf("上班时间没改：%d", got.WorkHours.Start)
	}
	if got.AgendaDays != 14 {
		t.Fatalf("日程天数没改：%d", got.AgendaDays)
	}
	// 没让动的字段要原样留着。
	if got.WorkHours.End != 22 {
		t.Fatalf("下班时间被动了：%d", got.WorkHours.End)
	}
}

// 没传的字段一律不动——patch 用指针就是为了区分「没传」和「传了零值」。
func TestSetConfigLeavesUntouchedFields(t *testing.T) {
	before := config.Config{
		WorkHours:  config.WorkHours{Start: 9, End: 22},
		AgendaDays: 7,
		Profile:    config.Profile{Interests: "我是研究生"},
		Agent:      config.Agent{Provider: "deepseek", Model: "deepseek-chat", MaxSteps: 20},
	}
	s, path := newSkill(t, before)

	if _, err := call(t, s, "set_config", map[string]any{"theme": "dark"}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, path)
	if got.Theme != "dark" {
		t.Fatalf("主题没改：%q", got.Theme)
	}
	for _, c := range []struct{ name, got, want string }{
		{"自述", got.Profile.Interests, before.Profile.Interests},
		{"服务商", got.Agent.Provider, before.Agent.Provider},
		{"模型", got.Agent.Model, before.Agent.Model},
	} {
		if c.got != c.want {
			t.Errorf("%s 被动了：%q → %q", c.name, c.want, c.got)
		}
	}
	if got.Agent.MaxSteps != before.Agent.MaxSteps {
		t.Errorf("最大轮数被动了：%d", got.Agent.MaxSteps)
	}
}

// 传零值是真的要设成零值，不能被当成「没传」——patch 用指针就为这个。
func TestSetConfigAcceptsZero(t *testing.T) {
	s, path := newSkill(t, config.Config{LogStateChanges: true})
	if _, err := call(t, s, "set_config", map[string]any{"log_state_changes": false}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, path); got.LogStateChanges {
		t.Fatal("传 false 该设成 false")
	}
}

// 有几个数值字段的零值会在加载时被默认值顶掉：config.fill() 把零值
// 当成「这份配置里没有这项」。所以 deadline_warn_days=0（到期当天才提醒）
// 这类意图现在表达不出来——patch 那头用指针分得清「没传」和「传了 0」，
// fill 这头分不清，两边语义对不上。
//
// 影响面窄（受影响的只有 deadline_warn_days / agenda_days / work_hours_end
// / max_steps 这几个，而且它们的 0 大多本来也没意义），先钉在这儿，
// 真要支持就得让 fill 按「字段在 JSON 里出现过没有」来判断。
func TestZeroIsOverriddenByDefaults(t *testing.T) {
	s, path := newSkill(t, config.Config{DeadlineWarnDays: 14})
	if _, err := call(t, s, "set_config", map[string]any{"deadline_warn_days": 0}); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, path); got.DeadlineWarnDays == 0 {
		t.Fatal("行为变了：现在 0 能存住了，把这个测试换成正向断言")
	}
}

// 秘书能读配置，但密钥这类东西不该出现在回话里。
func TestReadConfigHidesSecrets(t *testing.T) {
	s, _ := newSkill(t, config.Config{
		Agent: config.Agent{Provider: "deepseek", APIKey: "sk-绝密不能外泄"},
		Accounts: []config.MailAccount{
			{Name: "gmail", User: "me@gmail.com", Host: "imap.gmail.com"},
		},
	})

	got, err := call(t, s, "read_config", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "sk-绝密不能外泄") {
		t.Fatalf("密钥漏进回话里了：\n%s", got)
	}
	if !strings.Contains(got, "deepseek") {
		t.Fatalf("该能看到服务商：\n%s", got)
	}
}

func TestRemoveMailAccount(t *testing.T) {
	s, path := newSkill(t, config.Config{Accounts: []config.MailAccount{
		{Name: "gmail", User: "a@gmail.com", Host: "imap.gmail.com"},
		{Name: "qq", User: "b@qq.com", Host: "imap.qq.com"},
	}})

	if _, err := call(t, s, "remove_mail_account", map[string]any{"name": "gmail"}); err != nil {
		t.Fatal(err)
	}

	got := reload(t, path)
	if len(got.Accounts) != 1 || got.Accounts[0].Name != "qq" {
		t.Fatalf("该只剩 qq，实际 %+v", got.Accounts)
	}

	// 删一个不存在的要说清楚，别让模型以为删掉了。
	if _, err := call(t, s, "remove_mail_account", map[string]any{"name": "没这个"}); err == nil {
		t.Fatal("删不存在的账号该报错")
	}
}

// 配置文件的权限是 600——里面有邮箱地址这类东西。
func TestSavedConfigIsPrivate(t *testing.T) {
	s, path := newSkill(t, config.Config{AgendaDays: 7})
	if _, err := call(t, s, "set_config", map[string]any{"agenda_days": 3}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("配置文件权限是 %o，该是 600", perm)
	}
}
