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

    /// 归到哪一天（截止优先于计划），空表示没排期；距今几天，负数是过去了。
    /// 待办列表靠这两项分组。
    var due = ""
    var dueIn = 0
    var dueKind = ""   // deadline / scheduled / 空

    /// 只说有没有正文，正文本身点开详情时才取——正文和勾选项挂在每一条上，
    /// 单条要从约 180 字节涨到 900，五千条就是几 MB 每次刷新都过一遍桥。
    var hasNotes = false
    var file = ""

    var id: String { ref + kind + time }

    enum CodingKeys: String, CodingKey {
        case ref, kind, time, state, priority, title, parents, tags, done, total
        case repeatRule = "repeat"
        case overdue, category
        case due
        case dueIn = "due_in"
        case dueKind = "due_kind"
        case hasNotes = "has_notes"
        case file
    }

    /// 勾选项有几个才显示进度。
    var progressText: String? {
        total > 0 ? "\(done)/\(total)" : nil
    }

    var isDone: Bool { state == "DONE" }

    /// 一句话说清它什么时候到期：今天、明天、过期 3 天、9/20。
    /// 「2026-09-20」要在脑子里减一次才知道远近，「后天」不用。
    var dueText: String {
        guard !due.isEmpty else { return "" }
        let what = dueKind == "deadline" ? "截止" : ""
        let when: String
        switch dueIn {
        case ..<(-1): when = "逾期 \(-dueIn) 天"
        case -1:      when = "昨天"
        case 0:       when = "今天"
        case 1:       when = "明天"
        case 2:       when = "后天"
        case 3...6:   when = "\(dueIn) 天后"
        default:      when = String(due.suffix(5).replacingOccurrences(of: "-", with: "/"))
        }
        return what.isEmpty ? when : when + what
    }
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
        due = try c.decodeIfPresent(String.self, forKey: .due) ?? ""
        dueIn = try c.decodeIfPresent(Int.self, forKey: .dueIn) ?? 0
        dueKind = try c.decodeIfPresent(String.self, forKey: .dueKind) ?? ""
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
    /// asset / debt / income / expense / equity / other，Go 那边按 ledger
    /// 的顶层科目名判的。
    var kind: String = ""

    var id: String { name }

    /// 末一段。`Assets:Bank:招商银行储蓄卡` 在手机上宽度不够，从头截断会
    /// 变成「...sets:RealEstate:朝阳区某小区」，等于把最有用的那段留下、
    /// 最该看的开头砍掉。改成叶子名当主角，上级路径小字跟在后面。
    var leaf: String {
        name.split(separator: ":").last.map(String.init) ?? name
    }

    var parentPath: String {
        let parts = name.split(separator: ":").dropLast()
        return parts.joined(separator: " / ")
    }
}

struct TxnBrief: Codable, Identifiable {
    var date: String
    var payee: String
    /// 带符号：花出去是负的，收进来是正的。
    var amount: Double
    /// expense / income / transfer
    var kind: String = ""
    /// 花在哪儿 / 哪来的，科目的末一段。
    var category: String = ""

    var id: String { date + payee + String(amount) + category }

    var isTransfer: Bool { kind == "transfer" }
    var isIncome: Bool { kind == "income" }
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

/// 一封邮件。主角是 reason（为什么归到这一类），不是 subject——
/// 「Re: Re: FW:」这种主题本身没什么信息量。
struct MailItem: Codable, Identifiable, Hashable {
    var key = ""
    var account = ""
    var from = ""
    var addr = ""
    var subject = ""
    var date = ""
    var unread = false
    var snippet = ""
    var verdict = ""
    var reason = ""
    var hasBody = false

    var id: String { key }

    enum CodingKeys: String, CodingKey {
        case key, account, from, addr, subject, date, unread, snippet
        case verdict, reason
        case hasBody = "has_body"
    }
}

extension MailItem {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        key = try c.decodeIfPresent(String.self, forKey: .key) ?? ""
        account = try c.decodeIfPresent(String.self, forKey: .account) ?? ""
        from = try c.decodeIfPresent(String.self, forKey: .from) ?? ""
        addr = try c.decodeIfPresent(String.self, forKey: .addr) ?? ""
        subject = try c.decodeIfPresent(String.self, forKey: .subject) ?? ""
        date = try c.decodeIfPresent(String.self, forKey: .date) ?? ""
        unread = try c.decodeIfPresent(Bool.self, forKey: .unread) ?? false
        snippet = try c.decodeIfPresent(String.self, forKey: .snippet) ?? ""
        verdict = try c.decodeIfPresent(String.self, forKey: .verdict) ?? ""
        reason = try c.decodeIfPresent(String.self, forKey: .reason) ?? ""
        hasBody = try c.decodeIfPresent(Bool.self, forKey: .hasBody) ?? false
    }
}

