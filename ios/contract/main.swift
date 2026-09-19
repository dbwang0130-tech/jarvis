import Foundation

// 拿 iOS 真正在用的 Models.swift 去解 Go 真实吐出来的 JSON。
// 两端字段名对不上、或者 Go 让某个数组变成了 null，这里就会红。

let dir = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "."
var failed = 0

func check(_ name: String, _ body: () throws -> String) {
    do { print("  ok   \(name)：\(try body())") }
    catch { print("  FAIL \(name)：\(error)"); failed += 1 }
}

func load(_ file: String) throws -> Data {
    try Data(contentsOf: URL(fileURLWithPath: dir).appendingPathComponent(file))
}

let dec = JSONDecoder()

check("Snapshot") {
    let s = try dec.decode(Snapshot.self, from: try load("snapshot.json"))
    let items = s.days.reduce(0) { $0 + $1.items.count }
    guard items > 0, !s.todos.isEmpty, s.ledger != nil else {
        throw Err("解出来是空的，样例数据或解析有问题")
    }
    // org 的语义由 Go 投影过来，别在 Swift 侧照着字符串自己判——
    // CANCELLED 当成没了结，取消掉的事就会跑到「逾期」里去（真发生过）。
    guard let cancelled = s.todos.first(where: { $0.state == "CANCELLED" }) else {
        throw Err("样例里没有 CANCELLED 条目，closed 的取值面没被覆盖")
    }
    guard cancelled.isDone, cancelled.stateLabel == "已取消" else {
        throw Err("CANCELLED 没被当成了结：closed=\(cancelled.closed) "
                  + "label=\(cancelled.stateLabel)")
    }
    guard let waiting = s.todos.first(where: { $0.state == "WAITING" }) else {
        throw Err("样例里没有 WAITING 条目")
    }
    guard !waiting.isDone, waiting.stateLabel == "等回复" else {
        throw Err("WAITING 的中文说法不对：\(waiting.stateLabel)")
    }
    // 健康是后加的，两端字段对不上同样会让整份快照解不开。
    guard let h = s.health else { throw Err("health 没解出来") }
    guard h.profile.contains("岁"), !h.vitals.isEmpty, !h.meals.isEmpty else {
        throw Err("health 解出来是空的：\(h)")
    }
    // 血压是一条带两个分项的记录，过桥之后该是拼好的一串。
    guard h.vitals.contains(where: { $0.value.contains("/") }) else {
        throw Err("血压没拼成 124/81 的样子：\(h.vitals.map(\.value))")
    }
    // 趋势图的点。体重样例里有两条，该能连成线；v2 只有血压才有，
    // 别的点根本没这个键——合成解码器会因此炸掉，这条就是防它的。
    guard let w = h.vitals.first(where: { $0.kind == "体重" }) else {
        throw Err("没有体重")
    }
    guard w.hasTrendLine, w.points.count == 2, w.unit == "kg" else {
        throw Err("体重的趋势点不对：\(w.points.count) 个 / 单位 \(w.unit)")
    }
    guard w.points[0].date < w.points[1].date else {
        throw Err("趋势点没按时间升序")
    }
    // 类别和「是不是累计量」由 Go 那张体征表给——界面按它分区、配色、
    // 决定画柱还是画线。对不上的话整页会退成灰色一大坨。
    guard w.group == "身体测量", !w.isTotal, !w.number.isEmpty else {
        throw Err("体重的类别/累计标记不对：group=\(w.group) total=\(w.isTotal)")
    }
    guard let bp = h.vitals.first(where: { $0.kind == "血压" }),
          bp.points.count == 2, bp.points.last?.v == 124, bp.points.last?.v2 == 81 else {
        throw Err("血压的两条线没过来")
    }
    // 没写标签的条目也得是空数组而不是 null，否则上面 decode 就炸了。
    return "\(s.days.count) 天 / \(items) 条日程 / \(s.todos.count) 待办 / "
        + "净资产 \(s.ledger!.net) / 健康 \(h.profile)，\(h.vitals.count) 项"
}

check("Snapshot(空目录)") {
    let s = try dec.decode(Snapshot.self, from: try load("snapshot_empty.json"))
    return "\(s.days.count) 天 / \(s.todos.count) 待办"
}

check("Snapshot(出错)") {
    let s = try dec.decode(Snapshot.self, from: try load("snapshot_error.json"))
    guard let e = s.error, !e.isEmpty else { throw Err("出错时应该有 error") }
    return e
}

