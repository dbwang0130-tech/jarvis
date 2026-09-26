package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dabao/jarvis/internal/agent"
)

// Skill 把健康记录接成秘书的一项技能。
type Skill struct {
	Store *Store
	Now   func() time.Time
}

// New 构造健康技能。path 为空表示还没配。
func New(path string) *Skill {
	s, err := Load(path)
	if err != nil {
		s = &Store{Path: path}
	}
	return &Skill{Store: s}
}

func (s *Skill) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Skill) Name() string { return "健康" }

func (s *Skill) Describe(b *strings.Builder) {
	if s.Store == nil || s.Store.Path == "" {
		b.WriteString("还没配健康记录文件。\n")
		return
	}
	fmt.Fprintf(b, "健康记录：%s（FHIR NDJSON，共 %d 条）\n", s.Store.Path, len(s.Store.Resources))

	if p, ok := s.Store.Patient(); ok {
		line := "基础信息：" + genderCN(p.Gender)
		if p.BirthDate != "" {
			line += fmt.Sprintf("，生日 %s", p.BirthDate)
			if age, ok := ageOf(p.BirthDate, s.now()); ok {
				line += fmt.Sprintf("（%d 岁）", age)
			}
		}
		b.WriteString(line + "\n")
	} else {
		b.WriteString("基础信息还没填（生日、性别）。\n")
	}

	// 最近一次的关键指标。这里是模型判断「这次量的数正不正常、
	// 跟上次比怎么样」的唯一依据——不给的话它只能问用户上次多少。
	//
	// 跟界面用的是**同一个投影**（Summary）。各算各的话，「界面上显示的
	// 最近一次」和「模型看到的最近一次」迟早口径不同，而那种不一致
	// 最难查——两边都说自己没错。
	if sums := s.Store.Summary(s.now()); len(sums) > 0 {
		b.WriteString("最近一次测量（**截至这一轮对话开始时**）：\n")
		for _, v := range sums {
			line := fmt.Sprintf("  %-6s %-16s %s", v.Kind, v.Value, v.Ago)
			if v.Trend != "" {
				line += "（较上次 " + v.Trend + "）"
			}
			b.WriteString(line + "\n")
		}
	}

	if meals := s.Store.Recent("饮食", s.now().AddDate(0, 0, -1), 6); len(meals) > 0 {
		b.WriteString("最近一天吃了：\n")
		for _, m := range meals {
			fmt.Fprintf(b, "  %s %s\n", m.when().Format("01-02 15:04"), m.label())
		}
	}
}

func (s *Skill) Instructions() string {
	return `健康记录存的是 HL7 FHIR 资源（R5），一行一条，只追加不改写。

**LOINC 码和单位归工具管，你不用操心。** 你只说「体重 72.5 公斤」，
record_vital 会去查表填上 29463-7 和 kg。别自己往参数里塞编码——编错的码
比没有码更糟：看着像标准数据，导给医院或者 Apple 健康那边就全错了。

认得的体征：` + strings.Join(vitalNames, "、") + `。
用户说别的（比如「血脂」「尿酸」），直说还没支持，别硬塞到相近的那一项里——
把尿酸记成血糖，以后拿这份数据看趋势会得出完全错的结论。

怎么记：
- 单位按用户说的来，工具会换算成标准单位。血压说「120/80」就拆成收缩压
  120、舒张压 80，一次调用记完——FHIR 里血压是一条带两个分项的记录，
  分开记两条，别的系统读进去不知道这是一次量的。
- 时间留空就是现在。用户说「昨晚」「早上量的」，按【当下】给的今天换算。
- 睡眠记的是时长（小时）。「11 点睡 7 点起」你自己算成 8 小时。
- 饮食用 record_meal，分早餐/午餐/晚餐/加餐。热量不知道就别填——
  凭菜名猜一个数记进去，比空着更糟，因为它会被当成量出来的。

怎么答：
- 记完一句话说清记了什么。【当下】里那张表有上一次的数，值得提一句变化
  （「比上周重了 0.8 公斤」），但别每次都长篇分析。
- **你不是医生。** 可以说「这个数比上次高」「连着三天睡不到 6 小时」这类
  摆事实的话；不要下诊断、不要建议吃什么药、不要说「没事的」。
  真看着不对劲，就说清数值和它偏离了多少，建议去看医生。
- 用户问「我最近怎么样」再去 list_health 拉数据看，别每次记完都主动分析。
- **说到某项体征的变化时，另起一行单独写 [<体征>趋势](jarvis://chart/<体征>)**，
  体征名照上面那张清单里的写法（体重、血压、步数…）。手机上这一行会变成一张
  趋势图挂在回答底下——「最近在往上还是往下」是一句话说不清的东西，而它恰好
  一眼就能看出来。
  规矩三条：整行只写这一个链接，别夹在句子里；一次最多一张，别把四项都列出来；
  没记录过的那项别写。

基础信息（生日、性别、身高）用 set_health_profile。身高单独记成一次测量，
因为它会变（小孩、老人），而生日不会。`
}

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		s.toolRecordVital(),
		s.toolRecordMeal(),
		s.toolSetProfile(),
		s.toolListHealth(),
	}
}

