import SwiftUI
import UniformTypeIdentifiers

// 界面分四块：日程、待办、财务、对话。
// 手机屏幕小，每块只放最要紧的东西，详情靠点进去看。

struct RootView: View {
    @StateObject private var store = Store()
    @Environment(\.scenePhase) private var phase

    var body: some View {
        Group {
            if store.isConfigured {
                MainShell()
            } else {
                WelcomeView()
            }
        }
        .environmentObject(store)
        .tint(Brand.primary)
        // 图标那个人是圆润的软塑感，字跟着圆一点才像一家人。
        .fontDesign(.rounded)
        .task {
            guard store.isConfigured else { return }
            // 提醒默认开着，但权限得当面要一次——不要的话开关开着也白搭，
            // 而且用户会以为是 app 坏了。
            //
            // **不能等它**：requestAuthorization 会一直挂到用户点了为止，
            // 写成 await 的话，弹窗没点之前一个字都不会加载，界面就那么空着。
            if store.settings.remind {
                Task { _ = await Notifier.requestPermission() }
            }
            await store.reload()
        }
        // 切回前台重读一次。「跟电脑上是同一份」这句话得落到实处——
        // 在 Mac 上改完、拿起手机看到的还是旧的，那就不算同一份。
        // reload 内部会合并并发请求，跟首次进来那次撞上也不会读两遍。
        .onChange(of: phase) { _, now in
            guard now == .active, store.isConfigured else { return }
            Task { await store.reload() }
        }
    }
}

// MARK: - 还没选目录

struct WelcomeView: View {
    @EnvironmentObject var store: Store
    @State private var picking = false

    var body: some View {
        VStack(spacing: 20) {
            Spacer()
            Avatar(size: 140)
                .shadow(color: Brand.primary.opacity(0.25), radius: 18, y: 8)
            VStack(spacing: 8) {
                Wordmark(font: .largeTitle)
                Text(Brand.slogan)
                    .font(.title3)
                    .foregroundStyle(Brand.primary)
            }
            Text(Brand.subtitle)
                .font(.footnote)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 32)

            Button("选 iCloud 里的 Jarvis 目录") { picking = true }
                .buttonStyle(.glassProminent)

            Text("iCloud Drive → Jarvis")
                .font(.footnote)
                .foregroundStyle(.tertiary)
            Spacer()
        }
        .padding()
        .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
            if case .success(let url) = result {
                Task { await store.chooseVault(url) }
            }
        }
    }
}

// MARK: - 主界面

/// 根视图：**一整屏对话**。
///
/// 原来是四个并排的标签页，Jarvis 占第一个。标签栏最多五格，功能只会
/// 越来越多；更要紧的是，它把「说一句」和「翻一翻」摆成了平级的五选一，
/// 可这个 app 的主交互从来只有一个。
///
/// 现在对话就是底座，日程、健康、邮件、财务浮在输入框上面那一排，
/// 点开是盖上来的一张卡片。见 Tool。
struct MainShell: View {
    var body: some View {
        NavigationStack { ChatView() }
    }
}

// MARK: - 日程

/// 日程只有一个入口，里面两种看法。
///
/// 原来是并排两个 tab，而实际数据里它们是同一批东西：用户的 11 条未完成
/// 全都排了期，「没排期」一条都没有，于是待办就是日程摊平的样子，
/// 今天那组跟日程的今天 100% 重合。
///
/// 滴答清单、Todoist、Notion Calendar 都是「一个入口 + 视图切换」，
/// 并排两个清单展示同一批数据才是特例。合并之后标签也从四个减到三个。
struct PlannerView: View {
    @EnvironmentObject var store: Store
    @AppStorage("jarvis.planner.mode") private var raw = Mode.days.rawValue

    enum Mode: String, CaseIterable {
        case days = "按天"
        case list = "清单"
    }

    private var mode: Binding<Mode> {
        Binding(get: { Mode(rawValue: raw) ?? .days }, set: { raw = $0.rawValue })
    }

    var body: some View {
        Group {
            if mode.wrappedValue == .days {
                AgendaView()
            } else {
                TodosView()
            }
        }
        .navigationTitle("日程")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await store.reload() }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .toolbar {
            ToolbarItem(placement: .principal) {
                Picker("", selection: mode) {
                    ForEach(Mode.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .frame(width: 160)
            }
            SettingsLink()
            QuickAddButton()
        }
    }
}

struct AgendaView: View {
    @EnvironmentObject var store: Store
    @State private var selected: Item?

