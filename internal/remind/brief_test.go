package remind

import (
	"strings"
	"testing"
	"time"

	"github.com/dabao/jarvis/internal/org"
)

// 每天早上那一句要回答的是「今天该操心什么」。Check 只认排了具体钟点的事，
// 而截止和逾期本来就没有钟点——那两样恰恰是最容易忘的。
func briefAt(t *testing.T, text string, hour int, reminded map[string]int64) string {
	t.Helper()
	coll := &org.Collection{}
	doc := org.Parse(text)
	doc.Path = "t.org"
	coll.Docs = append(coll.Docs, doc)
	now := time.Date(2026, 9, 18, hour, 3, 0, 0, time.Local)
	if reminded == nil {
		reminded = map[string]int64{}
	}
	return Brief(coll, now, reminded)
}

func TestBriefCountsDeadlineAndOverdue(t *testing.T) {
	const sample = `* TODO 开会
  SCHEDULED: <2026-09-18 五 10:00>
* TODO 交报告
  DEADLINE: <2026-09-18 五>
* TODO 上周就该做完的
  SCHEDULED: <2026-09-10 四>
`
	got := briefAt(t, sample, BriefHour, nil)
	if got == "" {
		t.Fatal("有事却一声不吭")
	}
	for _, want := range []string{"2 件事", "1 件今天截止", "1 件逾期"} {
		if !strings.Contains(got, want) {
			t.Errorf("少了「%s」：%s", want, got)
		}
	}
	// 「先办哪件」得带上，只给数字还得点开才知道是什么。
	if !strings.Contains(got, "开会") {
		t.Errorf("没说先办什么：%s", got)
	}
}

// 逾期不该混进「今天几件事」里——那是两回事：一个是今天安排的，
// 一个是上周欠的。
func TestBriefSeparatesOverdue(t *testing.T) {
	const sample = `* TODO 上周就该做完的
  SCHEDULED: <2026-09-10 四>
`
	got := briefAt(t, sample, BriefHour, nil)
	if strings.Contains(got, "件事") {
		t.Errorf("把逾期算进今天的事里了：%s", got)
	}
	if !strings.Contains(got, "1 件逾期") {
		t.Errorf("没报逾期：%s", got)
	}
}

// 一天只说一次。launchd 每五分钟跑一趟，不挡的话八点到九点之间会响十二次。
func TestBriefOncePerDay(t *testing.T) {
	const sample = `* TODO 开会
  SCHEDULED: <2026-09-18 五 10:00>
`
	reminded := map[string]int64{}
	if briefAt(t, sample, BriefHour, reminded) == "" {
		t.Fatal("头一次就该说")
	}
	if got := briefAt(t, sample, BriefHour, reminded); got != "" {
		t.Errorf("同一天说了第二次：%s", got)
	}
}

// 不到点不说；什么都没有也不说——「今天没事」也是一次打扰。
func TestBriefStaysQuiet(t *testing.T) {
	const sample = `* TODO 开会
  SCHEDULED: <2026-09-18 五 10:00>
`
	if got := briefAt(t, sample, BriefHour+3, nil); got != "" {
		t.Errorf("不到点就说了：%s", got)
	}
	if got := briefAt(t, "* DONE 做完了\n", BriefHour, nil); got != "" {
		t.Errorf("没事也说了：%s", got)
	}
}
