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
	"fmt"
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

// vital 是一种体征的说明书：中文名对到哪个 LOINC 码、单位是什么、
// 归到哪一类、合理范围在哪儿。
type vital struct {
	Code    string  // LOINC
	Display string  // LOINC 的英文名，导出去别人要看的
	Unit    string  // 给人看的单位
	UCUM    string  // 机器看的单位
	Cat     string  // FHIR observation-category
	Low     float64 // 低于这个数就是明显不对，多半是打错了
	High    float64
}

// vitals 是全部认得的体征。
//
// 键用中文，因为用户和模型说的都是中文；LOINC 码在值里，不外露。
// 加一种体征就往这张表里加一行——别在别处再写一遍 if。
var vitals = map[string]vital{
	"体重":   {"29463-7", "Body weight", "kg", "kg", "vital-signs", 2, 400},
	"身高":   {"8302-2", "Body height", "cm", "cm", "vital-signs", 30, 260},
	"体温":   {"8310-5", "Body temperature", "℃", "Cel", "vital-signs", 30, 45},
	"心率":   {"8867-4", "Heart rate", "次/分", "/min", "vital-signs", 20, 250},
	"呼吸":   {"9279-1", "Respiratory rate", "次/分", "/min", "vital-signs", 4, 80},
	"血氧":   {"59408-5", "Oxygen saturation in Arterial blood by Pulse oximetry", "%", "%", "vital-signs", 50, 100},
	"腰围":   {"56086-2", "Waist Circumference", "cm", "cm", "vital-signs", 30, 250},
	"血糖":   {"15074-8", "Glucose [Moles/volume] in Blood", "mmol/L", "mmol/L", "laboratory", 1, 40},
	"步数":   {"41950-7", "Number of steps in 24 hour Measured", "步", "{steps}", "activity", 0, 200000},
	"睡眠":   {"93832-4", "Sleep duration", "小时", "h", "activity", 0, 24},
	"BMI":  {"39156-5", "Body mass index (BMI) [Ratio]", "kg/m²", "kg/m2", "vital-signs", 5, 100},
	"静息心率": {"40443-4", "Heart rate --resting", "次/分", "/min", "vital-signs", 20, 200},
	"血压":   {"85354-9", "Blood pressure panel with all children optional", "mmHg", "mm[Hg]", "vital-signs", 0, 0},
	"收缩压":  {"8480-6", "Systolic blood pressure", "mmHg", "mm[Hg]", "vital-signs", 50, 300},
	"舒张压":  {"8462-4", "Diastolic blood pressure", "mmHg", "mm[Hg]", "vital-signs", 20, 200},
}

// vitalNames 是给提示和错误话用的顺序，map 遍历是乱的。
var vitalNames = []string{
	"体重", "身高", "BMI", "血压", "静息心率", "心率", "体温", "血氧", "呼吸",
	"血糖", "腰围", "步数", "睡眠",
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

// newObservation 造一条测量记录。
func newObservation(name string, v vital, value float64, at time.Time, note string) Resource {
	r := Resource{
		ResourceType:  "Observation",
		Status:        "final",
		Category:      category(v.Cat, name),
		Code:          codeOf(v),
		Subject:       &Reference{Reference: patientRefID},
		EffectiveTime: at.Format(time.RFC3339),
		ValueQuantity: qty(value, v),
	}
	r.Code.Text = name
	if note != "" {
		r.Note = []Annotation{{Text: note}}
	}
	return r
}

// newBloodPressure 造一条血压。血压在 FHIR 里是一条带两个分项的观察，
// 不是两条独立记录——分开记的话，别的系统读进去不知道这两个数是一次量的。
func newBloodPressure(systolic, diastolic float64, at time.Time, note string) Resource {
	panelV := vitals["血压"]
	sysV, diaV := vitals["收缩压"], vitals["舒张压"]
	r := Resource{
		ResourceType:  "Observation",
		Status:        "final",
		Category:      category(panelV.Cat, "血压"),
		Code:          codeOf(panelV),
		Subject:       &Reference{Reference: patientRefID},
		EffectiveTime: at.Format(time.RFC3339),
		Component: []Component{
			{Code: *codeOf(sysV), ValueQuantity: qty(systolic, sysV)},
			{Code: *codeOf(diaV), ValueQuantity: qty(diastolic, diaV)},
		},
	}
	r.Code.Text = "血压"
	r.Component[0].Code.Text = "收缩压"
	r.Component[1].Code.Text = "舒张压"
	if note != "" {
		r.Note = []Annotation{{Text: note}}
	}
	return r
}

// newMeal 造一条饮食记录。
//
// 用 R5 的 NutritionIntake：R4 里只有 NutritionOrder，那是医嘱「该吃什么」，
// 拿来记「吃了什么」是用错了资源，导给别的系统会被当成医生开的食谱。
func newMeal(kind, when string, items []string, kcal float64, at time.Time, note string) Resource {
	r := Resource{
		ResourceType:  "NutritionIntake",
		Status:        "completed",
		Category:      category("dietary", kind),
		Subject:       &Reference{Reference: patientRefID},
		OccurrenceISO: at.Format(time.RFC3339),
	}
	if when != "" {
		r.Code = &CodeableConcept{Text: when}
	}
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

func (r Resource) when() time.Time {
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
		if len(r.Component) == 2 {
			return fmt.Sprintf("血压 %.0f/%.0f mmHg",
				r.Component[0].ValueQuantity.Value, r.Component[1].ValueQuantity.Value)
		}
		if r.ValueQuantity == nil || r.Code == nil {
			return r.ResourceType
		}
		return fmt.Sprintf("%s %s%s", r.Code.Text,
			trimNum(r.ValueQuantity.Value), r.ValueQuantity.Unit)
	}
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
