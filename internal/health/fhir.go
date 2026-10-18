// Package health 管个人健康记录：基础信息、体征测量、睡眠、饮食。
//
// 存的是 HL7 FHIR（R5）资源，一行一条，也就是 FHIR Bulk Data 规定的
// NDJSON 格式。选它有三个理由：
//
//   - **只追加**。跟账本一个路子——手机和电脑两头都在写同一个文件，
//     整文件重写就是在互相吃改动。一行一条，写的时候 O_APPEND 就完事。
//   - **每一行单独拿出来都是合法的 FHIR 资源**。哪天要导给医院、导进
//     Apple 健康、或者拿别的 FHIR 工具看，不需要我们写转换器。
//   - 自己发明一套 JSON 格式省不了多少事，却把数据锁死在这个 app 里。
//     体重就是体重，用 LOINC 29463-7 说出来全世界都认。
//
// **LOINC 码归 Go 管，不归模型管。** 模型只说「体重 72.5 公斤」，
// 查表这件事在这儿做——让模型自己填 LOINC 码，它会编一个出来，
// 而编错的码比没有码更糟：看着像标准数据，其实是假的。
package health

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// FHIR 里用到的几个固定 URL。写成常量是怕手抖打错——
// 这些字符串错一个字母，导出去的数据别人就认不出来了。
const (
	sysLOINC     = "http://loinc.org"
	sysUCUM      = "http://unitsofmeasure.org"
	sysObsCat    = "http://terminology.hl7.org/CodeSystem/observation-category"
	patientRefID = "Patient/me"
)

// Coding 是 FHIR 的编码项。
type Coding struct {
	System  string `json:"system,omitempty"`
	Code    string `json:"code,omitempty"`
	Display string `json:"display,omitempty"`
}

// CodeableConcept 是「一个概念 + 若干种编码说法 + 一句人话」。
type CodeableConcept struct {
	Coding []Coding `json:"coding,omitempty"`
	Text   string   `json:"text,omitempty"`
}

// Quantity 是带单位的数。单位一律走 UCUM，别写「公斤」这种人话——
// 人话在 Text 里，机器看的是 Code。
type Quantity struct {
	Value  float64 `json:"value"`
	Unit   string  `json:"unit,omitempty"`
	System string  `json:"system,omitempty"`
	Code   string  `json:"code,omitempty"`
}

// Identifier 是这条记录在**别的系统里**的身份。
//
// 从 Apple 健康导进来的每一条都带上那边的 UUID：导第二次时靠它认出
// 「这条已经有了」。不记的话，每导一次就多一份，看趋势的图上一个点
// 变成三个点，而且删不掉——文件是只追加的。
type Identifier struct {
	System string `json:"system,omitempty"`
	Value  string `json:"value,omitempty"`
}

// Reference 是指向别的资源。
type Reference struct {
	Reference string `json:"reference,omitempty"`
}

// Component 是一条观察里的分项，血压的收缩压/舒张压就靠它。
type Component struct {
	Code          CodeableConcept `json:"code"`
	ValueQuantity *Quantity       `json:"valueQuantity,omitempty"`
}

