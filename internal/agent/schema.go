package agent

// JSON Schema 的那点词汇，收在一处。
//
// **原先每个技能包各写一份**：obj 六份、str 七份、num 五份、flag 两份、arr 三份。
// 而它们已经开始漂——同一个 `num` 在 ledger 和 health 里生成 `"number"`，在 mail、
// orgtools、plan 里生成 `"integer"`。同名不同义，取决于你在哪个包里：照着邻居的
// 写法抄一行，抄来的是另一种类型，而模型读的正是这个 schema，运行期也不会报错。
//
// 所以整数和浮点**分成两个名字**（`Int` / `Num`）。选哪个在调用处写明白，不靠
// 所在的包决定。
//
// 各技能包里留的是一行别名（`var obj = agent.Obj`），不是第二份实现——别名指向
// 同一个函数，漂不了。
//
// 约束要写进 schema 而不只写在描述的散文里：模型读的是 schema，靠运行期报错去
// 纠正要白费一个来回。`ObjAnyOf` 就是为这个。

// Obj 是一个对象，required 列出必填的键。
func Obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// ObjAnyOf 在 Obj 之上加一句「anyOfOne 里至少得给一个」。
//
// 生成的是 `anyOf: [{required:[a]}, {required:[b]}]`——不是 oneOf：给了两个
// 也算对，我们要表达的是「不能一个都不给」，不是「只能给一个」。
func ObjAnyOf(props map[string]any, required []string, anyOfOne ...string) map[string]any {
	m := Obj(props, required...)
	alts := make([]map[string]any, 0, len(anyOfOne))
	for _, k := range anyOfOne {
		alts = append(alts, map[string]any{"required": []string{k}})
	}
	m["anyOf"] = alts
	return m
}

// Str 是一个字符串参数。
func Str(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// Int 是一个整数参数（天数、编号、第几步这类）。
func Int(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

// Num 是一个可以带小数的数（金额、体重这类）。
func Num(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

// Flag 是一个布尔参数。
func Flag(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// Arr 是一个数组参数。
func Arr(items map[string]any, desc string) map[string]any {
	return map[string]any{"type": "array", "items": items, "description": desc}
}
