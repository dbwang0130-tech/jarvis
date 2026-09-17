import Foundation

// 这些结构跟 Go 那边 mobile 包的 JSON 一一对应。
// 改动时两边要一起改——字段名就是契约。

struct Item: Codable, Identifiable, Hashable {
    var ref: String        // 形如 work.org:12，改动时要原样传回去
    var kind: String       // 计划 / 截止 / 事件 / 逾期 / 待办
    var time: String       // 10:00-11:30，没有就空着
    var state: String      // TODO / NEXT / DONE…
    var priority: String
    var title: String
    var parents: String    // 父条目路径，子任务才有
    var tags: [String]
    var done: Int          // 勾选项进度
    var total: Int
    var repeatRule: String
    var overdue: Bool
    var category: String

    /// 只说有没有正文，正文本身点开详情时才取——正文和勾选项挂在每一条上，
    /// 单条要从约 180 字节涨到 900，五千条就是几 MB 每次刷新都过一遍桥。
    var hasNotes = false
    var file = ""

    var id: String { ref + kind + time }

    enum CodingKeys: String, CodingKey {
        case ref, kind, time, state, priority, title, parents, tags, done, total
        case repeatRule = "repeat"
        case overdue, category
        case hasNotes = "has_notes"
        case file
    }

    /// 勾选项有几个才显示进度。
    var progressText: String? {
        total > 0 ? "\(done)/\(total)" : nil
    }

    var isDone: Bool { state == "DONE" }
}

struct Checkbox: Codable, Hashable, Identifiable {
    var done: Bool
    var text: String
    var id: String { (done ? "1" : "0") + text }
}

// 逐个字段 decodeIfPresent：手机上装的 framework 可能比 Swift 这头旧，
// 少一个键就整屏空白的教训今天吃过两次了。缺了就用默认值，别整个作废。
extension Item {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ref = try c.decodeIfPresent(String.self, forKey: .ref) ?? ""
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        time = try c.decodeIfPresent(String.self, forKey: .time) ?? ""
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        priority = try c.decodeIfPresent(String.self, forKey: .priority) ?? ""
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        parents = try c.decodeIfPresent(String.self, forKey: .parents) ?? ""
        tags = try c.decodeIfPresent([String].self, forKey: .tags) ?? []
        done = try c.decodeIfPresent(Int.self, forKey: .done) ?? 0
        total = try c.decodeIfPresent(Int.self, forKey: .total) ?? 0
        repeatRule = try c.decodeIfPresent(String.self, forKey: .repeatRule) ?? ""
        overdue = try c.decodeIfPresent(Bool.self, forKey: .overdue) ?? false
        category = try c.decodeIfPresent(String.self, forKey: .category) ?? ""
        hasNotes = try c.decodeIfPresent(Bool.self, forKey: .hasNotes) ?? false
        file = try c.decodeIfPresent(String.self, forKey: .file) ?? ""
    }
}

struct Day: Codable, Identifiable {
    var date: String
    var weekday: String
    var today: Bool
    var items: [Item]

    var id: String { date }
}

struct Account: Codable, Identifiable {
    var name: String
    var balance: Double
    var id: String { name }
}

struct TxnBrief: Codable, Identifiable {
    var date: String
    var payee: String
    var amount: Double
    var id: String { date + payee + String(amount) }
}

struct LedgerView: Codable {
    var net: Double
    var assets: Double
    var debts: Double
    var accounts: [Account]
    var recent: [TxnBrief]
}

struct Snapshot: Codable {
    var days: [Day]
    var todos: [Item]
    /// 截断前的待办总数，比 todos.count 大就说明有没显示出来的。
    var todosTotal: Int = 0
    var ledger: LedgerView?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case days, todos, ledger, error
        case todosTotal = "todos_total"
    }

    init(days: [Day], todos: [Item], ledger: LedgerView?, error: String?) {
        self.days = days
        self.todos = todos
        self.todosTotal = todos.count
        self.ledger = ledger
        self.error = error
    }

    static let empty = Snapshot(days: [], todos: [], ledger: nil, error: nil)
}