    var body: some View {
        List {
            ForEach(store.snapshot.days) { day in
                Section {
                    if day.items.isEmpty {
                        Text("空着").foregroundStyle(.tertiary).font(.footnote)
                    }
                    ForEach(Array(day.items.enumerated()), id: \.offset) { _, item in
                        Button { selected = item } label: { ItemRow(item: item) }
                            .buttonStyle(.plain)
                            .itemSwipeActions(item)
                    }
                } header: {
                    HStack {
                        Text("\(day.date.suffix(5)) \(day.weekday)")
                        if day.today {
                            Text("今天")
                                .font(.caption2.bold())
                                .padding(.horizontal, 6).padding(.vertical, 2)
                                .background(Brand.sheen, in: Capsule())
                                .foregroundStyle(.white)
                        }
                    }
                }
            }
        }
        // 标题、刷新、工具栏、错误条都归 PlannerView 管——两边都写一遍的话
        // 工具栏会出现两个 + 和两个齿轮。
        .overlay { EmptyHint(show: store.snapshot.days.isEmpty && !store.loading,
                                 text: "这几天没有安排", icon: "calendar") }
        .overlay { LoadingHint(isEmpty: store.snapshot.days.isEmpty) }
        // 整个列表共用一个详情页。挂在每一行上的话，有多少行就建多少套
        // 呈现状态，滚起来明显发涩。
        .sheet(item: $selected) { ItemDetail(item: $0) }
    }
}

// MARK: - 待办

/// 待办按「什么时候要做」分组。
///
/// 原来是一长条，今天该干的和下个月才到期的混在一起——名义上按优先级排过，
/// 但一条 C 级的今天要交，排在一条 A 级的下月任务后面，等于没排。
enum TodoBucket: Int, CaseIterable {
    // done 排在最后：做完的事该往下沉，不能跟还没办的抢位置。
    case overdue, today, tomorrow, week, later, someday, done

    var title: String {
        switch self {
        case .overdue:  return "逾期"
        case .today:    return "今天"
        case .tomorrow: return "明天"
        case .week:     return "这周内"
        case .later:    return "以后"
        case .someday:  return "没排期"
        case .done:     return "最近完成"
        }
    }

    var tint: Color {
        switch self {
        case .overdue:  return .red
        case .today:    return .orange
        case .tomorrow: return .blue
        case .done:     return .green
        default:        return .secondary
        }
    }

    var icon: String {
        switch self {
        case .overdue:  return "exclamationmark.triangle.fill"
        case .today:    return "sun.max.fill"
        case .tomorrow: return "sunrise.fill"
        case .week:     return "calendar"
        case .later:    return "calendar.badge.clock"
        case .someday:  return "tray"
        case .done:     return "checkmark.circle.fill"
        }
    }

    static func of(_ item: Item) -> TodoBucket {
        // 先看做完没有。做完的按到期日归组没有意义——早就不用管了。
        if item.isDone { return .done }
        // 逾期用 Go 给的那面旗，不在这边按 dueIn 自己推——
        // 同一个概念两个定义，迟早不一致（Go 那边的规则还带着截止日当天）。
        if item.overdue { return .overdue }
        guard !item.due.isEmpty else { return .someday }
        switch item.dueIn {
        case ..<0:  return .overdue
        case 0:     return .today
        case 1:     return .tomorrow
        case 2...6: return .week
        default:    return .later
        }
    }
}

struct TodosView: View {
    @EnvironmentObject var store: Store
    @State private var keyword = ""
    @State private var groups: [(bucket: TodoBucket, items: [Item])] = []
    @State private var selected: Item?
    @AppStorage("jarvis.todos.showDone") private var showDone = false

    private func header(_ group: (bucket: TodoBucket, items: [Item])) -> some View {
        HStack(spacing: 6) {
            Image(systemName: group.bucket.icon).font(.caption2)
            Text(group.bucket.title)
            Text("\(group.items.count)")
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.secondary)
        }
        .foregroundStyle(group.bucket.tint)
    }