// Resource 是存进 NDJSON 的一行。
//
// 没有按 FHIR 的每种资源各写一个结构体：那需要四五个结构体加一个
// 联合类型，而我们只用到 Patient / Observation / NutritionIntake 三种，
// 字段还高度重合。**一个结构体加 omitempty**，序列化出来跟分开写一模一样，
// 读的时候也不用先看 resourceType 再挑类型。
type Resource struct {
	ResourceType string       `json:"resourceType"`
	ID           string       `json:"id,omitempty"`
	Identifier   []Identifier `json:"identifier,omitempty"`

	// Patient
	BirthDate string `json:"birthDate,omitempty"`
	Gender    string `json:"gender,omitempty"`

	// Observation / NutritionIntake 共用
	Status        string            `json:"status,omitempty"`
	Category      []CodeableConcept `json:"category,omitempty"`
	Code          *CodeableConcept  `json:"code,omitempty"`
	Subject       *Reference        `json:"subject,omitempty"`
	EffectiveTime string            `json:"effectiveDateTime,omitempty"`
	OccurrenceISO string            `json:"occurrenceDateTime,omitempty"`
	ValueQuantity *Quantity         `json:"valueQuantity,omitempty"`
	Component     []Component       `json:"component,omitempty"`
	Note          []Annotation      `json:"note,omitempty"`

	// NutritionIntake
	ConsumedItem []ConsumedItem `json:"consumedItem,omitempty"`

	// at 是解析好的时间。不导出所以不进 JSON——它是从 EffectiveTime /
	// OccurrenceISO 算出来的缓存，不是新数据。读进来时解析一次，
	// 排序比较器、过滤、格式化就都不用再解了。
	at time.Time
}

// Annotation 是附注。
type Annotation struct {
	Text string `json:"text"`
}

// ConsumedItem 是吃下去的一样东西。
type ConsumedItem struct {
	Type          CodeableConcept `json:"type"`
	NutritionItem CodeableConcept `json:"nutritionProduct"`
	Amount        *Quantity       `json:"amount,omitempty"`
}

// vital 是一种体征的说明书。
//
// **关于这种体征的全部事实都写在这儿**，别在调用点上写 if。
// 之前 `kind == "BMI"`（派生值不从外部导）和 `kind == "血压"`（一条带两个
// 分项）这两条就散在导入和记录两条路上，加第三条录入路径时又得抄一遍。
type vital struct {
	Name    string  // 中文名，用户和模型说的就是这个
	Code    string  // LOINC
	Display string  // LOINC 的英文名，导出去别人要看的
	Unit    string  // 给人看的单位
	UCUM    string  // 机器看的单位
	Cat     string  // FHIR observation-category
	Low     float64 // 低于这个数就是明显不对，多半是打错了
	High    float64
	// Derived 表示这个值是**算出来的**（现在只有 BMI）。算出来的不从外面导：
	// 搬一份进来，万一那边的身高跟这边不一样，就会跟自己算的那条打架。
	Derived bool
	// Parts 是分项名。非空表示这一项在 FHIR 里是**一条带分项的记录**
	// （血压就是收缩压 + 舒张压）。分开记两条的话，别的系统读进去
	// 不知道这两个数是一次量的。
	Parts []string
	// Sub 表示它只作为别人的分项存在，自己不单独成一项，
	// 所以不进展示顺序、不进提示词里那张「认得哪些」的清单。
	Sub bool
}

