import Foundation

// 这些结构跟 Go 那边 mobile 包的 JSON 一一对应。
// 改动时两边要一起改——字段名就是契约。
//
// **下面这些 id 只够用来做 Identifiable，不保证唯一**：它们是拿内容拼出来的，
// 两笔同日同金额同收款方的账、两次一模一样的工具调用、两个同名的附件，
// 都会撞成同一个 id。撞了之后 SwiftUI 的 List 差分会算错行数，直接崩在
// _Bug_Detected_In_Client_Of_UICollectionView_Invalid_Number_Of_Items。
//
// 所以**列表里一律用 ForEach(Array(xs.enumerated()), id: \.offset)**，
// 别直接 ForEach(xs)。真要按 id 做动画和状态保持，先确保那个 id 真的唯一。

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
    /// 这条了结了没有——**做完了或者取消了都算**。
    ///
    /// 由 Go 给，别在这边照着 state 判：了结用哪几个关键字是每份 org 文件
    /// 自己定的（`#+TODO:` 那一行），默认是 DONE 和 CANCELLED，还能加
    /// 别的。这边写死成 `state == "DONE"` 的时候，取消掉的事跑到
    /// 「逾期」里去了。
    var closed = false
    /// 状态给人看的说法（「待办」「等回复」）。由 Go 给——
    /// 关键字集合是每份 org 文件用 `#+TODO:` 自己定的，这边不可能知道。
    var stateLabel = ""
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

    /// 这条正在计时（org 里有一条没收尾的 CLOCK），以及这次是什么时候
    /// 开始的（RFC3339）。
    ///
    /// 以前只有详情里给得出，于是列表上看不出哪条在走，也就给不了「停止」
    /// ——点了开始只能靠「完成」收尾。有了起点，界面和小组件都能让秒表
    /// 自己走，不必每分钟回来问一次。
    var running = false
    var runningSince = ""

    var id: String { ref + kind + time }

    /// 计时从什么时候开始的。给 `Text(_:style: .timer)` 用。
    var startedAt: Date? {
        guard running, !runningSince.isEmpty else { return nil }
        return ISO8601DateFormatter().date(from: runningSince)
    }

    enum CodingKeys: String, CodingKey {
        case ref, kind, time, state, priority, title, parents, tags, done, total
        case repeatRule = "repeat"
        case overdue, closed, category
        case stateLabel = "state_label"
        case due
        case dueIn = "due_in"
        case dueKind = "due_kind"
        case hasNotes = "has_notes"
        case file
        case running
        case runningSince = "running_since"
    }

    /// 勾选项有几个才显示进度。
    var progressText: String? {
        total > 0 ? "\(done)/\(total)" : nil
    }

    var isDone: Bool { closed }

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
        // **不猜。** 这里原先写 `?? (state == "DONE")`，而那正是要修的那段
        // 错误逻辑——Go 那头一旦是旧的（比如 framework 没重编），CANCELLED
        // 又悄悄回到逾期里，还没有任何信号指向真正的原因。宁可全都当成
        // 没了结，那至少是看得见的不对劲。
        closed = try c.decodeIfPresent(Bool.self, forKey: .closed) ?? false
        stateLabel = try c.decodeIfPresent(String.self, forKey: .stateLabel) ?? state
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
    /// 这笔在账本里的下标，冲账时要传回去。光有下标不够安全，那头还会拿
    /// 日期和收款方核对一遍。
    var index: Int = 0

    /// 这笔本身就是一笔冲账。已经冲过的不能再冲——两笔反向的抵完，
    /// 再来一笔就成了重复记账。
    var isReversal: Bool { payee.hasPrefix("冲账：") }

    var id: String { date + payee + String(amount) + category }

    var isTransfer: Bool { kind == "transfer" }
    var isIncome: Bool { kind == "income" }
}

/// 这个月到今天为止的收支，以及跟上月同期的比较。
struct MonthFlow: Codable {
    var income: Double = 0
    var expense: Double = 0
    /// 上个月**同一天为止**的支出，不是上月全月——跟全月比的话，每个月初
    /// 都会显得「比上月少花很多」，到月底又追平。
    var lastExpense: Double = 0
    var days: Int = 0

    /// 比上月同期多花/少花多少。上月同期一分没花时不比——
    /// 从 0 涨上来说「多花了 100%」没有意义。
    var delta: Double? {
        guard lastExpense > 0 else { return nil }
        return expense - lastExpense
    }