    var body: some View {
        List {
            ForEach(groups, id: \.bucket) { group in
                Section {
                    if group.bucket != .done || showDone {
                        ForEach(Array(group.items.enumerated()), id: \.offset) { _, item in
                            Button { selected = item } label: { ItemRow(item: item) }
                                .buttonStyle(.plain)
                                .itemSwipeActions(item)
                        }
                    }
                } header: {
                    // 「最近完成」默认收起：这一组只是让人确认没白干，
                    // 展开着会把还没办的挤到看不见的地方。
                    if group.bucket == .done {
                        Button {
                            withAnimation { showDone.toggle() }
                        } label: {
                            HStack(spacing: 6) {
                                header(group)
                                Spacer(minLength: 0)
                                Image(systemName: showDone ? "chevron.down" : "chevron.right")
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .buttonStyle(.plain)
                    } else {
                        header(group)
                    }
                }
            }
        }
        .searchable(text: $keyword, prompt: "找点什么")
        .overlay { EmptyHint(show: groups.isEmpty && !store.loading,
                                 text: "没有待办", icon: "checklist") }
        .overlay { LoadingHint(isEmpty: groups.isEmpty) }
        .safeAreaInset(edge: .bottom) { truncatedHint }
        .sheet(item: $selected) { ItemDetail(item: $0) }
        // 过滤和分组放进 task：写成计算属性的话，每次 body 重算都要把整份
        // 待办再过一遍。id 变了旧任务会被取消，顺带就是输入防抖。
        .task(id: FilterKey(keyword: keyword, count: store.snapshot.todos.count)) {
            await regroup()
        }
    }

    private struct FilterKey: Equatable {
        var keyword: String
        var count: Int
    }

    private func regroup() async {
        let k = keyword.trimmingCharacters(in: .whitespaces)
        if !k.isEmpty {
            // 打字没停就别急着过滤；被取消了就什么都不做。
            try? await Task.sleep(for: .milliseconds(180))
            if Task.isCancelled { return }
        }
        var shown = store.snapshot.todos
        if !k.isEmpty {
            shown = shown.filter {
                $0.title.localizedCaseInsensitiveContains(k)
                    || $0.tags.contains { $0.localizedCaseInsensitiveContains(k) }
                    || $0.category.localizedCaseInsensitiveContains(k)
            }
        }
        var byBucket: [TodoBucket: [Item]] = [:]
        for item in shown {
            byBucket[TodoBucket.of(item), default: []].append(item)
        }
        groups = TodoBucket.allCases.compactMap { b in
            guard let items = byBucket[b], !items.isEmpty else { return nil }
            // 组内先按优先级，同级按日期近的在前。Go 那边已经按优先级排过，
            // 这里只需在同级里把日期理顺。
            return (b, items.sorted { a, c in
                let pa = priorityRank(a.priority), pc = priorityRank(c.priority)
                if pa != pc { return pa < pc }
                if a.due.isEmpty != c.due.isEmpty { return !a.due.isEmpty }
                return a.due < c.due
            })
        }
    }

    /// A 最急，没标的排最后。
    private func priorityRank(_ p: String) -> Int {
        p.isEmpty ? 99 : Int(p.unicodeScalars.first!.value)
    }

    /// 待办被截断时说清楚，否则找不到某一条会以为是丢了。
    @ViewBuilder
    private var truncatedHint: some View {
        let s = store.snapshot
        if s.todosTotal > s.todos.count {
            HintPill(text: "共 \(s.todosTotal) 条，只显示前 \(s.todos.count) 条")
        }
    }
}

// MARK: - 财务

struct LedgerTab: View {
    @EnvironmentObject var store: Store
    /// 展开过就记住——每次进来都要再点一下，比不收起来还烦。
    @AppStorage("jarvis.ledger.showTxns") private var showTxns = false

    var body: some View {
        Group {
            if let l = store.snapshot.ledger {
                List {
                    Section {
                        // 三个并排在手机上每个只有一百多点宽，数字一到七八位
                        // 就得缩到看不清。净资产是最该一眼看到的，单独占一行
                        // 给足宽度；另外两个并排够用。
                        GlassEffectContainer(spacing: 10) {
                            VStack(spacing: 10) {
                                Stat(name: "净资产", value: l.net, emphasize: true)
                                HStack(spacing: 10) {
                                    Stat(name: "资产", value: l.assets)
                                    Stat(name: "负债", value: -l.debts)
                                }
                            }
                        }
                        .listRowInsets(EdgeInsets(top: 8, leading: 8,
                                                  bottom: 8, trailing: 8))
                        .listRowBackground(Color.clear)
                    }
                    // 按科目分组。原来把 Income:Salary 跟银行卡并排列在「账户」
                    // 里，看着像是有这么个账户——那是记账科目，不是钱放在哪儿。
                    ForEach(accountGroups(l.accounts), id: \.title) { group in
                        Section(group.title) {
                            ForEach(Array(group.items.enumerated()), id: \.offset) { AccountRow(account: $1) }
                        }
                    }
                    // 流水默认收起来。打开财务页多半是想看「还剩多少」，
                    // 而流水能有几十上百条，一进来就糊一屏，净资产反倒被挤走了。
                    Section {
                        Button {
                            withAnimation(.snappy) { showTxns.toggle() }
                        } label: {
                            HStack {
                                Label("最近流水", systemImage: "list.bullet.rectangle")
                                Spacer()
                                Text("\(l.recent.count) 笔")
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                                Image(systemName: showTxns ? "chevron.up" : "chevron.down")
                                    .font(.caption).foregroundStyle(.tertiary)
                            }
                        }
                        .buttonStyle(.plain)
                    }

                    if showTxns {
                        // 按天分组。一条一条平铺的话，日期在每行重复一遍，
                        // 而「今天花了多少」这个最常问的问题反而得自己加。
                        ForEach(dayGroups(l.recent), id: \.date) { day in
                            Section {
                                ForEach(Array(day.items.enumerated()), id: \.offset) { TxnRow(txn: $1) }
                            } header: {
                                HStack {
                                    Text(dayTitle(day.date))
                                    Spacer()
                                    if day.spent > 0 {
                                        Text("支出 " + moneyRound(day.spent))
                                            .font(.caption.monospacedDigit())
                                            .foregroundStyle(.secondary)
                                    }
                                }
                            }
                        }
                    }
                }
            } else {
                EmptyHint(show: !store.loading, text: "没有账本", icon: "book.closed")
            }
        }
        .navigationTitle("财务")
        .refreshable { await store.reload() }
        .overlay { LoadingHint(isEmpty: store.snapshot.ledger == nil) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .toolbar { SettingsLink() }
    }
}

/// 账户按科目归类。顺序是「钱在哪儿 → 欠多少 → 花在哪儿 → 挣多少」，
/// 最常看的排前面；权益和认不出来的放最后。
func accountGroups(_ accounts: [Account]) -> [(title: String, items: [Account])] {
    let order: [(String, String)] = [
        ("asset", "账户"), ("debt", "负债"),
        ("expense", "支出"), ("income", "收入"),
        ("equity", "权益"), ("other", "其他"),
    ]
    return order.compactMap { kind, title in
        let items = accounts.filter { $0.kind == kind }
        return items.isEmpty ? nil : (title, items)
    }
}

/// 把流水按天归拢，顺带算出当天花了多少。
///
/// 转账不计入支出——钱从招行挪到余额宝不是花掉了，混进去会把当天的账
/// 撑得莫名其妙。
func dayGroups(_ txns: [TxnBrief]) -> [(date: String, items: [TxnBrief], spent: Double)] {
    var order: [String] = []
    var byDay: [String: [TxnBrief]] = [:]
    for t in txns {
        if byDay[t.date] == nil { order.append(t.date) }
        byDay[t.date, default: []].append(t)
    }
    return order.map { d in
        let items = byDay[d] ?? []
        let spent = items.filter { !$0.isTransfer && $0.amount < 0 }
            .reduce(0) { $0 - $1.amount }
        return (d, items, spent)
    }
}

/// 日期写成人话。今天昨天用字，再往前用月日——「2026-09-12」要在脑子里
/// 算一下才知道是几天前。
func dayTitle(_ date: String) -> String {
    let f = DateFormatter()
    f.locale = Locale(identifier: "en_US_POSIX")
    f.dateFormat = "yyyy-MM-dd"
    guard let d = f.date(from: date) else { return date }
    let cal = Calendar.current
    if cal.isDateInToday(d) { return "今天" }
    if cal.isDateInYesterday(d) { return "昨天" }
    let days = cal.dateComponents([.day], from: d, to: Date()).day ?? 0
    if days < 7 {
        let names = ["周日", "周一", "周二", "周三", "周四", "周五", "周六"]
        return names[cal.component(.weekday, from: d) - 1]
    }
    return String(date.suffix(5).replacingOccurrences(of: "-", with: "/"))
}

struct TxnRow: View {
    let txn: TxnBrief

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon)
                .font(.footnote)
                .foregroundStyle(tint)
                .frame(width: 20)

            VStack(alignment: .leading, spacing: 1) {
                Text(txn.payee).font(.callout).lineLimit(1)
                if !txn.category.isEmpty {
                    Text(txn.category)
                        .font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }

            Spacer(minLength: 8)

            // 收入带正号，支出带负号——光看数字分不出方向。
            Text(signed)
                .font(.callout.monospacedDigit())
                .foregroundStyle(tint)
                .lineLimit(1).minimumScaleFactor(0.7)
        }
    }

    private var signed: String {
        txn.isTransfer ? money(abs(txn.amount))
                       : (txn.amount > 0 ? "+" : "") + money(txn.amount)
    }

    private var icon: String {
        if txn.isTransfer { return "arrow.left.arrow.right" }
        return txn.isIncome ? "arrow.down.circle.fill" : "arrow.up.circle.fill"
    }

    private var tint: Color {
        if txn.isTransfer { return .secondary }
        return txn.isIncome ? .green : Brand.onSoft
    }
}

struct AccountRow: View {
    let account: Account

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 1) {
                Text(account.leaf).font(.callout).lineLimit(1)
                if !account.parentPath.isEmpty {
                    Text(account.parentPath)
                        .font(.caption2).foregroundStyle(.tertiary).lineLimit(1)
                }
            }
            Spacer(minLength: 8)
            Text(money(account.balance))
                .font(.callout.monospacedDigit())
                .lineLimit(1).minimumScaleFactor(0.7)
                .foregroundStyle(account.balance < 0 ? Color.red : Color.primary)
        }
    }
}