// vitalList 是全部认得的体征，**顺序就是展示顺序**。
//
// 加一种体征就往这张表里加一行，别处一个字都不用动：提示词里的清单、
// 导入时导不导、要不要拆分项，全从这儿派生。
var vitalList = []vital{
	{Name: "体重", Code: "29463-7", Display: "Body weight", Unit: "kg", UCUM: "kg", Cat: "vital-signs", Low: 2, High: 400},
	{Name: "身高", Code: "8302-2", Display: "Body height", Unit: "cm", UCUM: "cm", Cat: "vital-signs", Low: 30, High: 260},
	{Name: "BMI", Code: "39156-5", Display: "Body mass index (BMI) [Ratio]", Unit: "kg/m²", UCUM: "kg/m2", Cat: "vital-signs", Low: 5, High: 100, Derived: true},
	{Name: "血压", Code: "85354-9", Display: "Blood pressure panel with all children optional", Unit: "mmHg", UCUM: "mm[Hg]", Cat: "vital-signs", Parts: []string{"收缩压", "舒张压"}},
	{Name: "静息心率", Code: "40443-4", Display: "Heart rate --resting", Unit: "次/分", UCUM: "/min", Cat: "vital-signs", Low: 20, High: 200},
	{Name: "心率", Code: "8867-4", Display: "Heart rate", Unit: "次/分", UCUM: "/min", Cat: "vital-signs", Low: 20, High: 250},
	{Name: "体温", Code: "8310-5", Display: "Body temperature", Unit: "℃", UCUM: "Cel", Cat: "vital-signs", Low: 30, High: 45},
	{Name: "血氧", Code: "59408-5", Display: "Oxygen saturation in Arterial blood by Pulse oximetry", Unit: "%", UCUM: "%", Cat: "vital-signs", Low: 50, High: 100},
	{Name: "呼吸", Code: "9279-1", Display: "Respiratory rate", Unit: "次/分", UCUM: "/min", Cat: "vital-signs", Low: 4, High: 80},
	{Name: "血糖", Code: "15074-8", Display: "Glucose [Moles/volume] in Blood", Unit: "mmol/L", UCUM: "mmol/L", Cat: "laboratory", Low: 1, High: 40},
	{Name: "腰围", Code: "56086-2", Display: "Waist Circumference", Unit: "cm", UCUM: "cm", Cat: "vital-signs", Low: 30, High: 250},
	{Name: "步数", Code: "41950-7", Display: "Number of steps in 24 hour Measured", Unit: "步", UCUM: "{steps}", Cat: "activity", Low: 0, High: 200000},
	{Name: "睡眠", Code: "93832-4", Display: "Sleep duration", Unit: "小时", UCUM: "h", Cat: "activity", Low: 0, High: 24},

	// 下面两条只作为血压的分项存在。
	{Name: "收缩压", Code: "8480-6", Display: "Systolic blood pressure", Unit: "mmHg", UCUM: "mm[Hg]", Cat: "vital-signs", Low: 50, High: 300, Sub: true},
	{Name: "舒张压", Code: "8462-4", Display: "Diastolic blood pressure", Unit: "mmHg", UCUM: "mm[Hg]", Cat: "vital-signs", Low: 20, High: 200, Sub: true},
}

var (
	// vitals 按中文名查。
	vitals = map[string]vital{}
	// vitalNames 是展示和提示词里的顺序，从表里派生——
	// 手抄第二份清单的话，只加进表里的新体征会「静默半可用」：
	// 记得下，但提示词里不列、界面上不出卡片。
	vitalNames []string
)

func init() {
	for _, v := range vitalList {
		vitals[v.Name] = v
		if !v.Sub {
			vitalNames = append(vitalNames, v.Name)
		}
	}
}

func codeOf(v vital) *CodeableConcept {
	return &CodeableConcept{
		Coding: []Coding{{System: sysLOINC, Code: v.Code, Display: v.Display}},
	}
}

func qty(value float64, v vital) *Quantity {
	return &Quantity{Value: value, Unit: v.Unit, System: sysUCUM, Code: v.UCUM}
}

func category(code, text string) []CodeableConcept {
	return []CodeableConcept{{
		Coding: []Coding{{System: sysObsCat, Code: code}},
		Text:   text,
	}}
}

// ErrUnknownVital 表示不认得这种体征。调用方据此决定是报给模型还是默默丢掉。
var ErrUnknownVital = errors.New("不认得这种体征")

// ErrOutOfRange 表示这个数明显不可能，多半是小数点点错了。
var ErrOutOfRange = errors.New("这个数不像是真的")