    enum CodingKeys: String, CodingKey {
        case income, expense, days
        case lastExpense = "last_expense"
    }
}

struct LedgerView: Codable {
    var net: Double
    var assets: Double
    var debts: Double
    var accounts: [Account]
    var recent: [TxnBrief]
    var month = MonthFlow()
}

/// 健康记录整理好的样子。
///
/// FHIR 的解析全在 Go 那头做——这边再实现一遍 LOINC 查表、血压分项、
/// 单位换算，两边迟早对不上，而这份数据的全部意义就在于它是标准的。
/// 过桥拿到的每个字段都已经是能直接摆上屏幕的字符串。
struct HealthView: Codable {
    var profile: String
    var vitals: [HealthVital]
    var meals: [HealthEntry]
    var total: Int

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        profile = try c.decodeIfPresent(String.self, forKey: .profile) ?? ""
        vitals = try c.decodeIfPresent([HealthVital].self, forKey: .vitals) ?? []
        meals = try c.decodeIfPresent([HealthEntry].self, forKey: .meals) ?? []
        total = try c.decodeIfPresent(Int.self, forKey: .total) ?? 0
    }
}

struct HealthVital: Codable, Identifiable, Hashable {
    var kind = ""
    var value = ""
    var when = ""
    var ago = ""
    /// 跟上一次比的变化，如「+0.8kg」。只有一次记录时是空串——
    /// 拿第一次当基线画个 0 出来，看着像「没变化」，其实是没得比。
    var trend = ""
    /// 坐标轴上那个单位。
    var unit = ""
    /// 不带单位的那一串（「72.5」「124/81」）。
    /// 跟 value 分开给，是因为要把数字排成大号、单位排成小号。
    var number = ""
    /// 归到哪一类（身体测量 / 心脏 / 活动 …），界面按它分区配色。
    var group = ""
    /// 这个数是一天累计出来的（步数、睡眠）——画柱子，不画折线。
    /// 把累计量连成折线，等于在说两天之间的那些点也有意义。
    var isTotal = false
    /// 按时间升序的历史。少于两个点画不出线。
    var points: [HealthPoint] = []

    var id: String { kind }

    enum CodingKeys: String, CodingKey {
        case kind, value, when, ago, trend, unit, number, group, points
        case isTotal = "total_kind"
    }

    /// 两个点起才画得出趋势。
    var hasTrendLine: Bool { points.count >= 2 }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
        when = try c.decodeIfPresent(String.self, forKey: .when) ?? ""
        ago = try c.decodeIfPresent(String.self, forKey: .ago) ?? ""
        trend = try c.decodeIfPresent(String.self, forKey: .trend) ?? ""
        unit = try c.decodeIfPresent(String.self, forKey: .unit) ?? ""
        number = try c.decodeIfPresent(String.self, forKey: .number) ?? value
        group = try c.decodeIfPresent(String.self, forKey: .group) ?? ""
        isTotal = try c.decodeIfPresent(Bool.self, forKey: .isTotal) ?? false
        points = try c.decodeIfPresent([HealthPoint].self, forKey: .points) ?? []
    }
}

/// 趋势图上的一个点。血压一个点有两个值。
struct HealthPoint: Codable, Hashable, Identifiable {
    /// 时间戳（RFC3339）。给的是时刻不是日期——一天量两次体重，
    /// 只给日期的话两个点会叠在一起。
    var t = ""
    var v: Double = 0
    var v2: Double = 0

    var id: String { t }

    /// 解析好的时刻。
    ///
    /// **在解码时解一次**，不做成计算属性。图上可能有几百个点，而每个点
    /// 在一次 body 里要被读四五遍（过滤、连线、画点、列表）——现算的话
    /// 就是几万次 ISO8601 解析，切一下区间卡一下。
    let date: Date

    // v2 只有血压才有，Go 那边是 omitempty——绝大多数点根本没这个键。
    // 合成的解码器对非可选字段一律要求键存在（有默认值也不行），
    // 所以这里得自己来。契约测试逮到过。
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        t = try c.decodeIfPresent(String.self, forKey: .t) ?? ""
        v = try c.decodeIfPresent(Double.self, forKey: .v) ?? 0
        v2 = try c.decodeIfPresent(Double.self, forKey: .v2) ?? 0
        date = HealthPoint.parser.date(from: t) ?? .distantPast
    }

    enum CodingKeys: String, CodingKey { case t, v, v2 }

    /// 解析器建一次就够。每个点各建一个的话，一屏几百个点就是几百次
    /// DateFormatter 初始化——那玩意儿出了名的慢。
    private static let parser: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()
}