struct Stat: View {
    let name: String
    let value: Double
    var emphasize = false

    var body: some View {
        VStack(spacing: 4) {
            Text(name).font(.caption).foregroundStyle(.secondary)
            Text(moneyRound(value))
                .font(emphasize ? .largeTitle.bold().monospacedDigit()
                                : .title3.monospacedDigit())
                .foregroundStyle(emphasize ? AnyShapeStyle(Brand.sheen)
                                           : AnyShapeStyle(Color.primary))
                .lineLimit(1)
                .minimumScaleFactor(0.4)
        }
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity)
        // 净资产是这页最该先看到的，给它一点色，另外两个保持素净。
        .glassEffect(emphasize ? .regular.tint(Brand.soft) : .regular,
                     in: .rect(cornerRadius: Brand.radius))
    }
}

/// 列表里的金额：千分位分组、两位小数。
/// v == 0 那下是把 -0.00 拍平成 0.00。
func money(_ v: Double) -> String {
    let n = v == 0 ? 0 : v
    return Formatter.money2.string(from: NSNumber(value: n))
        ?? String(format: "%.2f", n)
}

/// 概览上那三个数字用这个：千分位分组、不要小数。
///
/// 分组是为了一眼数得清位数——231891 和 23189 不分组扫一眼分不出差一个零。
/// 小数在概览里没意义，还白占宽度，精确到分的数字在下面账户列表里。
/// 概览那几个数字用这个：大了自动换成万、亿。
///
/// 12,504,569 得一位一位数着看才知道量级；写成 1250.46万 扫一眼就清楚。
/// 概览要的正是量级，精确到分的数字在下面账户列表里。
func moneyRound(_ v: Double) -> String {
    let n = v == 0 ? 0 : v
    let a = abs(n)
    if a >= 1e8 { return trimZeros(n / 1e8) + "亿" }
    if a >= 1e4 { return trimZeros(n / 1e4) + "万" }
    return Formatter.moneyGrouping.string(from: NSNumber(value: n.rounded()))
        ?? String(format: "%.0f", n)
}

