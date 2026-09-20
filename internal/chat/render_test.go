package chat

import (
	"strings"
	"testing"

	"github.com/dabao/jarvis/internal/agent"
	"github.com/mattn/go-runewidth"
)

func TestVisibleTail(t *testing.T) {
	// 没超宽时原样返回，光标宽度按显示宽算（中文两列）。
	got, before := visibleTail("你好ab", 3, 20)
	if got != "你好ab" || before != 5 {
		t.Fatalf("短文本：got %q before %d", got, before)
	}
	// 超宽时只留光标附近的一段，且不超过可用宽度。
	long := strings.Repeat("字", 40)
	got, before = visibleTail(long, 40, 10)
	if w := runewidth.StringWidth(got); w > 10 {
		t.Fatalf("截断后仍超宽：%d", w)
	}
	if before > 10 {
		t.Fatalf("光标跑到框外：%d", before)
	}
}

func TestPadVisIgnoresEscapes(t *testing.T) {
	plain := padVis("abc", 10)
	colored := padVis(coral+"abc"+reset, 10)
	if runewidth.StringWidth(ansiRe.ReplaceAllString(colored, "")) != runewidth.StringWidth(plain) {
		t.Fatalf("带色和不带色补出来的可见宽度不一致：%q vs %q", ansiRe.ReplaceAllString(colored, ""), plain)
	}
}

func TestConfirmOptionsByRisk(t *testing.T) {
	cases := []struct {
		risk agent.Risk
		want int // 选项个数
	}{
		{agent.RiskNone, 2},
		{agent.RiskReversible, 3},
		{agent.RiskIrreversible, 3},
	}
	for _, c := range cases {
		got := newConfirm(&agent.Pending{Title: "清理邮件", Risk: c.risk})
		if len(got.options) != c.want {
			t.Fatalf("risk %v：选项数 %d，想要 %d", c.risk, len(got.options), c.want)
		}
		// 最后一项永远是取消，且不批准。
		got.sel = len(got.options) - 1
		if ok, _ := got.result(); ok {
			t.Fatalf("risk %v：最后一项应当是取消", c.risk)
		}
	}
}

func TestConfirmPickAndResult(t *testing.T) {
	c := newConfirm(&agent.Pending{Title: "发信", Detail: "收件人：a@b.c", Risk: agent.RiskIrreversible})
	c.sel = 1 // 「确认，以后发信也不用问我」
	ok, lv := c.result()
	if !ok || lv == nil || *lv != agent.AutoAll {
		t.Fatalf("提档选项：ok=%v lv=%v", ok, lv)
	}
	c.sel = 0
	if ok, lv := c.result(); !ok || lv != nil {
		t.Fatalf("普通确认不该改档位：ok=%v lv=%v", ok, lv)
	}
	if c.pick(99) {
		t.Fatal("越界编号不该被选中")
	}
}

func TestConfirmLinesAreRectangular(t *testing.T) {
	c := newConfirm(&agent.Pending{Title: "清理邮件", Detail: "3 封广告\n移进垃圾箱", Risk: agent.RiskReversible})
	lines := c.lines()
	if len(lines) < 5 {
		t.Fatalf("框太矮：%d 行", len(lines))
	}
	w := runewidth.StringWidth(ansiRe.ReplaceAllString(lines[0], ""))
	for i, l := range lines {
		if got := runewidth.StringWidth(ansiRe.ReplaceAllString(l, "")); got != w {
			t.Fatalf("第 %d 行宽 %d，框宽 %d：%q", i, got, w, ansiRe.ReplaceAllString(l, ""))
		}
	}
	// 选中项前面有箭头，其余没有。
	body := strings.Join(lines, "\n")
	if strings.Count(body, "❯") != 1 {
		t.Fatalf("应当只有一个选中箭头：\n%s", ansiRe.ReplaceAllString(body, ""))
	}
}

// fakeSession 造一个不接终端的会话，用来测确认框的按键流转。
func fakeSession(p *agent.Pending) *Session {
	s := &Session{
		reader: &reader{ch: make(chan event, 4)},
		ag:     &agent.Agent{Auto: agent.AutoOff},
		box:    newConfirm(p),
	}
	s.pending = p
	return s
}

func TestDecideApproveAndRaiseAuto(t *testing.T) {
	p := &agent.Pending{Title: "清理邮件", Risk: agent.RiskReversible, Reply: make(chan bool, 1)}
	s := fakeSession(p)
	defer scr.stopSpin()

	s.decide("down")  // 挪到「确认，以后这类整理不用问我」
	s.decide("enter") // 选定

	if !<-p.Reply {
		t.Fatal("应当放行这次动作")
	}
	if s.ag.Auto != agent.AutoClean {
		t.Fatalf("档位应提到自动整理，现在是 %v", s.ag.Auto)
	}
	if s.pending != nil || s.box != nil {
		t.Fatal("答完之后挂起状态应当清掉")
	}
}

func TestDecideEscCancels(t *testing.T) {
	p := &agent.Pending{Title: "发信", Risk: agent.RiskIrreversible, Reply: make(chan bool, 1)}
	s := fakeSession(p)
	defer scr.stopSpin()

	s.decide("esc")

	if <-p.Reply {
		t.Fatal("esc 应当否决")
	}
	if s.ag.Auto != agent.AutoOff {
		t.Fatalf("取消不该动档位，现在是 %v", s.ag.Auto)
	}
}

func TestDecideNumberPicksDirectly(t *testing.T) {
	p := &agent.Pending{Title: "清理邮件", Risk: agent.RiskReversible, Reply: make(chan bool, 1)}
	s := fakeSession(p)
	defer scr.stopSpin()

	s.decide("3") // 第三项是取消

	if <-p.Reply {
		t.Fatal("选了取消却放行了")
	}
}

func TestInsertPutsMessageBackInTheBox(t *testing.T) {
	r := &reader{ch: make(chan event, 1)}
	r.insert("帮我把今天的邮件理一下")

	if got := string(r.buf); got != "帮我把今天的邮件理一下" {
		t.Fatalf("消息没退回输入框：%q", got)
	}
	if r.cur != len([]rune("帮我把今天的邮件理一下")) {
		t.Fatalf("光标应当停在末尾，现在在 %d", r.cur)
	}
	// 退回来的消息还能接着编辑。
	r.insert("，只看要紧的")
	if got := string(r.buf); got != "帮我把今天的邮件理一下，只看要紧的" {
		t.Fatalf("接着输入的内容没接上：%q", got)
	}
}

func TestNoteInterruptedMarksHistory(t *testing.T) {
	a := &agent.Agent{}
	a.NoteInterrupted()

	if len(a.History) != 1 {
		t.Fatalf("应当往上下文里记一笔，现在有 %d 条", len(a.History))
	}
	if a.History[0].Role != "system" {
		t.Fatalf("这一笔应当是 system 消息，现在是 %q", a.History[0].Role)
	}
	if !strings.Contains(a.History[0].Content, "没有执行完") {
		t.Fatalf("得说清楚上一条没做完：%q", a.History[0].Content)
	}
}