check("AskOutput(出错)") {
    let a = try dec.decode(AskOutput.self, from: try load("ask_error.json"))
    guard let e = a.error, !e.isEmpty else { throw Err("出错时应该有 error") }
    return e
}

check("AskOutput(干完活)") {
    let a = try dec.decode(AskOutput.self, from: try load("ask_ok.json"))
    guard a.history.count == 4 else { throw Err("history 应该有 4 条，实际 \(a.history.count)") }
    // 只调工具那条消息 content 是空的，但必须能解出来而不是抛错。
    let toolCaller = a.history[1]
    guard toolCaller.role == "assistant", toolCaller.content.isEmpty,
          toolCaller.toolCalls?.first?.function?.name == "schedule_item" else {
        throw Err("只调工具的那条消息解错了：\(toolCaller)")
    }
    return "reply=\(a.reply) history=\(a.history.count) steps=\(a.steps?.count ?? 0)"
}

for f in ["mail_list", "mail_error"] {
    check("MailResult(\(f))") {
        let r = try dec.decode(MailResult.self, from: try load("\(f).json"))
        return "items=\(r.items.count) err=\(r.error ?? "-")"
    }
}

// 出参的**必填键**。
//
// 别的断言都是「解得开 + 值对不对」，可 Models.swift 全篇
// `decodeIfPresent ?? 默认值`（为的是旧 framework 不至于整个解不开），
// 于是「Go 少给了一个键」永远不会让解码失败——契约测试唯一的失败通道
// 被那条政策堵死了。closed 就是这么漏过去的：Go 加了字段，手机上装的还是
// 旧 framework，Swift 悄悄走默认值，测试全绿而界面是错的。
//
// 所以这儿绕开模型，直接看 JSON 的键。
check("Snapshot 的必填键") {
    let obj = try JSONSerialization.jsonObject(with: try load("snapshot.json")) as! [String: Any]
    let todos = obj["todos"] as! [[String: Any]]
    guard let first = todos.first else { throw Err("样例里没有待办") }
    let want = ["category", "closed", "due", "due_in", "due_kind", "done", "file",
                "has_notes", "kind", "overdue", "parents", "priority", "ref",
                "repeat", "running", "running_since", "state", "state_label",
                "tags", "time", "title", "total"]
    let missing = want.filter { first[$0] == nil }
    guard missing.isEmpty else {
        throw Err("Item 少了这些键：\(missing)——Swift 那边会悄悄走默认值")
    }
    guard let h = obj["health"] as? [String: Any],
          let vs = h["vitals"] as? [[String: Any]], let v0 = vs.first else {
        throw Err("样例里没有健康数据")
    }
    let vitalKeys = ["kind", "value", "when", "ago", "trend", "unit", "number",
                     "group", "total_kind", "points"]
    for k in vitalKeys where v0[k] == nil {
        throw Err("HealthVital 少了键 \(k)")
    }
    return "Item \(want.count) 个键 / HealthVital \(vitalKeys.count) 个键"
}

// AskInput 方向相反，是 Swift 编码给 Go 的，字段名同样得对上。
check("AskInput 编码") {
    let inp = AskInput(files: ["a.org": "* TODO x"], ledger: "", question: "今天干嘛",
                       history: [Message(role: "user", content: "你好")],
                       model: ModelConfig(), profile: "学生")
    let obj = try JSONSerialization.jsonObject(
        with: try JSONEncoder().encode(inp)) as! [String: Any]
    let want = ["accounts", "files", "health", "history", "images", "ledger", "memory",
                "model", "profile", "question", "tz"]
    guard obj.keys.sorted() == want else {
        throw Err("字段对不上：\(obj.keys.sorted()) 应该是 \(want)")
    }
    let m = obj["model"] as! [String: Any]
    let wantModel = ["api_key", "base_url", "max_steps", "model", "provider",
                     "temperature", "timeout_sec"]
    guard m.keys.sorted() == wantModel else {
        throw Err("model 字段对不上：\(m.keys.sorted())")
    }
    guard let tz = obj["tz"] as? String, !tz.isEmpty else {
        throw Err("tz 是空的——手机上会按 UTC 算日期")
    }
    return "顶层 \(want.count) 个字段（tz=\(tz)），model \(wantModel.count) 个"
}

struct Err: Error, CustomStringConvertible {
    let description: String
    init(_ d: String) { description = d }
}

if failed > 0 {
    print("\n\(failed) 项不通过。两端的字段名和可选性得一起改。")
    exit(1)
}
print("\n两端契约对得上。")