/// 两位小数，末尾的零去掉：1250.46 / 3.5 / 12（而不是 12.00）。
func trimZeros(_ v: Double) -> String {
    var s = String(format: "%.2f", v)
    if s.contains(".") {
        while s.hasSuffix("0") { s.removeLast() }
        if s.hasSuffix(".") { s.removeLast() }
    }
    return s
}

extension Formatter {
    static let moneyGrouping = decimal(fraction: 0)
    static let money2 = decimal(fraction: 2)

    private static func decimal(fraction: Int) -> NumberFormatter {
        let f = NumberFormatter()
        f.numberStyle = .decimal
        f.minimumFractionDigits = fraction
        f.maximumFractionDigits = fraction
        f.groupingSeparator = ","
        return f
    }
}

// MARK: - 对话

struct ChatView: View {
    @EnvironmentObject var store: Store
    @State private var draft = ""
    @State private var openSettings = false
    @State private var tool: Tool?
    @FocusState private var focused: Bool

    var body: some View {
        // 输入区用 safeAreaInset 浮在内容之上，不是拿 VStack 摞在下面。
        // 摞着的话玻璃背后什么都没有，只能是一块白板——跟别的页面里
        // 内容从标签栏底下穿过去的样子对不上，看着很突兀。
        transcript
            .safeAreaInset(edge: .bottom, spacing: 0) { inputBar }
            .navigationTitle("Jarvis")
            .navigationBarTitleDisplayMode(.inline)
            // 秘书要做收不回来的事（比如发信）会停在这儿等。
            .sheet(item: $store.pending) { ConfirmSheet(ask: $0) }
            .sheet(item: $tool) { ToolSheet(tool: $0) }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button { store.clearChat() } label: {
                        Image(systemName: "trash")
                    }
                    .disabled(store.messages.isEmpty)
                    .accessibilityLabel("清空")
                }
                SettingsLink()
            }
    }

    // MARK: - 对话记录

    private var transcript: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if shownMessages.isEmpty && !store.thinking {
                        VStack(alignment: .leading, spacing: 16) {
                            BrandHeader(size: 44)
                            Starters(pick: startWith)
                        }
                        .padding(.top, 20)
                    }
                    ForEach(Array(shownMessages.enumerated()), id: \.offset) { _, m in
                        Bubble(message: m)
                    }
                    if store.thinking {
                        ThinkingRow(steps: store.steps)
                    }
                    Color.clear.frame(height: 1).id("bottom")
                }
                .padding()
            }
            // 一开始滚就收键盘。.interactively 要手指往下拖着才收，
            // 而这里想要的是「去看上面说了什么」，滚动本身就是意思。
            .scrollDismissesKeyboard(.immediately)
            // 内容滚到输入区底下时柔和淡出，不然是硬生生被玻璃切一刀。
            .scrollEdgeEffectStyle(.soft, for: .bottom)
            // 恢复出来的对话落在底部，不然打开看到的是最早那几句。
            // 但空着的时候要顶到上面——否则那几句例句会被压到屏幕最下方，
            // 上面留一大片空白。
            .defaultScrollAnchor(shownMessages.isEmpty ? .top : .bottom)
            .onChange(of: store.messages.count) { _, _ in toBottom(proxy) }
            .onChange(of: store.steps.count) { _, count in
                // 开始动手了就把键盘收掉——这会儿是要看它干了什么，
                // 而不是接着打字。只在第一步冒出来时收，之后不打扰。
                if count == 1 { focused = false }
                toBottom(proxy)
            }
            .onChange(of: store.thinking) { _, _ in toBottom(proxy) }
        }
    }

    private func toBottom(_ proxy: ScrollViewProxy) {
        withAnimation { proxy.scrollTo("bottom", anchor: .bottom) }
    }

    // MARK: - 输入区

    private var inputBar: some View {
        VStack(spacing: 0) {
            if needsKey {
                Button {
                    openSettings = true
                } label: {
                    Label("还没填模型密钥，点这里去设置", systemImage: "key")
                        .font(.footnote)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .padding(.horizontal, 16).padding(.top, 8)
            }
            // 打字的时候把这排收起来。小屏上键盘一弹，光输入框就占掉半屏，
            // 再压着一排胶囊，能看见的对话只剩两行。
            if !focused {
                ToolStrip { tool = $0 }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            // 输入区浮在内容之上，不再用一条实心底栏压着——下面的话能透出来，
            // 屏幕显得高一些。GlassEffectContainer 让挨着的两块玻璃算作一体，
            // 各自算各自的会在缝隙处叠出一道亮边。
            GlassEffectContainer(spacing: 8) {
                HStack(alignment: .bottom, spacing: 8) {
                    // 想说的话可能不止一行，让它自己长，最多六行。
                    TextField("说点什么", text: $draft, axis: .vertical)
                        .lineLimit(1...6)
                        .padding(.horizontal, 16)
                        .padding(.vertical, 11)
                        .glassEffect(.regular, in: .capsule)
                        .focused($focused)

                    Button(action: submit) {
                        Image(systemName: store.thinking ? "ellipsis" : "arrow.up")
                            .font(.system(size: 17, weight: .bold))
                            .foregroundStyle(canSend ? Color.white : Color.secondary)
                            .frame(width: 38, height: 38)
                            .contentTransition(.symbolEffect(.replace))
                    }
                    .glassEffect(canSend ? .regular.tint(Brand.primary).interactive()
                                         : .regular,
                                 in: .circle)
                    .disabled(!canSend)
                    .animation(.easeInOut(duration: 0.18), value: canSend)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .animation(.easeOut(duration: 0.2), value: focused)
        .sheet(isPresented: $openSettings) { SettingsView() }
    }

    private var needsKey: Bool { store.settings.apiKey.isEmpty }

    private var canSend: Bool {
        !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !store.thinking
    }

    private func submit() {
        let q = draft
        draft = ""
        // 不在这儿收键盘：刚发完还想接着补一句是常事。
        // 等它真动起手来（操作列表冒出来）再收，那会儿是要看它干什么。
        Task { await store.send(q) }
    }

    private func startWith(_ text: String) {
        draft = text
        focused = true
    }

    // 工具调用和系统提示那些中间消息不用给人看。
    // __local 是我们自己插的提示（比如出错说明），显示但不喂回给模型。
    var shownMessages: [Message] {
        store.messages.filter {
            ["user", "assistant", "__local"].contains($0.role) && !$0.content.isEmpty
        }
    }
}

/// 空着的时候给几句现成的，省得第一次打开不知道能说什么。
/// 点一下只是填进输入框，不直接发——让人有机会改。
struct Starters: View {
    let pick: (String) -> Void

    private let examples = [
        "今天还有什么要做的？",
        "把明天上午空出来",
        "这周花了多少钱",
        "记一笔：午饭 35 块",
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("可以这么使唤它")
                .font(.footnote)
                .foregroundStyle(.secondary)
            ForEach(examples, id: \.self) { e in
                Button { pick(e) } label: {
                    Text(e)
                        .font(.callout)
                        .foregroundStyle(Brand.onSoft)
                        .padding(.horizontal, 14).padding(.vertical, 9)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(Brand.soft).interactive(), in: .capsule)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct Bubble: View {
    @EnvironmentObject var store: Store
    let message: Message

    var isUser: Bool { message.role == "user" }

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            if isUser {
                Spacer(minLength: 40)
            } else {
                Avatar(size: 26).padding(.top, 2)
            }
            Group {
                if isUser {
                    Text(message.content)
                } else {
                    MarkdownText(text: message.content)
                }
            }
                .padding(10)
                .background(isUser ? Brand.primary : Brand.soft,
                            in: RoundedRectangle(cornerRadius: 14))
                .foregroundStyle(isUser ? Color.white : Brand.onSoft)
                .textSelection(.enabled)
                // 长按：抄走，或者换个说法再问一遍。重发对用户自己那句
                // 尤其有用——秘书会错意时，改一个词比重打一整句快得多。
                .contextMenu {
                    Button {
                        UIPasteboard.general.string = message.content
                        UIImpactFeedbackGenerator(style: .light).impactOccurred()
                    } label: {
                        Label("复制", systemImage: "doc.on.doc")
                    }
                    if isUser {
                        Button {
                            Task { await store.send(message.content) }
                        } label: {
                            Label("再问一遍", systemImage: "arrow.clockwise")
                        }
                        .disabled(store.thinking)
                    }
                }
            if !isUser { Spacer(minLength: 40) }
        }
    }
}

/// 干活过程。原来只显示最后一步，清理邮件那种「一口气动了好几封」的事
/// 结束后完全看不出它到底动了什么。现在整条轨迹都留着，点一下展开。
struct ThinkingRow: View {
    @EnvironmentObject var store: Store
    let steps: [Step]
    @State private var expanded = false

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Avatar(size: 26)

            VStack(alignment: .leading, spacing: 6) {
                Button { expanded.toggle() } label: {
                    HStack(spacing: 6) {
                        ProgressView().controlSize(.small)
                        Text(headline).font(.caption).foregroundStyle(.secondary)
                        if steps.count > 1 {
                            Image(systemName: expanded ? "chevron.up" : "chevron.down")
                                .font(.caption2).foregroundStyle(.tertiary)
                        }
                    }
                }
                .buttonStyle(.plain)

                if expanded {
                    VStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(steps.enumerated()), id: \.offset) { StepLine(step: $1) }
                    }
                    .padding(.leading, 2)
                }
            }

            Spacer(minLength: 8)

            // 一轮可能要跑一分钟。中途改主意总得能喊停——桌面端按 esc。
            Button { store.stopThinking() } label: {
                Image(systemName: "stop.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 26, height: 26)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: .circle)
            .accessibilityLabel("停下")
        }
    }

    private var headline: String {
        guard let last = steps.last else { return "想想…" }
        let n = steps.count > 1 ? "（第 \(steps.count) 步）" : ""
        return label(for: last) + n
    }

    func label(for step: Step) -> String {
        step.kind == "result" ? step.text : step.display
    }
}

/// 过程里的一行。做了什么用主色，做完的结果用次要色，出错标红，
/// 自动做掉的（清理邮件那类可逆动作）单独标出来——那些没问过你。
struct StepLine: View {
    let step: Step

    var body: some View {
        HStack(alignment: .top, spacing: 5) {
            Image(systemName: icon)
                .font(.system(size: 9))
                .foregroundStyle(color)
                .padding(.top, 3)
            Text(text)
                .font(.caption2)
                .foregroundStyle(step.kind == "error" ? Color.red : .secondary)
                .lineLimit(2)
        }
    }

    private var text: String {
        switch step.kind {
        case "tool":  return step.display
        case "auto":  return step.display + "：" + step.text + "（没问你，可逆）"
        default:      return step.text.isEmpty ? step.display : step.text
        }
    }

    private var icon: String {
        switch step.kind {
        case "tool":   return "circle.fill"
        case "auto":   return "bolt.fill"
        case "error":  return "exclamationmark.triangle.fill"
        default:       return "checkmark"
        }
    }

    private var color: Color {
        switch step.kind {
        case "tool":  return Brand.primary
        case "auto":  return .orange
        case "error": return .red
        default:      return .green
        }
    }
}

// MARK: - 零碎

/// 收不回来的动作停在这儿等用户点头。
///
/// 可逆的动作（清理邮件之类）走不到这里——自动整理档下它们直接就做了，
/// 只会在过程里留一条记录。
struct ConfirmSheet: View {
    @EnvironmentObject var store: Store
    let ask: PendingAsk

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 16) {
                HStack(spacing: 10) {
                    Image(systemName: ask.irreversible
                          ? "exclamationmark.triangle.fill" : "questionmark.circle.fill")
                        .font(.title2)
                        .foregroundStyle(ask.irreversible ? Color.orange : Brand.primary)
                    Text(ask.title).font(.headline)
                }

                if !ask.detail.isEmpty {
                    ScrollView {
                        Text(ask.detail)
                            .font(.callout)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .textSelection(.enabled)
                    }
                }

                if ask.irreversible {
                    Label("这一步做了收不回来", systemImage: "arrow.uturn.backward.slash")
                        .font(.footnote)
                        .foregroundStyle(.orange)
                }

                Spacer(minLength: 0)

                HStack(spacing: 12) {
                    Button("算了") { store.answerPending(false) }
                        .buttonStyle(.glass)
                        .controlSize(.large)
                        .frame(maxWidth: .infinity)
                    Button(ask.irreversible ? "确认发出" : "去吧") {
                        store.answerPending(true)
                    }
                    .buttonStyle(.glassProminent)
                    .controlSize(.large)
                    .tint(ask.irreversible ? .orange : Brand.primary)
                    .frame(maxWidth: .infinity)
                }
            }
            .padding()
            .navigationTitle(ask.tool)
            .navigationBarTitleDisplayMode(.inline)
            .interactiveDismissDisabled()
        }
        .presentationDetents([.medium])
    }
}

