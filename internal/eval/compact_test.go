package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/dabao/jarvis/internal/config"
	"github.com/dabao/jarvis/internal/ledger"
	"github.com/dabao/jarvis/internal/memory"
	"github.com/dabao/jarvis/internal/org"
	"github.com/dabao/jarvis/internal/orgtools"
	"github.com/dabao/jarvis/internal/skillset"
)

// 压缩之后，要紧的事不能被糊掉。
//
// compactPrompt 里列着「必须保留：用户提出过的目标和偏好 / 你已经做了哪些改动 /
// 还没做完的事 / 用户纠正过你的地方」——而**在这条之前没有任何东西验证它真的
// 保留了**。internal/agent 那几条钉的是不变量（工具配对没切散、失败时历史不动、
// 最近几轮原样留着），钉不到「摘要里有没有该有的内容」，那得真模型写一遍才知道。
//
// **不靠真的攒满上下文**：把 ContextLimit 调到很低（默认 32000，这儿给 2500），
// 几轮就越过 0.7 的水位线。攒满两万 token 要跑十几分钟、花几块钱，而要验的那件事
// 跟历史有多长没关系。
func TestCompactKeepsWhatMatters(t *testing.T) {
	if os.Getenv("JARVIS_EVAL") != "1" {
		t.Skip("要真模型、要花钱：JARVIS_EVAL=1 才跑（或 make eval）")
	}
	cfg, err := config.Load(config.Path())
	if err != nil {
		t.Fatalf("读不到配置：%v", err)
	}
	if cfg.Agent.Provider == "" {
		t.Skip("还没配模型服务商")
	}

	dir := t.TempDir()
	orgDir := dir + "/org"
	if err := os.MkdirAll(orgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// **塞满待办，让历史长得快。**
	//
	// 头一版用空日程加七轮短对话，只攒到 58% 就跑完了——短对话攒得太慢，
	// 而每多一轮就多花一次钱。真实会话里查询的返回本来就是一大块，所以
	// 造一份有点分量的日程，两三次查询就越线。
	var seed strings.Builder
	seed.WriteString("#+TITLE: 工作\n#+TODO: TODO NEXT WAITING | DONE CANCELLED\n\n")
	for i := 1; i <= 25; i++ {
		fmt.Fprintf(&seed, "* TODO 第 %d 件事：跟进一下进度并回个话\n  SCHEDULED: <2026-09-%02d Sat 10:00-11:00>\n",
			i, (i%28)+1)
	}
	if err := os.WriteFile(orgDir+"/work.org", []byte(seed.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Files = []string{orgDir}
	cfg.Inbox = orgDir + "/work.org"
	cfg.LedgerFile = dir + "/ledger.dat"
	cfg.MemoryFile = dir + "/memory.md"
	cfg.Accounts = nil

	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.Local)
	coll, err := org.Load(orgDir)
	if err != nil {
		t.Fatal(err)
	}
	ws := orgtools.NewWorkspace(coll, cfg)
	ws.Now = func() time.Time { return now }

	ag, err := agent.Build(cfg, skillset.Build(skillset.Deps{
		Org:    ws,
		Ledger: ledger.NewSkill(cfg.LedgerFile),
		Memory: memory.New(cfg.MemoryFile),
	})...)
	if err != nil {
		t.Fatal(err)
	}
	ag.Now = func() time.Time { return now }
	ag.Stream = false
	ag.Auto = agent.AutoAll
	ag.AutoCompact = true

	// **预算按实测的固定开销算，不能随便填个小数。**
	//
	// 第一次写这条时填了 2500，结果压缩压根没触发（5810 tokens，占 18%）——
	// `contextLimit()` 里有一条「旧口径自愈」：预算不大于固定开销两倍时就当它是
	// 老配置，兜回默认的 32000。那条逻辑是对的（提示和工具声明本来就得装得下），
	// 所以逼压缩只能从上面绕：取一个**刚刚超过固定开销两倍**的预算，于是 0.7 的
	// 水位线离起点很近，几轮历史就越过去了。
	fixed, _ := ag.ContextUsage() // 这会儿历史还是空的，等于固定开销
	// **留余量，别贴着自愈线取值。** 贴着取过一次（fixed*2+1），跑到后面
	// 反而被兜回 32000 了——因为 fixed 自己会浮动：【当下】里的环境快照跟着
	// 日程变，条目一改它就长一点，`ContextLimit > fixed*2` 当场不成立。
	// 表现很迷惑：报告说「8634 tokens，占 27%」，两个数对不上。
	ag.ContextLimit = (fixed + 600) * 2
	t.Logf("固定开销约 %d tokens，预算设成 %d（水位线 %d）",
		fixed, ag.ContextLimit, int(float64(ag.ContextLimit)*0.7))

	var compacted bool
	emit := func(e agent.Event) {
		if e.Kind == agent.EventCompact {
			compacted = true
			t.Logf("压缩：%s", e.Text)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 第一轮交代一件**只有他说过**的事：模型没法猜、也没法从文件里查到。
	// 压缩要是把它糊掉了，后面就答不出来。
	turns := []string{
		"记一下，我车牌是京 A·7X392，以后提到「我的车」就是这一辆",
		"列一下全部待办",
		"这周有什么安排",
		"下周呢",
		"再列一遍全部待办",
		"这个月有哪些没做完的",
		"再看一遍这周",
	}
	// 一轮一轮加，压缩一触发就停——多跑一轮就多花一次钱，而要验的东西
	// 在压缩发生的那一刻就齐了。
	for i, q := range turns {
		if _, err := ag.Ask(ctx, q, emit); err != nil {
			t.Fatalf("第 %d 轮失败：%v", i+1, err)
		}
		if compacted {
			t.Logf("第 %d 轮之后触发了压缩", i+1)
			break
		}
	}
	if !compacted {
		tokens, ratio := ag.ContextUsage()
		// 打**实际生效**的预算，不是我们设的那个——两者不一样正是上面说的
		// 那条自愈在起作用，而只打设进去的值会看得一头雾水。
		effective := 0
		if ratio > 0 {
			effective = int(float64(tokens) / ratio)
		}
		t.Fatalf("跑完 %d 轮还没触发压缩：%d tokens，占 %.0f%%；"+
			"设的预算 %d，实际生效 %d（不一样就是被自愈兜回默认值了）",
			len(turns), tokens, ratio*100, ag.ContextLimit, effective)
	}

	reply, err := ag.Ask(ctx, "我的车牌号是多少？", emit)
	if err != nil {
		t.Fatalf("最后一问失败：%v", err)
	}
	t.Logf("答复：%s", strings.TrimSpace(reply))
	// 空格和分隔符怎么写不管，认数字那一段。
	flat := strings.NewReplacer(" ", "", "·", "", "-", "").Replace(reply)
	if !strings.Contains(flat, "7X392") && !strings.Contains(flat, "7x392") {
		t.Errorf("压缩把用户交代过的事糊掉了——他会发现秘书突然不记得刚说的话：%q", reply)
	}
}