type tool struct {
	spec     agent.ToolSpec
	run      func(args string) (string, bool, error)
	readonly bool
}

func (t tool) Spec() agent.ToolSpec                   { return t.spec }
func (t tool) Risk() agent.Risk                       { return agent.RiskNone }
func (t tool) Preview(string) (string, string, error) { return "", "", nil }
func (t tool) Run(args string) (string, bool, error)  { return t.run(args) }
func (t tool) ReadOnly() bool                         { return t.readonly }

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "number", "description": desc} }

func (s *Skill) toolRecordVital() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name: "record_vital",
			Description: "记一次身体测量：" + strings.Join(vitalNames, "、") +
				"。LOINC 码和标准单位由工具查表填，不用传。",
			Parameters: obj(map[string]any{
				"kind":      str("测的是什么，取值：" + strings.Join(vitalNames, "、")),
				"value":     num("数值。血压不填这个，填 systolic 和 diastolic"),
				"systolic":  num("收缩压（高压），只有 kind 是血压时填"),
				"diastolic": num("舒张压（低压），只有 kind 是血压时填"),
				"at":        str("测量时间，格式 2006-01-02 15:04，留空表示现在"),
				"note":      str("附注，如「空腹」「运动后」"),
			}, "kind"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Kind      string   `json:"kind"`
				Value     *float64 `json:"value"`
				Systolic  *float64 `json:"systolic"`
				Diastolic *float64 `json:"diastolic"`
				At        string   `json:"at"`
				Note      string   `json:"note"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			kind := strings.TrimSpace(a.Kind)
			v, ok := vitals[kind]
			if !ok || v.Sub {
				return "", false, fmt.Errorf("不认得「%s」，能记的是：%s",
					kind, strings.Join(vitalNames, "、"))
			}
			at, err := parseAt(a.At, s.now())
			if err != nil {
				return "", false, err
			}

			v1, v2, err := valuesFor(v, a.Value, a.Systolic, a.Diastolic)
			if err != nil {
				return "", false, err
			}
			r, err := NewVital(kind, v1, v2, at, a.Note)
			if err != nil {
				// 范围不对就多说一句「是不是点错了小数点」——手记这条路上
				// 错误是给模型看的，它该回头跟用户确认，不是默默丢掉。
				if errors.Is(err, ErrOutOfRange) {
					return "", false, fmt.Errorf("%w——是不是点错了小数点？"+
						"确实是这个数的话，跟用户确认一下再记", err)
				}
				return "", false, err
			}
			msg, _, err := s.appendAndReport(kind, r)
			if err != nil {
				return "", false, err
			}
			return msg + s.deriveBMI(kind, v1, at), false, nil
		},
	}
}

// valuesFor 把入参里的几个可选数摆成 NewVital 要的两个位置。
//
// 带分项的（血压）两个都得给；单值的只看 value。
func valuesFor(v vital, value, systolic, diastolic *float64) (float64, float64, error) {
	if len(v.Parts) == 2 {
		if systolic == nil || diastolic == nil {
			return 0, 0, fmt.Errorf("%s 要同时给 systolic 和 diastolic", v.Name)
		}
		return *systolic, *diastolic, nil
	}
	if value == nil {
		return 0, 0, fmt.Errorf("要给 value")
	}
	return *value, 0, nil
}

// deriveBMI 量了体重就顺手把 BMI 也记上，返回要补进回话的那句。
//
// 这是**能算准**的派生值（身高在库里），交给模型自己乘除只会时对时错。
// 没量过身高就算不出来，返回空串——拿个默认身高凑一个数，比不给更糟。
func (s *Skill) deriveBMI(kind string, weightKg float64, at time.Time) string {
	if kind != "体重" {
		return ""
	}
	b, ok := s.Store.bmi(weightKg)
	if !ok {
		return ""
	}
	r, err := NewVital("BMI", round1(b), 0, at, "按最近一次身高算")
	if err != nil || s.Store.Append(r) != nil {
		return ""
	}
	return fmt.Sprintf("\nBMI %.1f（按最近一次身高算）", b)
}

// appendAndReport 写进去，并把「上一次是多少」一起说出来。
//
// 只报新值的话，模型手上另一个数是系统提示里那份快照，它会把两者之差
// 当成异常——记账那边踩过这个坑。
func (s *Skill) appendAndReport(kind string, r Resource) (string, bool, error) {
	prev, hadPrev := s.Store.Latest(kind)
	if err := s.Store.Append(r); err != nil {
		return "", false, err
	}
	msg := "已记录：" + r.label() + "（" + r.when().Format("2006-01-02 15:04") + "）"
	if hadPrev {
		msg += fmt.Sprintf("\n上一次：%s，%s", prev.label(), agoCN(prev.when(), s.now()))
	} else {
		msg += "\n这是第一次记这一项。"
	}
	return msg, false, nil
}

func (s *Skill) toolRecordMeal() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "record_meal",
			Description: "记一餐吃了什么。热量不知道就别填，凭菜名猜的数会被当成量出来的。",
			Parameters: obj(map[string]any{
				"meal": str("哪一餐：早餐 / 午餐 / 晚餐 / 加餐"),
				"items": map[string]any{"type": "array", "items": str("一样吃的东西"),
					"description": "吃了些什么，如 [\"小米粥\", \"鸡蛋\"]"},
				"kcal": num("大概多少千卡，不知道就不填"),
				"at":   str("吃的时间，格式 2006-01-02 15:04，留空表示现在"),
				"note": str("附注"),
			}, "meal", "items"),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Meal  string   `json:"meal"`
				Items []string `json:"items"`
				Kcal  float64  `json:"kcal"`
				At    string   `json:"at"`
				Note  string   `json:"note"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			meal := strings.TrimSpace(a.Meal)
			if meal == "" {
				return "", false, fmt.Errorf("要说是哪一餐")
			}
			var items []string
			for _, it := range a.Items {
				if t := strings.TrimSpace(it); t != "" {
					items = append(items, t)
				}
			}
			if len(items) == 0 {
				return "", false, fmt.Errorf("要说吃了什么")
			}
			at, err := parseAt(a.At, s.now())
			if err != nil {
				return "", false, err
			}
			r := newMeal(meal, items, a.Kcal, at, a.Note)
			if err := s.Store.Append(r); err != nil {
				return "", false, err
			}
			return "已记录：" + r.label() + "（" + at.Format("2006-01-02 15:04") + "）", false, nil
		},
	}
}