/// 空状态。
///
/// 原先四个页面统一是系统那个灰托盘——日程空了给你看一个托盘，
/// 既不贴切也不像自家东西。图标改成各页自己的，底色用品牌浅蓝。
/// 小标题。健康那几块在用；别的页面走 List 的原生 Section。
///
/// 放这儿是因为 EmptyHint / LoadingHint / ErrorBar 这些共享组件都在这个
/// 文件里——收在某个功能页里的话，下一个要用的人找不到，就又写一个。
struct SectionLabel: View {
    let text: String
    var icon: String?

    var body: some View {
        HStack(spacing: 6) {
            if let icon {
                Image(systemName: icon).font(.caption2)
            }
            Text(text).font(.subheadline.weight(.semibold))
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 4)
    }
}

struct EmptyHint: View {
    let show: Bool
    let text: String
    var icon = "tray"

    var body: some View {
        if show {
            VStack(spacing: 14) {
                Image(systemName: icon)
                    .font(.system(size: 28, weight: .medium))
                    .foregroundStyle(Brand.sheen)
                    .frame(width: 72, height: 72)
                    .background(Brand.soft, in: Circle())
                Text(text)
                    .font(.callout)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 32)
        }
    }
}

/// 读失败得说出来。原来只有设置页和秘书页显示 lastError，日程/待办/财务
/// 下拉刷新失败什么都不显示——转圈停了、数字没变，看着就像「本来就没变化」。
struct ErrorBar: View {
    @EnvironmentObject var store: Store

