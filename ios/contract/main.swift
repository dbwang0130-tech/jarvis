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
    // 没写标签的条目也得是空数组而不是 null，否则上面 decode 就炸了。
    return "\(s.days.count) 天 / \(items) 条日程 / \(s.todos.count) 待办 / 净资产 \(s.ledger!.net)"
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

// AskInput 方向相反，是 Swift 编码给 Go 的，字段名同样得对上。
check("AskInput 编码") {
    let inp = AskInput(files: ["a.org": "* TODO x"], ledger: "", question: "今天干嘛",
                       history: [Message(role: "user", content: "你好")],
                       model: ModelConfig(), profile: "学生")
    let obj = try JSONSerialization.jsonObject(
        with: try JSONEncoder().encode(inp)) as! [String: Any]
    let want = ["files", "history", "ledger", "model", "profile", "question"]
    guard obj.keys.sorted() == want else {
        throw Err("字段对不上：\(obj.keys.sorted()) 应该是 \(want)")
    }
    let m = obj["model"] as! [String: Any]
    let wantModel = ["api_key", "base_url", "max_steps", "model", "provider",
                     "temperature", "timeout_sec"]
    guard m.keys.sorted() == wantModel else {
        throw Err("model 字段对不上：\(m.keys.sorted())")
    }
    return "顶层 \(want.count) 个字段，model \(wantModel.count) 个"
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