// NewVital 是**造一条体征记录的唯一入口**。
//
// 手记（record_vital）和从「健康」导入（toResource）两条路原先各写了一遍
// 「查表 → 是不是血压 → 分别校验范围 → 选 panel 还是单值」，于是两边的
// 判断已经开始不一样了。加第三条录入路径（Shortcuts、CSV、桌面端命令）
// 还得再抄一遍。
//
// 两条路真正不同的只有**拿到错误怎么办**：导入是丢掉，手记是报给模型让它
// 跟用户确认。所以错误分了类型，策略留在调用方。
func NewVital(kind string, v1, v2 float64, at time.Time, note string) (Resource, error) {
	v, ok := vitals[strings.TrimSpace(kind)]
	if !ok {
		return Resource{}, fmt.Errorf("%w：%s", ErrUnknownVital, kind)
	}
	if at.IsZero() {
		return Resource{}, fmt.Errorf("%s 没有时间", v.Name)
	}
	r := Resource{
		ResourceType:  "Observation",
		Status:        "final",
		Category:      category(v.Cat, v.Name),
		Code:          codeOf(v),
		Subject:       &Reference{Reference: patientRefID},
		EffectiveTime: at.Format(time.RFC3339),
		at:            at,
	}
	r.Code.Text = v.Name

	if len(v.Parts) == 2 {
		// 一条带两个分项的记录，不是两条独立记录——分开记的话，
		// 别的系统读进去不知道这两个数是一次量的。
		vals := [2]float64{v1, v2}
		for i, name := range v.Parts {
			part := vitals[name]
			if err := rangeCheck(part, vals[i]); err != nil {
				return Resource{}, err
			}
			c := Component{Code: *codeOf(part), ValueQuantity: qty(vals[i], part)}
			c.Code.Text = part.Name
			r.Component = append(r.Component, c)
		}
	} else {
		if err := rangeCheck(v, v1); err != nil {
			return Resource{}, err
		}
		r.ValueQuantity = qty(v1, v)
	}
	if note != "" {
		r.Note = []Annotation{{Text: note}}
	}
	return r, nil
}

// rangeCheck 拦明显不可能的数。
//
// 挡的是打错小数点这类事：体重 725 公斤、体温 368 度。这种数一旦记进去，
// 以后看趋势的图就全毁了，而当时用户多半没注意到。
func rangeCheck(v vital, value float64) error {
	if v.Low == 0 && v.High == 0 {
		return nil
	}
	if value < v.Low || value > v.High {
		return fmt.Errorf("%w：%s %s%s（正常应在 %s~%s%s）",
			ErrOutOfRange, v.Name, trimNum(value), v.Unit,
			trimNum(v.Low), trimNum(v.High), v.Unit)
	}
	return nil
}

// newMeal 造一条饮食记录。
//
// 用 R5 的 NutritionIntake：R4 里只有 NutritionOrder，那是医嘱「该吃什么」，
// 拿来记「吃了什么」是用错了资源，导给别的系统会被当成医生开的食谱。
func newMeal(kind string, items []string, kcal float64, at time.Time, note string) Resource {
	r := Resource{
		ResourceType:  "NutritionIntake",
		Status:        "completed",
		Category:      category("dietary", kind),
		Subject:       &Reference{Reference: patientRefID},
		OccurrenceISO: at.Format(time.RFC3339),
		at:            at,
	}
	r.Code = &CodeableConcept{Text: kind}
	for _, it := range items {
		r.ConsumedItem = append(r.ConsumedItem, ConsumedItem{
			Type:          CodeableConcept{Text: kind},
			NutritionItem: CodeableConcept{Text: it},
		})
	}
	if kcal > 0 {
		r.ValueQuantity = &Quantity{Value: kcal, Unit: "kcal", System: sysUCUM, Code: "kcal"}
	}
	if note != "" {
		r.Note = []Annotation{{Text: note}}
	}
	return r
}

// newPatient 造/更新基础信息。
//
// 同一个 id 再写一条，读的时候后来的盖前面的——文件只追加，改资料就是
// 再记一版。旧版留着不是冗余：体重按生日算年龄这类事，翻回去才知道
// 当时填的是什么。
func newPatient(birth, gender string) Resource {
	return Resource{
		ResourceType: "Patient",
		ID:           "me",
		BirthDate:    birth,
		Gender:       gender,
	}
}

// hasCode 判断这条记录说的是不是某一项。
//
// 原先这三行 nil 守卫在 Latest、matches、history 三处各写了一遍——
// 那是「一个结构体装三种资源」的代价，收进一个方法就只付一次。
func (r Resource) hasCode(code string) bool {
	return r.Code != nil && len(r.Code.Coding) > 0 && r.Code.Coding[0].Code == code
}