extension Snapshot {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        days = try c.decodeIfPresent([Day].self, forKey: .days) ?? []
        todos = try c.decodeIfPresent([Item].self, forKey: .todos) ?? []
        todosTotal = try c.decodeIfPresent(Int.self, forKey: .todosTotal) ?? todos.count
        ledger = try c.decodeIfPresent(LedgerView.self, forKey: .ledger)
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}

/// 一条条目的详情，点开才取。字段跟 mobile.DetailOutput 对应。
struct DetailData: Codable {
    var scheduled = ""
    var deadline = ""
    var effort = ""
    var clocked = 0
    var notes = ""
    var boxes: [Checkbox] = []
    var error: String?

    static let empty = DetailData()
}

// MARK: - 对话

struct Message: Codable, Hashable {
    var role: String
    var content: String

    // Go 那边的 agent.Message 还有工具调用等字段，原样带着走就行，
    // Swift 这头只关心 role 和 content。
    var toolCalls: [ToolCall]?
    var toolCallID: String?
    var name: String?
    var reasoningContent: String?

    enum CodingKeys: String, CodingKey {
        case role, content, name
        case toolCalls = "tool_calls"
        case toolCallID = "tool_call_id"
        case reasoningContent = "reasoning_content"
    }
}

/// Go 那边的 agent.Message 是 OpenAI 的 wire 格式，`content` 带 omitempty——
/// 模型只调工具不说话的那条消息，content 是空串，**整个 key 会被丢掉**。
/// 非可选的 String 解到「键不存在」直接抛错，一轮对话就整个作废。
/// 那几个 tag 不能动（发回模型时要合规），所以在这头容忍。
extension Message {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        role = try c.decodeIfPresent(String.self, forKey: .role) ?? ""
        content = try c.decodeIfPresent(String.self, forKey: .content) ?? ""
        toolCalls = try c.decodeIfPresent([ToolCall].self, forKey: .toolCalls)
        toolCallID = try c.decodeIfPresent(String.self, forKey: .toolCallID)
        name = try c.decodeIfPresent(String.self, forKey: .name)
        reasoningContent = try c.decodeIfPresent(String.self, forKey: .reasoningContent)
    }
}

struct ToolCall: Codable, Hashable {
    var id: String?
    var type: String?
    var function: ToolFunction?
}

struct ToolFunction: Codable, Hashable {
    var name: String?
    var arguments: String?
}

struct Step: Codable, Identifiable, Hashable {
    var kind: String   // tool / result / error
    var name: String
    var text: String
    var id: String { kind + name + text }
}

struct ModelConfig: Codable {
    var provider: String = "deepseek"
    var model: String = ""
    var baseURL: String = ""
    var apiKey: String = ""
    var temperature: Double = 0
    var maxSteps: Int = 12
    var timeoutSec: Int = 60

    enum CodingKeys: String, CodingKey {
        case provider, model, temperature
        case baseURL = "base_url"
        case apiKey = "api_key"
        case maxSteps = "max_steps"
        case timeoutSec = "timeout_sec"
    }
}

struct AskInput: Codable {
    var files: [String: String]
    var ledger: String
    var question: String
    var history: [Message]
    var model: ModelConfig
    var profile: String
    /// Go 在 iOS 上读不到系统时区库，不带过去就按 UTC 算日期。
    var tz: String = TimeZone.current.identifier
    /// 长期记忆全文，跟电脑上共用 iCloud 里那一份。
    var memory: String = ""
}

struct AskOutput: Codable {
    var reply: String
    var files: [String: String]?
    var ledger: String?
    var history: [Message]
    var steps: [Step]?
    /// 只在秘书改过记忆时才有。
    var memory: String?
    var error: String?
}
