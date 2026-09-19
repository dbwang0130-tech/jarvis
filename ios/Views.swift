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
        // 通知点击要能找到 store。AppDelegate 那边挂 delegate 时 store
        // 还没建出来，所以反过来由这儿交过去。
        .onAppear {
            NotificationRouter.store = store
            // 冷启动时 onOpenURL 比这儿还早，攒下的图这会儿交出来。
            SharedImages.flush(to: store)
        }
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
    @State private var entered = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(spacing: 20) {
            Spacer()
            Avatar(size: 140)
                .shadow(color: Brand.primary.opacity(0.25), radius: 18, y: 8)
                // 启动屏上那个头像是 120pt、正中、浅蓝底。这一屏接着它演：
                // 同一个人往前凑近一点，底色从上面那片蓝里化开。
                // 不做的话，蓝底「啪」地变白屏，像是换了个 app。
                .scaleEffect(entered || reduceMotion ? 1 : 0.86)
                .opacity(entered || reduceMotion ? 1 : 0)
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
        // 顶上这片蓝就是启动屏的底色（LaunchBackground 跟 Brand.soft
        // 是同一个色值），往下化掉。两屏之间因此没有硬边。
        .background(alignment: .top) {
            LinearGradient(colors: [Brand.soft, Color(.systemBackground)],
                           startPoint: .top, endPoint: .center)
                .ignoresSafeArea()
        }
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.smooth(duration: 0.55)) { entered = true }
        }
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
        .safeAreaInset(edge: .top, spacing: 0) { OverdueBar() }
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
    /// Siri 带来的金额：有的话一进来就把记账表单摆开，金额先填上。
    var spendAmount: Double?
    @State private var showTxns = false
    @State private var adding = false

    var body: some View {
        Group {
            if let l = store.snapshot.ledger {
                List {
                    Section {
                        // 三个并排在手机上每个只有一百多点宽，数字一到七八位
                        // 就得缩到看不清。净资产是最该一眼看到的，单独占一行
                        // 给足宽度；另外两个并排够用。
                        VStack(spacing: 10) {
                            Stat(name: "净资产", value: l.net, emphasize: true)
                            HStack(spacing: 10) {
                                Stat(name: "资产", value: l.assets)
                                Stat(name: "负债", value: -l.debts)
                            }
                            // 净资产回答的是「还剩多少」，回答不了「花得凶
                            // 不凶」——而打开记账 app 最先想知道的是后者。
                            MonthCard(flow: l.month)
                        }
                        .listRowInsets(EdgeInsets(top: 8, leading: 8,
                                                  bottom: 8, trailing: 8))
                        .listRowBackground(Color.clear)
                    }
                    // 按科目分组。原来把 Income:Salary 跟银行卡并排列在「账户」
                    // 里，看着像是有这么个账户——那是记账科目，不是钱放在哪儿。
                    ForEach(accountGroups(l.accounts)) { group in
                        Section {
                            ForEach(Array(group.items.enumerated()), id: \.offset) {
                                AccountRow(account: $1, tint: group.kind.tint)
                            }
                        } header: {
                            GroupHeader(text: group.title, icon: group.kind.icon,
                                        tint: group.kind.tint)
                        }
                    }
                    // 流水点进去看。打开财务页多半是想看「还剩多少」，
                    // 而流水能有几十上百条，摊在同一页里既把净资产挤走，
                    // 又让这一页每重画一次就得把整份流水按天归拢一遍。
                    if !l.recent.isEmpty {
                        Section {
                            Button { showTxns = true } label: {
                                HStack {
                                    GroupHeader(text: "最近流水",
                                                icon: "list.bullet.rectangle", tint: .blue)
                                    Spacer()
                                    Text("\(l.recent.count) 笔")
                                        .font(.caption.monospacedDigit())
                                        .foregroundStyle(.secondary)
                                    Image(systemName: "chevron.right")
                                        .font(.caption).foregroundStyle(.tertiary)
                                }
                            }
                            .buttonStyle(.plain)
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
        .sheet(isPresented: $showTxns) { TxnListView() }
        // 这一页一直只能看。余额、账户、流水都摆出来了，可「刚花了 35 块」
        // 只能开口跟秘书说一句——一个记账 app 在手机上记不了账。
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button { adding = true } label: { Image(systemName: "plus") }
                    .accessibilityLabel("记一笔")
                    .disabled(store.snapshot.ledger == nil)
            }
        }
        .sheet(isPresented: $adding) {
            SpendView(accounts: store.snapshot.ledger?.accounts ?? [],
                      preset: spendAmount)
        }
        // Siri 说了金额就直接摆开表单——人已经开口说过「记一笔 35」，
        // 再让他自己点一下「+」等于白说。
        .task {
            if spendAmount != nil { adding = true }
        }
    }
}

/// 账户按科目归类。顺序是「钱在哪儿 → 欠多少 → 花在哪儿 → 挣多少」，
/// 最常看的排前面；权益和认不出来的放最后。
func accountGroups(_ accounts: [Account]) -> [AccountGroup] {
    AccountKind.order.compactMap { k in
        let items = accounts.filter { $0.kind == k.rawValue }
        return items.isEmpty ? nil : AccountGroup(kind: k, items: items)
    }
}

struct AccountGroup: Identifiable {
    let kind: AccountKind
    let items: [Account]
    var id: String { kind.rawValue }
    var title: String { kind.title }
}

/// 科目的样子：叫什么、什么颜色、什么图标。
///
/// 跟健康那边 HealthGroup 一个路子——颜色和图标是界面的事，收在一处，
/// 别散在各个 switch 里。顺序就是「钱在哪儿 → 欠多少 → 花在哪儿 →
/// 挣多少」，最常看的排前面；权益和认不出来的放最后。
enum AccountKind: String, CaseIterable {
    case asset, debt, expense, income, equity, other

    static let order: [AccountKind] = [.asset, .debt, .expense, .income, .equity, .other]

    var title: String {
        switch self {
        case .asset:   return "账户"
        case .debt:    return "负债"
        case .expense: return "支出"
        case .income:  return "收入"
        case .equity:  return "权益"
        case .other:   return "其他"
        }
    }

    var tint: Color {
        switch self {
        case .asset:   return .green
        case .debt:    return .red
        case .expense: return .orange
        case .income:  return .mint
        case .equity:  return .indigo
        case .other:   return .secondary
        }
    }

    var icon: String {
        switch self {
        case .asset:   return "banknote"
        case .debt:    return "creditcard"
        case .expense: return "cart"
        case .income:  return "arrow.down.circle"
        case .equity:  return "scalemass"
        case .other:   return "square.grid.2x2"
        }
    }
}

/// 最近流水单独一页。
///
/// 从财务页里搬出来的。财务页要回答的是「还剩多少」——净资产、各个账户
/// 的余额；流水是「钱去哪儿了」，是另一个问题，也是另一屏东西。搬走之后
/// 财务页的 body 只碰一个 count，按天归拢、日期换算、几十行的行视图
/// 都只在点开之后才发生。
///
/// **盖一张卡片，不 push。** 这个 app 里翻详情只有这一种手势：邮件点开
/// 一封、健康点开一项指标、日程点开一条，都是盖上来、往下一划回去。
/// 只有这儿 push 的话，返回得去左上角找按钮，划一下反而回到了对话。
///
/// 数据仍从 store 读，不在打开那一刻定格——切回前台或者下拉都会重读，
/// 「跟电脑上是同一份」得在这一页也成立。
struct TxnListView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    @State private var asking: TxnBrief?
    @State private var keyword = ""

    /// 搜的是收款方和分类——「上个月星巴克花了多少」这种问题，翻是翻不
    /// 出来的，六十笔流水摊开有好几屏。
    private var txns: [TxnBrief] {
        let all = store.snapshot.ledger?.recent ?? []
        let k = keyword.trimmingCharacters(in: .whitespaces)
        guard !k.isEmpty else { return all }
        return all.filter {
            $0.payee.localizedCaseInsensitiveContains(k)
                || $0.category.localizedCaseInsensitiveContains(k)
        }
    }

    private var askingBinding: Binding<Bool> {
        Binding(get: { asking != nil }, set: { if !$0 { asking = nil } })
    }

    private func reverse(_ txn: TxnBrief) {
        asking = nil
        Task {
            let ok = await store.reverse(txn)
            ok ? Haptics.warning() : Haptics.failure()
        }
    }

    var body: some View {
        NavigationStack {
            list
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button { dismiss() } label: { Image(systemName: "xmark") }
                            .accessibilityLabel("关掉")
                    }
                }
        }
    }

    private var list: some View {
        List {
            // 按天分组。一条一条平铺的话，日期在每行重复一遍，
            // 而「今天花了多少」这个最常问的问题反而得自己加。
            ForEach(dayGroups(txns), id: \.date) { day in
                Section {
                    ForEach(Array(day.items.enumerated()), id: \.offset) { _, txn in
                        TxnRow(txn: txn)
                            // 记错了不能删，只能冲——账本是流水账，
                            // 回头改历史的账没法对。
                            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                if !txn.isReversal {
                                    Button { asking = txn } label: {
                                        Label("冲账", systemImage: "arrow.uturn.backward")
                                    }
                                    .tint(.orange)
                                }
                            }
                    }
                } header: {
                    HStack {
                        Text(dayTitle(day.date))
                            .font(.subheadline.weight(.semibold))
                            .foregroundStyle(.primary)
                            .textCase(nil)
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
        .navigationTitle("最近流水")
        .navigationBarTitleDisplayMode(.inline)
        .searchable(text: $keyword, prompt: "找收款方或分类")
        .refreshable { await store.reload() }
        .confirmationDialog(asking.map { "冲掉「\($0.payee)」？" } ?? "",
                            isPresented: askingBinding, titleVisibility: .visible) {
            if let txn = asking {
                Button("记一笔冲账", role: .destructive) { reverse(txn) }
            }
            Button("算了", role: .cancel) {}
        } message: {
            Text("原来那笔会留在账上，另记一笔方向相反的把它抵掉——"
                 + "账本是流水账，改不得历史。")
        }
        .overlay {
            // 搜不到和一笔都没有是两回事。都说「还没有流水」的话，
            // 人会以为账本空了。
            EmptyHint(show: txns.isEmpty && !store.loading,
                      text: keyword.isEmpty ? "还没有流水" : "没有对得上的",
                      icon: keyword.isEmpty ? "list.bullet.rectangle" : "magnifyingglass")
        }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
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
        // 一笔账是一条信息。不合并的话 VoiceOver 念成「收款方」「分类」
        // 「金额」三下，还得自己在脑子里拼回去。
        .accessibilityElement(children: .combine)
        .accessibilityLabel(spoken)
    }

    /// 念出来的说法。符号和箭头图标对眼睛管用，对耳朵得换成词。
    private var spoken: String {
        let what = txn.category.isEmpty ? txn.payee : "\(txn.payee)，\(txn.category)"
        if txn.isTransfer { return "转账，\(what)，\(money(abs(txn.amount)))" }
        return "\(txn.isIncome ? "收入" : "支出")，\(what)，\(money(abs(txn.amount)))"
    }

    private var signed: String {
        txn.isTransfer ? money(abs(txn.amount))
                       : (txn.amount > 0 ? "+" : "") + money(txn.amount)
    }

    private var icon: String {
        // 冲账那笔单独一个图标：它跟普通收支不是一回事，是用来抵掉
        // 另一笔的。不标出来的话，账上会凭空多出一笔「收入」。
        if txn.isReversal { return "arrow.uturn.backward.circle.fill" }
        if txn.isTransfer { return "arrow.left.arrow.right" }
        return txn.isIncome ? "arrow.down.circle.fill" : "arrow.up.circle.fill"
    }

    private var tint: Color {
        if txn.isReversal { return .orange }
        if txn.isTransfer { return .secondary }
        return txn.isIncome ? .green : Brand.onSoft
    }
}

struct AccountRow: View {
    let account: Account
    var tint: Color = .secondary

    var body: some View {
        HStack(spacing: 10) {
            // 一个小色点。整行都染色太吵，但完全没有的话，滑到中间就分不清
            // 现在看的是哪一组了——分区标题早滚出屏幕了。
            Circle().fill(tint).frame(width: 7, height: 7)
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
        // 色点是分组用的，念不出来；账户全名要连着父级一起念，
        // 光一个「储蓄」不知道是哪家的。
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(account.name)，\(money(account.balance))")
    }
}

struct Stat: View {
    let name: String
    let value: Double
    var emphasize = false

    var body: some View {
        VStack(spacing: 4) {
            Text(name).font(.caption).foregroundStyle(.secondary)
            // 数字排大号、单位排小号。同一个字号的话，一眼扫过去分不出
            // 「231891」和「元」哪个是重点——健康页那几张卡也是这么排的。
            HStack(alignment: .firstTextBaseline, spacing: 2) {
                Text(moneyRound(value))
                    .font(.system(emphasize ? .largeTitle : .title3,
                                  design: .rounded).weight(.semibold))
                    .monospacedDigit()
                    .foregroundStyle(emphasize ? AnyShapeStyle(Brand.sheen)
                                               : AnyShapeStyle(Color.primary))
                Text("元").font(.caption).foregroundStyle(.secondary)
            }
            .lineLimit(1)
            .minimumScaleFactor(0.4)
        }
        .padding(.vertical, 14)
        .frame(maxWidth: .infinity)
        // 净资产是这页最该先看到的，给它一点色，另外两个保持素净。
        .background(emphasize ? AnyShapeStyle(Brand.soft)
                              : AnyShapeStyle(Color(.secondarySystemGroupedBackground)),
                    in: .rect(cornerRadius: Brand.radius))
    }
}

/// 逾期堆起来的时候，给一条「都挪到今天」。
///
/// **这是最该自动化的重复劳动**：逾期的事会一直堆着，而清理它们的动作
/// 完全一样——左滑、推迟、下一条。十条就是三十个动作，而人想做的只是
/// 「这些今天再说」。
///
/// 不自动挪：什么该挪、什么该干脆放弃，只有人知道。这里只把那一下从
/// 三十个动作压成两个（点一下、点头）。
struct OverdueBar: View {
    @EnvironmentObject var store: Store
    @State private var asking = false
    @State private var working = false

    private var overdue: [Item] {
        store.snapshot.todos.filter { $0.overdue && !$0.isDone }
    }

    var body: some View {
        if !overdue.isEmpty {
            HStack(spacing: 10) {
                Image(systemName: "exclamationmark.arrow.circlepath")
                    .foregroundStyle(.orange)
                    .accessibilityHidden(true)
                Text("\(overdue.count) 件逾期")
                    .font(.footnote.weight(.medium))
                Spacer(minLength: 0)
                Button {
                    asking = true
                } label: {
                    if working {
                        ProgressView().controlSize(.small)
                    } else {
                        Text("都挪到今天").font(.footnote.weight(.semibold))
                    }
                }
                .buttonStyle(.plain)
                .foregroundStyle(Brand.primary)
                .disabled(working || store.thinking)
            }
            .padding(.horizontal, 14).padding(.vertical, 9)
            .background(Color.orange.opacity(0.12), in: .capsule)
            .padding(.horizontal, 12).padding(.bottom, 6)
            .confirmationDialog("把 \(overdue.count) 件逾期的挪到今天？",
                                isPresented: $asking, titleVisibility: .visible) {
                Button("都挪过来") { move() }
                Button("算了", role: .cancel) {}
            } message: {
                Text("只改它们的计划时间，内容不动。"
                     + "不想做的那几件，挪之前先标成「不做了」。")
            }
        }
    }

    private func move() {
        let refs = overdue.map(\.ref)
        working = true
        Task {
            // 用 today 而不是 postpone：postpone 的 days 零值按 1 天算
            //（「推迟」默认推一天），表达不出「挪到今天」。
            let ok = await store.act("today", on: "", refs: refs)
            working = false
            ok ? Haptics.done() : Haptics.failure()
        }
    }
}

/// 这个月花了多少。
///
/// 摆在净资产下面，因为它回答的是另一个问题：净资产是「还剩多少」，
/// 这张卡是「花得凶不凶」。记了一个月的账，最先想知道的就是这个。
///
/// **跟上月同期比，不跟上月全月比**：每个月初都会显得「比上月少花很多」，
/// 到月底又追平——那不是花钱习惯变了，只是天数不一样。
struct MonthCard: View {
    let flow: MonthFlow

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            VStack(alignment: .leading, spacing: 3) {
                Text("这个月花了")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                HStack(alignment: .firstTextBaseline, spacing: 2) {
                    Text(moneyRound(flow.expense))
                        .font(.system(.title2, design: .rounded).weight(.semibold))
                        .monospacedDigit()
                    Text("元").font(.caption).foregroundStyle(.secondary)
                }
                .lineLimit(1)
                .minimumScaleFactor(0.5)
            }

            Spacer(minLength: 0)

            VStack(alignment: .trailing, spacing: 3) {
                if flow.income > 0 {
                    Text("进账 " + moneyRound(flow.income))
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.green)
                }
                compare
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity)
        .background(Color(.secondarySystemGroupedBackground),
                    in: .rect(cornerRadius: Brand.radius))
        .accessibilityElement(children: .combine)
        .accessibilityLabel(spoken)
    }

    /// 跟上月同期比。差得不到一块钱就不说了——那是噪声，不是趋势。
    @ViewBuilder
    private var compare: some View {
        if let d = flow.delta, abs(d) >= 1 {
            Label {
                Text("比上月同期" + (d > 0 ? "多 " : "少 ") + moneyRound(abs(d)))
            } icon: {
                Image(systemName: d > 0 ? "arrow.up.right" : "arrow.down.right")
            }
            .font(.caption2.monospacedDigit())
            // 多花是橙的不是红的：花钱不是错，红色留给真出问题的地方。
            .foregroundStyle(d > 0 ? Color.orange : Color.green)
        } else if flow.days <= 3 {
            // 月初还没几天，比出来的数没意义，直说就是了。
            Text("这个月才过了 \(flow.days) 天")
                .font(.caption2)
                .foregroundStyle(.tertiary)
        }
    }

    private var spoken: String {
        var s = "这个月花了 \(moneyRound(flow.expense)) 元"
        if flow.income > 0 { s += "，进账 \(moneyRound(flow.income)) 元" }
        if let d = flow.delta, abs(d) >= 1 {
            s += "，比上月同期" + (d > 0 ? "多" : "少") + " \(moneyRound(abs(d))) 元"
        }
        return s
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
    @State private var askClear = false
    /// Siri 说「记一笔账」时带来的金额，交给财务页把表单摆好。
    @State private var spendAmount: Double?
    /// 这一轮要一起发出去的图。
    @State private var shots: [Data] = []
    @State private var entered = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var phase
    @FocusState private var focused: Bool
    /// 发送键跟着字号长。写死 38 的话，输入框在无障碍档位下长高了，
    /// 旁边这颗还是原样，看着像掉在坑里。
    @ScaledMetric(relativeTo: .headline) private var send: CGFloat = 44

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
            .sheet(item: $tool) { ToolSheet(tool: $0, spendAmount: spendAmount) }
            // 点了「该收信了」那条通知：直接开邮件页，那边看到标记会自己
            // 收一次。通知里说的是「点一下，我这就去收」，那就得真去收，
            // 不能把人扔在对话页自己找。
            .onChange(of: store.wantsMailCheck) { _, want in
                if want { tool = .mail }
            }
            // Siri / 快捷指令带进来的那一件事。起来时取一次；切回前台也
            // 取一次——app 已经开着的时候喊 Siri，走的是后面那条。
            .task { await takeIntent() }
            // 别的 app 分享进来的图：落进输入框，等人补一句话再发。
            //
            // 不直接发出去——分享一张图过来多半是要问点什么（「这张小票
            // 记一下」），替他发了反而得重来一轮。
            .onChange(of: store.sharedShots) { _, incoming in
                guard !incoming.isEmpty else { return }
                for data in incoming where shots.count < PhotoButton.maxShots {
                    shots.append(data)
                }
                store.sharedShots = []
                focused = true
                Haptics.done()
            }
            .onChange(of: phase) { _, now in
                if now == .active { Task { await takeIntent() } }
            }
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button { askClear = true } label: {
                        Image(systemName: "trash")
                    }
                    .disabled(store.messages.isEmpty)
                    .accessibilityLabel("清空对话")
                }
                SettingsLink()
            }
            // 一下点掉整段对话，之前是碰到就没了。别的收不回来的事
            // （发信、删邮件）都要点头，自己这一处不该例外。
            .confirmationDialog("清空这段对话？", isPresented: $askClear,
                                titleVisibility: .visible) {
                Button("清空", role: .destructive) {
                    store.clearChat()
                    Haptics.warning()
                }
                Button("算了", role: .cancel) {}
            } message: {
                Text("说过的话会从这台设备上删掉，日程和账本不受影响。")
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
                                // 例句是**可以点的样例**，不是要逐字读的正文。
                                // 一路放大的话四句话就占满两屏，人得先滚过
                                // 它们才够得着输入框——而这一屏的正事是打字。
                                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                        }
                        .padding(.top, 20)
                        // 头像刚从启动屏那儿过来，这一屏接着往上浮一点。
                        // 硬糊上去的话，启动屏那一下就白铺垫了。
                        .opacity(entered || reduceMotion ? 1 : 0)
                        .offset(y: entered || reduceMotion ? 0 : 10)
                        .onAppear {
                            guard !reduceMotion else { return }
                            withAnimation(.smooth(duration: 0.45)) { entered = true }
                        }
                    }
                    ForEach(Array(shownMessages.enumerated()), id: \.offset) { i, m in
                        Bubble(message: m, isLast: i == shownMessages.count - 1)
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
                        .padding(.horizontal, 14).padding(.vertical, 8)
                }
                .buttonStyle(.plain)
                .foregroundStyle(Brand.primary)
                // 底是**实的**，不是玻璃。这是一条横幅，压在对话之上；
                // 玻璃会把底下滚过去的气泡透上来，两层蓝叠在一起谁都读不清
                // ——字号调大之后尤其明显。别处用玻璃是因为底下没有字。
                .background(Brand.soft, in: .capsule)
                .overlay(Capsule().strokeBorder(Brand.primary.opacity(0.15)))
                .padding(.horizontal, 12).padding(.bottom, 6)
            }
            // 打字的时候把这排收起来。小屏上键盘一弹，光输入框就占掉半屏，
            // 再压着一排胶囊，能看见的对话只剩两行。
            if !focused {
                ToolStrip { tool = $0 }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    // 这排是**控件**，不是正文。跟着无障碍字号一路放大的话，
                    // 一个「日程」就占掉半屏宽，还把输入框挤下去——
                    // 而这一屏真正要保的是能打字。HIG 允许控件类元素封顶。
                    .dynamicTypeSize(...DynamicTypeSize.accessibility1)
            }
            // 输入区浮在内容之上，不再用一条实心底栏压着——下面的话能透出来，
            // 屏幕显得高一些。GlassEffectContainer 让挨着的两块玻璃算作一体，
            // 各自算各自的会在缝隙处叠出一道亮边。
            // 挑好的图摆在输入框上面：发出去之前得看得见自己挑了什么，
            // 也得能反悔。
            if !shots.isEmpty {
                ShotStrip(shots: $shots)
                    // **得有自己的底。** 这一条跟输入框一样浮在对话之上，
                    // 没有底的话下面滚过去的气泡会从图的缝隙里透上来，
                    // 看着像是图片糊了——跟「还没填模型密钥」那条当初
                    // 踩的是同一个坑。
                    .background(.bar, in: .rect(cornerRadius: Brand.radius))
                    .padding(.horizontal, 12)
                    .padding(.bottom, 6)
            }

            GlassEffectContainer(spacing: 8) {
                HStack(alignment: .bottom, spacing: 8) {
                    // 想说的话可能不止一行，让它自己长，最多六行。
                    // 相机在左、麦克风在右，**分开站**。
                    //
                    // 三颗都堆在右边（相机、麦克风，外面还有发送）的话，
                    // 指头落下去那一片全是按钮，想发图点成了发送、想说话
                    // 点成了发图。左右分开，误点就没了。
                    //
                    // 用 HStack 而不是 overlay：overlay 叠在 padding 之后的
                    // 整块区域上，靠 alignment 加 offset 去凑位置，多行输入
                    // 长高之后就会飘。摆进 HStack，位置是算出来的。
                    //
                    // 垂直方向按**底部**对齐，但两颗按钮各往上提 5pt：
                    // TextField 自带 11pt 的上下留白，纯按底边对齐的话，
                    // 按钮会沉到那片留白里，看着比文字低一截。
                    HStack(alignment: .bottom, spacing: 2) {
                        PhotoButton(shots: $shots)
                            .padding(.bottom, 5)
                        TextField(shots.isEmpty ? "说点什么" : "问问这张图",
                                  text: $draft, axis: .vertical)
                            .lineLimit(1...6)
                            .padding(.vertical, 11)
                            .focused($focused)
                        MicButton(draft: $draft)
                            .padding(.bottom, 5)
                    }
                    .padding(.horizontal, 6)
                    .glassEffect(.regular, in: .capsule)

                    Button(action: submit) {
                        Image(systemName: store.thinking ? "ellipsis" : "arrow.up")
                            .font(.system(.headline, weight: .bold))
                            .foregroundStyle(canSend ? Color.white : Color.secondary)
                            .frame(width: send, height: send)
                            .contentTransition(.symbolEffect(.replace))
                    }
                    .glassEffect(canSend ? .regular.tint(Brand.primary).interactive()
                                         : .regular,
                                 in: .circle)
                    .disabled(!canSend)
                    .animation(.easeInOut(duration: 0.18), value: canSend)
                    .accessibilityLabel(store.thinking ? "正在办" : "发送")
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
        // 光发图不说话也行——「这是什么」有时候不必打字。
        let hasText = !draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return (hasText || !shots.isEmpty) && !store.thinking
    }

    private func submit() {
        let q = draft
        let imgs = shots
        draft = ""
        shots = []
        Haptics.tap()
        // 不在这儿收键盘：刚发完还想接着补一句是常事。
        // 等它真动起手来（操作列表冒出来）再收，那会儿是要看它干什么。
        Task { await store.send(q, images: imgs) }
    }

    private func startWith(_ text: String) {
        draft = text
        focused = true
    }

    /// 取走 Siri / 快捷指令留下的那件事。
    ///
    /// 说的话直接发出去，不填进输入框等人再按一下——人已经开口说过了，
    /// 再让他确认一遍等于白说。记账则是把表单摆好：科目得挑，而挑科目
    /// 要看到账本里有哪些，Siri 那头给不了。
    private func takeIntent() async {
        guard let task = IntentInbox.take() else { return }
        switch task {
        case .say(let text):
            let q = text.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !q.isEmpty else { return }
            await store.send(q)
        case .spend(let amount):
            spendAmount = amount
            tool = .ledger
        }
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
                Button { Haptics.tap(); pick(e) } label: {
                    Text(e)
                        .font(.callout)
                        .foregroundStyle(Brand.onSoft)
                        .padding(.horizontal, 14).padding(.vertical, 9)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(Brand.soft).interactive(), in: .capsule)
                // 点了只是填进输入框，不直接发。看得见的人从上下文能猜到，
                // 读屏的人猜不到——一句话说清就不用试。
                .accessibilityHint("填进输入框，可以改完再发")
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct Bubble: View {
    @EnvironmentObject var store: Store
    let message: Message
    /// 是不是最后一条。只有最后那条秘书的回答能重答——中间某条重答了，
    /// 后面的对话就接不上了。
    var isLast = false

    var isUser: Bool { message.role == "user" }

    /// 四个角不一样圆：靠说话人那一侧的下角收紧。
    ///
    /// 四角同圆的话，一串气泡看着像并排的卡片，得靠颜色和左右位置才分得出
    /// 谁在说；收一个角，形状本身就指向说话的人——头像在左，左下角就尖，
    /// 话是从那儿出来的。iMessage、微信都是这个道理。
    private var shape: UnevenRoundedRectangle {
        let round: CGFloat = 16
        let tight: CGFloat = 5
        return UnevenRoundedRectangle(
            topLeadingRadius: round,
            bottomLeadingRadius: isUser ? round : tight,
            bottomTrailingRadius: isUser ? tight : round,
            topTrailingRadius: round)
    }

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
                .background(isUser ? Brand.primary : Brand.soft, in: shape)
                .foregroundStyle(isUser ? Color.white : Brand.onSoft)
                .textSelection(.enabled)
                // 左右对齐和颜色是给眼睛分的，VoiceOver 只听得到一串
                // 文字，一问一答连在一起读完全分不出谁在说话。
                .accessibilityLabel(isUser ? "你说：\(message.content)"
                                           : "Jarvis 说：\(message.content)")
                // 长按：抄走，或者换个说法再问一遍。重发对用户自己那句
                // 尤其有用——秘书会错意时，改一个词比重打一整句快得多。
                .contextMenu {
                    Button {
                        UIPasteboard.general.string = message.content
                        Haptics.tap()
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
                    } else if isLast {
                        // 答偏了、答得太啰嗦、或者中途被打断只答了一半——
                        // 以前唯一的办法是把原话重新打一遍。只给最后那条：
                        // 中间某条重答了，后面的对话就接不上了。
                        Button {
                            Task { await store.retryLast() }
                        } label: {
                            Label("重答一遍", systemImage: "arrow.clockwise")
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
            BusyAvatar(size: 26)

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
            Button { store.stopThinking(); Haptics.tap() } label: {
                Image(systemName: "stop.fill")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .frame(width: 30, height: 30)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: .circle)
            // 玻璃圆还是 30，但**能按到的地方**撑到 44——这是 HIG 的下限，
            // 而这颗按钮偏偏是着急的时候要点的那颗。
            .frame(width: 44, height: 44)
            .contentShape(Circle())
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
                .accessibilityHidden(true)
            Text(text)
                .font(.caption2)
                .foregroundStyle(step.kind == "error" ? Color.red : .secondary)
                .lineLimit(2)
        }
        .accessibilityElement(children: .combine)
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
                        // 收不回来的事给一下更重的——手指离开屏幕的瞬间
                        // 就该知道「这下真发出去了」。
                        ask.irreversible ? Haptics.warning() : Haptics.tap()
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
/// 分区标题：彩色图标 + 黑色粗体。
///
/// 比 List 默认那种灰色小字重得多，是**故意的**：一屏十来张卡片摞下来，
/// 灰色小字分不出段落。系统「健康」和「设置」里的分组标题也是这个分量。
struct GroupHeader: View {
    let text: String
    var icon: String?
    var tint: Color = .secondary

    var body: some View {
        HStack(spacing: 7) {
            if let icon {
                Image(systemName: icon)
                    // 语义字号而不是定死 15pt：标题用 .title3，图标不跟着
                    // 缩放的话，字放大到无障碍档位就成了大字配小图标。
                    .font(.system(.subheadline, weight: .semibold))
                    .foregroundStyle(tint)
            }
            Text(text).font(.title3.weight(.bold))
                .foregroundStyle(.primary)
        }
        // List 的 section header 默认全大写、全灰；这两下把它收回来。
        .textCase(nil)
        .padding(.vertical, 2)
        // 让 VoiceOver 的「按标题浏览」能停在这儿——一屏十来张卡片，
        // 不能只有从头听到尾一条路。
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

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
    /// 圆底跟着字号长。写死 72 的话，字调大之后图标会顶破这个圈。
    @ScaledMetric(relativeTo: .title) private var badge: CGFloat = 72

    var body: some View {
        if show {
            VStack(spacing: 14) {
                Image(systemName: icon)
                    .font(.system(.title, weight: .medium))
                    .foregroundStyle(Brand.sheen)
                    .frame(width: badge, height: badge)
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
                    .accessibilityHidden(true)
                Text(err).lineLimit(3)
                Spacer(minLength: 0)
                // 能再试一次的就给一颗。以前这条子只能关掉——读 iCloud
                // 超时、收信断在半路，看完那句话人能做的只有把它关了，
                // 然后自己找个地方下拉刷新，而出错的地方未必有下拉。
                if let again = store.retry {
                    Button {
                        store.lastError = nil
                        Task { await again() }
                    } label: {
                        Text("重试")
                            .font(.caption.weight(.semibold))
                            .padding(.horizontal, 10)
                            .frame(height: 30)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
                Button { store.lastError = nil } label: {
                    Image(systemName: "xmark")
                        .frame(width: 30, height: 30)
                        .contentShape(Rectangle())
                }
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

    var body: some View {
        if (busy ?? store.loading) && isEmpty {
            VStack(spacing: 12) {
                BusyAvatar()
                Text(text).font(.footnote).foregroundStyle(.secondary)
            }
            // 「正在读」要念出来，不然屏幕上什么都没有，读屏的人不知道
            // 是在等还是空了。
            .accessibilityElement(children: .combine)
            .accessibilityLabel(text)
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

/// 设置的入口。**只挂在对话那一页上。**
///
/// 日程、邮件、财务、健康原先各挂了一个——那会儿它们是并排的标签页，
/// 不各带一个就没别的路进设置。现在它们是从对话里掀起来的卡片，右上角
/// 已经有「关掉」了，再摆个齿轮就是两个按钮挤在一起，而且点进去还得
/// 关两层才回得到刚才那一页。
struct SettingsLink: ToolbarContent {
    @State private var open = false

    var body: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            Button { open = true } label: { Image(systemName: "gearshape") }
                .accessibilityLabel("设置")
                .sheet(isPresented: $open) { SettingsView() }
        }
    }
}