// isPanel 判断这条是不是带分项的（血压那种）。
func (r Resource) isPanel() bool { return len(r.Component) == 2 }

// when 是这条记录说的那个时刻。
// statusVoided 是 FHIR 里「这条记错了」的状态。
//
// 记错的记录**不删**：医疗记录跟账本一样不改历史，那条仍留在文件里，
// 只是读的人要当它不存在。FHIR 为此专门留了这个状态，不是这个项目
// 自己发明的约定——别的工具写进来的也认得。
const statusVoided = "entered-in-error"

// voided 报告这条是不是被标成了「录错了」。
func (r Resource) voided() bool { return r.Status == statusVoided }

func (r Resource) when() time.Time {
	if !r.at.IsZero() {
		return r.at
	}
	return r.parseWhen()
}

func (r Resource) parseWhen() time.Time {
	s := r.EffectiveTime
	if s == "" {
		s = r.OccurrenceISO
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// codeValue 是这条记录说的是哪一项（LOINC 码），没有就是空串。
func (r Resource) codeValue() string {
	if r.Code == nil || len(r.Code.Coding) == 0 {
		return ""
	}
	return r.Code.Coding[0].Code
}

// label 是这条记录给人看的一行。
func (r Resource) label() string {
	switch r.ResourceType {
	case "Patient":
		return fmt.Sprintf("基础信息：生日 %s，性别 %s", or(r.BirthDate, "未填"), genderCN(r.Gender))
	case "NutritionIntake":
		s := r.categoryText()
		if len(r.ConsumedItem) > 0 {
			s += "："
			for i, it := range r.ConsumedItem {
				if i > 0 {
					s += "、"
				}
				s += it.NutritionItem.Text
			}
		}
		if r.ValueQuantity != nil {
			s += fmt.Sprintf("（约 %.0f kcal）", r.ValueQuantity.Value)
		}
		return s
	default:
		if r.Code == nil {
			return r.ResourceType
		}
		return r.Code.Text + " " + r.valueText()
	}
}

// valueText 是这条记录的值，带单位，不带项目名。
//
// 摘要卡片上「体重」和「72.5kg」是分开两栏的，原先是拿 label() 拼好再把
// 「体重 」这个前缀剪回去——从自己刚格式化出来的字符串里反解，改一下
// label() 的排版就会静默把手机上的数字弄坏。
func (r Resource) valueText() string {
	n, u := r.numberText(), r.unitText()
	if n == "" {
		return ""
	}
	if r.isPanel() {
		return n + " " + u // 「124/81 mmHg」，数字和单位之间留一格
	}
	return n + u
}

// numberText 只是数字那一串，不带单位。
func (r Resource) numberText() string {
	if r.isPanel() {
		return trimNum(r.Component[0].ValueQuantity.Value) + "/" +
			trimNum(r.Component[1].ValueQuantity.Value)
	}
	if r.ValueQuantity == nil {
		return ""
	}
	return trimNum(r.ValueQuantity.Value)
}

func (r Resource) unitText() string {
	if r.isPanel() {
		return r.Component[0].ValueQuantity.Unit
	}
	if r.ValueQuantity == nil {
		return ""
	}
	return r.ValueQuantity.Unit
}

func (r Resource) categoryText() string {
	if len(r.Category) > 0 && r.Category[0].Text != "" {
		return r.Category[0].Text
	}
	return "饮食"
}

// trimNum 把 72.50 写成 72.5、把 8000.00 写成 8000——
// 步数后面挂两位小数看着就不像步数。
func trimNum(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

func or(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

func genderCN(g string) string {
	switch g {
	case "male":
		return "男"
	case "female":
		return "女"
	case "other":
		return "其他"
	case "":
		return "未填"
	default:
		return g
	}
}