    var body: some View {
        if let err = store.lastError, !err.isEmpty {
            HStack(alignment: .top, spacing: 6) {
                Image(systemName: "exclamationmark.triangle.fill")
                Text(err).lineLimit(3)
                Spacer(minLength: 0)
                Button { store.lastError = nil } label: { Image(systemName: "xmark") }
                    .accessibilityLabel("关掉")
            }
            .font(.caption)
            .foregroundStyle(.white)
            .padding(.horizontal, 14).padding(.vertical, 10)
            .glassEffect(.regular.tint(.red), in: .rect(cornerRadius: Brand.smallRadius))
            .padding(.horizontal, 12)
            .padding(.top, 6)
        }
    }
}

/// 第一次进来要读 iCloud、下载占位符，可能要等几秒。
/// 不给指示的话就是一屏空白，像是数据没了。
struct LoadingHint: View {
    @EnvironmentObject var store: Store
    let isEmpty: Bool
    /// 不传就看 store.loading（读 iCloud 那条路）；邮件页有自己的忙碌状态。
    var busy: Bool?
    var text = "正在读 iCloud…"

    @State private var breathe = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        if (busy ?? store.loading) && isEmpty {
            VStack(spacing: 12) {
                // 裸转圈哪个 app 都长一样。让头像轻轻喘气，
                // 这几秒就还是「他在给你办」，而不是「系统在忙」。
                Avatar(size: 56)
                    .scaleEffect(breathe ? 1.05 : 0.95)
                    .opacity(breathe ? 1 : 0.7)
                    .onAppear { breathe = !reduceMotion }
                    .animation(reduceMotion ? nil
                               : .easeInOut(duration: 1.1).repeatForever(autoreverses: true),
                               value: breathe)
                Text(text).font(.footnote).foregroundStyle(.secondary)
            }
        }
    }
}

/// 底部那颗说明用的玻璃胶囊。待办的「还有多少没显示」和邮件的动作附注
/// 长得一模一样，各写一遍只会让下次调样式时漏掉一处。
struct HintPill: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.caption)
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14).padding(.vertical, 8)
            .glassEffect(.regular, in: .capsule)
            .padding(.bottom, 8)
    }
}

/// 「记一条」。放在工具栏而不是右下角悬浮按钮——右下角会跟标签栏挤在一起，
/// 而且这几页本来就有工具栏。
struct QuickAddButton: ToolbarContent {
    @State private var open = false

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Button { open = true } label: { Image(systemName: "plus") }
                .accessibilityLabel("记一条")
                .sheet(isPresented: $open) { QuickAddView() }
        }
    }
}

struct SettingsLink: ToolbarContent {
    @State private var open = false

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Button { open = true } label: { Image(systemName: "gearshape") }
                .sheet(isPresented: $open) { SettingsView() }
        }
    }
}