struct HealthEntry: Codable, Identifiable, Hashable {
    var when = ""
    var ago = ""
    var text = ""
    /// 精确时刻（RFC3339）。when 是给人看的（「09-18 12:30」），定位不能
    /// 用它——记错了要标掉时，得拿一个不会因为显示格式变化而漂的东西
    /// 去找那一条。
    var at = ""

    var id: String { when + text }

    var date: Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: at)
    }
}

/// 从「健康」App 导数据的结果。
struct HealthImport: Codable {
    /// 只在真有新增时才有——一条都没加就别让界面白写一遍文件。
    var health: String?
    var added = 0
    var skipped = 0
    var bad = 0
    var message = ""
    var error: String?
}

struct Snapshot: Codable {
    var days: [Day]
    var todos: [Item]
    /// 截断前的待办总数，比 todos.count 大就说明有没显示出来的。
    var todosTotal: Int = 0
    var ledger: LedgerView?
    var health: HealthView?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case days, todos, ledger, health, error
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
        health = try c.decodeIfPresent(HealthView.self, forKey: .health)
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

    var id: String { key }

    enum CodingKeys: String, CodingKey {
        case key, account, from, addr, subject, date, unread, snippet
        case verdict, reason
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

/// 附件只给清单不给内容——几 MB 的东西过一次 JSON 桥没有意义。
///
/// 刻意**不声明 Identifiable**：能拿来当 id 的只有名字，而同一封信里
/// 两个都叫「未命名.png」的附件很常见。声明了就等于承诺唯一，兑现不了
/// 的承诺会让 List 的差分算错行数直接崩。列表里按下标遍历即可。
struct MailAttachment: Codable, Hashable {
    var name = ""
    var type = ""
    var size = 0

    var sizeText: String {
        ByteCountFormatter.string(fromByteCount: Int64(size), countStyle: .file)
    }
}

// 合成的 Decodable **不会**用属性默认值，缺一个键就抛错、整个出参作废。
// MailItem 已经手写了容错解码，这两个当时漏了——防了一层没防另一层。
extension MailResult {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        items = try c.decodeIfPresent([MailItem].self, forKey: .items) ?? []
        body = try c.decodeIfPresent(MailBody.self, forKey: .body)
        message = try c.decodeIfPresent(String.self, forKey: .message) ?? ""
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}

extension MailBody {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        html = try c.decodeIfPresent(String.self, forKey: .html) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        attachments = try c.decodeIfPresent([MailAttachment].self,
                                            forKey: .attachments) ?? []
        inlineDropped = try c.decodeIfPresent(Int.self, forKey: .inlineDropped) ?? 0
    }
}

extension MailAttachment {
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        type = try c.decodeIfPresent(String.self, forKey: .type) ?? ""
        size = try c.decodeIfPresent(Int.self, forKey: .size) ?? 0
    }
}

/// 传给 Go 的邮件入参。照 AskInput 的样子走 Codable，一次编码到位——
/// 手拼 [String: Any] 要把 accounts 和 model 各自编一次、解一次、再整体
/// 编一次，而且 try? 失败时会把字段悄悄丢掉（accounts 没了就是「还没配邮箱」）。
struct MailInput: Codable {
    var op: String
    var indexDir: String
    var bodyDir: String
    var accounts: [MailAccountInput]
    var model: ModelConfig
    var profile: String
    var key: String
    var keys: [String]