struct MailResult: Codable {
    var items: [MailItem] = []
    var body: MailBody?
    var message = ""
    var error: String?
}

/// 一封邮件的原文。HTML 里的内联图片已经嵌成 data URI。
struct MailBody: Codable {
    var html = ""
    var text = ""
    var attachments: [MailAttachment] = []
    /// 因为太大没嵌进来的内联图片数。
    var inlineDropped = 0

    enum CodingKeys: String, CodingKey {
        case html, text, attachments
        case inlineDropped = "inline_dropped"
    }

    var isEmpty: Bool { html.isEmpty && text.isEmpty }
}

struct MailAttachment: Codable, Identifiable, Hashable {
    var name = ""
    var type = ""
    var size = 0

    var id: String { name + type + String(size) }

    var sizeText: String {
        if size < 1024 { return "\(size) B" }
        if size < 1024 * 1024 { return String(format: "%.0f KB", Double(size) / 1024) }
        return String(format: "%.1f MB", Double(size) / 1024 / 1024)
    }
}

/// 模型连通性自检的结果。
struct ModelCheck: Codable {
    var ok = false
    var model = ""
    var reply = ""
    var millis = 0
    var message = ""
    var error: String?
}

/// 直接操作的结果。files 里只有真变过的那几个。
struct ActResult: Codable {
    var message = ""
    var files: [String: String]?
    var error: String?
}

/// 一条条目的详情，点开才取。字段跟 mobile.DetailOutput 对应。
struct DetailData: Codable {
    var scheduled = ""
    var deadline = ""
    var effort = ""
    var clocked = 0
    var notes = ""
    var boxes: [Checkbox] = []
    /// LOGBOOK 里的记录，新的在前。只给「已计时 3 小时」这个总数的话，
    /// 「什么时候做的、做了几回、中间状态怎么变的」全看不出来。
    var log: [LogItem] = []
    var error: String?

    static let empty = DetailData()
}

struct LogItem: Codable, Identifiable {
    /// clock / state / note / other
    var kind = ""
    var when = ""
    var text = ""
    var minutes = 0
    var running = false

    var id: String { kind + when + text }

    var duration: String {
        if running { return "进行中" }
        if minutes < 60 { return "\(minutes) 分钟" }
        return "\(minutes / 60) 小时 \(minutes % 60) 分"
    }
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
    /// 邮箱账号。空的话这一轮就没有邮件能力。
    var accounts: [MailAccountInput] = []
}

/// 传给 Go 的邮箱账号。密码从 iOS 钥匙串取出来现填，不进 UserDefaults。
struct MailAccountInput: Codable {
    var name: String
    var host: String
    var port: Int
    var user: String
    var password: String
    var smtpHost: String
    var smtpPort: Int
    var from: String

    enum CodingKeys: String, CodingKey {
        case name, host, port, user, password, from
        case smtpHost = "smtp_host"
        case smtpPort = "smtp_port"
    }
}

/// 存在本机的邮箱设置。**密码不在这儿**——它单独进钥匙串。
struct MailAccount: Codable, Identifiable, Hashable {
    var id = UUID()
    var name = ""
    var host = ""
    var port = 993
    var user = ""
    var smtpHost = ""
    var smtpPort = 0
    var from = ""

    /// 钥匙串里这条密码的键。跟着 id 走，改名字不会把密码弄丢。
    var secretKey: String { "jarvis.mail.\(id.uuidString)" }

    var password: String {
        get { Keychain.read(key: secretKey) ?? "" }
        nonmutating set { Keychain.write(key: secretKey, value: newValue) }
    }
}

struct AskOutput: Codable {
    var reply: String
    var files: [String: String]?
    var ledger: String?
    var history: [Message]
    var steps: [Step]?
    /// 只在秘书改过记忆时才有。
    var memory: String?
    /// 这一轮是被叫停的，不是自己跑完的。
    var stopped: Bool?
    var error: String?
}
