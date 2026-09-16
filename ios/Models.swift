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

    var id: String { ref + kind + time }

    enum CodingKeys: String, CodingKey {
        case ref, kind, time, state, priority, title, parents, tags, done, total
        case repeatRule = "repeat"
        case overdue, category
    }

    /// 勾选项有几个才显示进度。
    var progressText: String? {
        total > 0 ? "\(done)/\(total)" : nil
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
    var ledger: LedgerView?
    var error: String?

    static let empty = Snapshot(days: [], todos: [], ledger: nil, error: nil)
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
}

struct AskOutput: Codable {
    var reply: String
    var files: [String: String]?
    var ledger: String?
    var history: [Message]
    var steps: [Step]?
    var error: String?
}