    enum CodingKeys: String, CodingKey {
        case op, accounts, model, profile, key, keys
        case indexDir = "index_dir"
        case bodyDir = "body_dir"
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

/// 认小票的结果。**都要拿给人过目**，不直接落账。
struct ReceiptRead: Codable {
    var amount: Double = 0
    var payee = ""
    var date = ""
    /// 猜的支出科目，只会是账本里已有的那些之一；拿不准就是空的。
    var account = ""
    var error: String?
}

/// 标「录错了」的结果。health 只在真改过之后才有。
struct VoidHealthResult: Codable {
    var message = ""
    var health: String?
    var error: String?
}

/// 记一笔账的结果。ledger 只在真写进去之后才有，跟 ActResult 一个规矩。
struct SpendResult: Codable {
    var message = ""
    var ledger: String?
    var error: String?
}

/// 邮箱连通性自检的结果。字段跟 mobile.TestMailOutput 对应。
struct MailCheck: Codable {
    var ok = false
    /// 登录后能看到几个信箱。能列出来说明登录真的成了，不只是连上了。
    var boxes = 0
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
    /// 这份文件认得的 TODO 关键字，顺序就是状态循环的顺序。由 Go 给——
    /// 关键字是每份 org 文件用 `#+TODO:` 自己定的，这边不可能知道。
    var states: [String] = []
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

/// 界面上的一条对话。
///
/// **跟传给引擎的 `Message` 是两回事，这是有意分开的。**
///
/// 一份结构同时伺候两边，迟早要往里塞对方不认的字段——图片编号就是这么
/// 一路塞到 Go 的 agent.Message 上，再靠桥接层「找到最近一条 user 消息
/// 缝回去」。那不是设计，是缝合。
///
/// 现在各管各的：
///   - 这份归界面，带附件、稳定 id，**引擎改不了它**；
///   - `Message` 归引擎，纯 OpenAI wire 格式，可以被压缩裁剪。
///
/// 于是图片编号根本不用出门。压缩也只动引擎那份，用户翻上去还是完整的
/// 对话——早先 `messages = out.history` 把两者混作一谈，压缩一触发，
/// 人说过的话就在屏幕上变成了一句摘要。
struct ChatMessage: Identifiable, Codable, Hashable {
    var id = UUID()
    /// user / assistant / __local（我们自己插的提示，只显示不外发）
    var role: String
    var text: String
    /// 配图在本机的编号，图本身在 ImageStore 里。
    var imageRefs: [String] = []

    var isUser: Bool { role == "user" }

    init(id: UUID = UUID(), role: String, text: String, imageRefs: [String] = []) {
        self.id = id
        self.role = role
        self.text = text
        self.imageRefs = imageRefs
    }

    // 老存档里没有 id 和 imageRefs，缺了给默认值，别让整份记录解不开。
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(UUID.self, forKey: .id) ?? UUID()
        role = try c.decodeIfPresent(String.self, forKey: .role) ?? "assistant"
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
        imageRefs = try c.decodeIfPresent([String].self, forKey: .imageRefs) ?? []
    }

    enum CodingKeys: String, CodingKey {
        case id, role, text
        case imageRefs = "image_refs"
    }
}

/// 传给引擎的一条消息，字段跟 OpenAI 对齐。界面不直接用它——见 ChatMessage。
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
    /// 工具名的中文说法，直接显示这个。
    ///
    /// 由 Go 查好了送过来。这边原先自己维护着一张对照表，跟桌面端那张
    /// 无人对账——结果它留着早就删掉的 read_mail、挂着五个根本不存在的
    /// 工具，而新加的健康那四个一个都没补。表现在只有一份。
    var label: String = ""
    var text: String
    var id: String { kind + name + text }

    /// 没有标签（旧 framework）就退回工具名，总比空着强。
    var display: String { label.isEmpty ? name : label }

    init(kind: String, name: String, label: String, text: String) {
        self.kind = kind
        self.name = name
        self.label = label
        self.text = text
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text) ?? ""
    }
}

/// 一个服务商的预设，由 Go 那边的 Presets 表过桥来。
struct ProviderInfo: Codable, Identifiable, Hashable {
    var name = ""
    var model = ""
    var baseURL = ""
    /// 预设的那个默认模型认不认得图片。手机是能发图、能拍照记账的那一端，
    /// 选了纯文本模型，图发出去石沉大海，最难查。
    var vision = false
    /// 本地跑的（ollama），不用填密钥。
    var local = false

    var id: String { name }

    enum CodingKeys: String, CodingKey {
        case name, model, vision, local
        case baseURL = "base_url"
    }
}

struct ProviderList: Codable {
    var providers: [ProviderInfo] = []
    var error = ""
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
    /// 健康记录全文（FHIR NDJSON，一行一条资源）。
    var health: String = ""
    /// 邮箱账号。空的话这一轮就没有邮件能力。
    var accounts: [MailAccountInput] = []
    /// 这一轮随话一起发的图片（data URL）。
    ///
    /// **只跟这一轮走，不进会话存档**：图片按 token 算贵得多，存进历史
    /// 每轮都重发一遍，几轮下来就把上下文撑爆了、钱也白花。
    var images: [String] = []
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
    /// 只在健康记录被追加过时才有。
    var health: String?
    /// 这一轮是被叫停的，不是自己跑完的。
    var stopped: Bool?
    var error: String?
}