func (s *Skill) toolSetProfile() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "set_health_profile",
			Description: "填基础信息：生日、性别。身高会变，用 record_vital 记成一次测量。",
			Parameters: obj(map[string]any{
				"birth_date": str("生日，格式 2006-01-02"),
				"gender":     str("性别：男 / 女 / 其他"),
			}),
		},
		run: func(raw string) (string, bool, error) {
			var a struct {
				Birth  string `json:"birth_date"`
				Gender string `json:"gender"`
			}
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				return "", false, err
			}
			// 没传的那项沿用上一版，别把已经填好的抹成空。
			old, _ := s.Store.Patient()
			birth, gender := strings.TrimSpace(a.Birth), genderCode(a.Gender)
			if birth == "" {
				birth = old.BirthDate
			} else if _, err := time.Parse("2006-01-02", birth); err != nil {
				return "", false, fmt.Errorf("生日要写成 2006-01-02 的样子")
			}
			if gender == "" {
				gender = old.Gender
			}
			r := newPatient(birth, gender)
			if err := s.Store.Append(r); err != nil {
				return "", false, err
			}
			return "已记录：" + r.label(), false, nil
		},
	}
}

func (s *Skill) toolListHealth() agent.Tool {
	return tool{
		spec: agent.ToolSpec{
			Name:        "list_health",
			Description: "查健康记录，用于回答「我最近怎么样」「上个月体重变化」。",
			Parameters: obj(map[string]any{
				"kind":  str("看哪一项：" + strings.Join(vitalNames, "、") + " 或 饮食；留空看全部"),
				"days":  num("往回看几天，默认 30"),
				"limit": num("最多给几条，默认 30"),
			}),
		},
		readonly: true,
		run: func(raw string) (string, bool, error) {
			var a struct {
				Kind  string `json:"kind"`
				Days  int    `json:"days"`
				Limit int    `json:"limit"`
			}
			_ = json.Unmarshal([]byte(raw), &a)
			days, limit := a.Days, a.Limit
			if days <= 0 {
				days = 30
			}
			if limit <= 0 {
				limit = 30
			}
			kind := strings.TrimSpace(a.Kind)
			if kind != "" && kind != "饮食" {
				if _, ok := vitals[kind]; !ok {
					return "", false, fmt.Errorf("不认得「%s」，能查的是：%s 或 饮食",
						kind, strings.Join(vitalNames, "、"))
				}
			}
			recs := s.Store.Recent(kind, s.now().AddDate(0, 0, -days), limit)
			if len(recs) == 0 {
				return fmt.Sprintf("最近 %d 天没有%s记录。\n", days, kind), false, nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "最近 %d 天共 %d 条：\n", days, len(recs))
			for _, r := range recs {
				fmt.Fprintf(&b, "%s  %s\n", r.when().Format("01-02 15:04"), r.label())
			}
			return b.String(), false, nil
		},
	}
}

func parseAt(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02", time.RFC3339} {
		if t, err := time.ParseInLocation(layout, s, now.Location()); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("时间要写成 2006-01-02 15:04 的样子，收到的是 %q", s)
}

func genderCode(s string) string {
	switch strings.TrimSpace(s) {
	case "男", "male", "M":
		return "male"
	case "女", "female", "F":
		return "female"
	case "":
		return ""
	default:
		return "other"
	}
}

func ageOf(birth string, now time.Time) (int, bool) {
	b, err := time.Parse("2006-01-02", birth)
	if err != nil {
		return 0, false
	}
	age := now.Year() - b.Year()
	if now.YearDay() < b.YearDay() {
		age--
	}
	return age, age >= 0
}

// agoCN 把时间说成「今天」「3 天前」——模型看绝对日期还要自己减一遍。
func agoCN(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := dayOf(now).Sub(dayOf(t))
	days := int(d.Hours() / 24)
	switch {
	case days <= 0:
		return "今天"
	case days == 1:
		return "昨天"
	case days < 30:
		return fmt.Sprintf("%d 天前", days)
	default:
		return t.Format("2006-01-02")
	}
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func round1(f float64) float64 {
	return float64(int(f*10+0.5)) / 10
}
