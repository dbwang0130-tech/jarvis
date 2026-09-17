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
                MainTabs()
            } else {
                WelcomeView()
            }
        }
        .environmentObject(store)
        .tint(Brand.primary)
        // 图标那个人是圆润的软塑感，字跟着圆一点才像一家人。
        .fontDesign(.rounded)
        .task {
            if store.isConfigured { await store.reload() }
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
            Text("Jarvis").font(.largeTitle.bold())
            Text("日程和账本都放在 iCloud 里，\n跟电脑上那份是同一份。")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)

            Button("选 iCloud 里的 Jarvis 目录") { picking = true }
                .buttonStyle(.borderedProminent)

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

struct MainTabs: View {
    @EnvironmentObject var store: Store

    var body: some View {
        // 秘书排第一，因为它是主交互——其余三页是"看"，这页是"使唤"。
        // TabView 默认选第一个，不用另外记选中状态。
        TabView {
            NavigationStack { ChatView() }
                .tabItem { Label("Jarvis", systemImage: "bubble.left.and.text.bubble.right") }
            NavigationStack { AgendaView() }
                .tabItem { Label("日程", systemImage: "calendar") }
            NavigationStack { TodosView() }
                .tabItem { Label("待办", systemImage: "checklist") }
            NavigationStack { LedgerTab() }
                .tabItem { Label("财务", systemImage: "yensign.circle") }
        }
    }
}

// MARK: - 日程

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
                    ForEach(day.items) { item in
                        Button { selected = item } label: { ItemRow(item: item) }
                            .buttonStyle(.plain)
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
        .navigationTitle("日程")
        .refreshable { await store.reload() }
        .overlay { EmptyHint(show: store.snapshot.days.isEmpty && !store.loading, text: "这几天没有安排") }
        .overlay { LoadingHint(isEmpty: store.snapshot.days.isEmpty) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .toolbar { SettingsLink() }
        // 整个列表共用一个详情页。挂在每一行上的话，有多少行就建多少套
        // 呈现状态，滚起来明显发涩。
        .sheet(item: $selected) { ItemDetail(item: $0) }
    }
}

// MARK: - 待办

struct TodosView: View {
    @EnvironmentObject var store: Store
    @State private var keyword = ""
    @State private var shown: [Item] = []
    @State private var selected: Item?

    var body: some View {
        List(shown) { item in
            Button { selected = item } label: { ItemRow(item: item) }
                .buttonStyle(.plain)
        }
        .searchable(text: $keyword, prompt: "找点什么")
        .navigationTitle("待办")
        .refreshable { await store.reload() }
        .overlay { EmptyHint(show: shown.isEmpty && !store.loading, text: "没有待办") }
        .overlay { LoadingHint(isEmpty: shown.isEmpty) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .safeAreaInset(edge: .bottom) { truncatedHint }
        .toolbar { SettingsLink() }
        .sheet(item: $selected) { ItemDetail(item: $0) }
        // 过滤放进 task：写成计算属性的话，每次 body 重算都要把整份待办
        // 再过一遍。id 变了旧任务会被取消，顺带就是输入防抖。
        .task(id: FilterKey(keyword: keyword, count: store.snapshot.todos.count)) {
            await refilter()
        }
    }

    private struct FilterKey: Equatable {
        var keyword: String
        var count: Int
    }

    private func refilter() async {
        let k = keyword.trimmingCharacters(in: .whitespaces)
        if !k.isEmpty {
            // 打字没停就别急着过滤；被取消了就什么都不做。
            try? await Task.sleep(for: .milliseconds(180))
            if Task.isCancelled { return }
        }
        let all = store.snapshot.todos
        guard !k.isEmpty else { shown = all; return }
        shown = all.filter {
            $0.title.localizedCaseInsensitiveContains(k)
                || $0.tags.contains { $0.localizedCaseInsensitiveContains(k) }
        }
    }

    /// 待办被截断时说清楚，否则找不到某一条会以为是丢了。
    @ViewBuilder
    private var truncatedHint: some View {
        let s = store.snapshot
        if s.todosTotal > s.todos.count {
            Text("共 \(s.todosTotal) 条，只显示前 \(s.todos.count) 条")
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 14).padding(.vertical, 8)
                .glassEffect(.regular, in: .capsule)
                .padding(.bottom, 8)
        }
    }
}

// MARK: - 财务

struct LedgerTab: View {
    @EnvironmentObject var store: Store

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
                    Section("账户") {
                        ForEach(l.accounts) { a in
                            HStack {
                                Text(a.name)
                                    .font(.callout).lineLimit(1)
                                    .truncationMode(.head)  // 账户名前缀重复，砍前面
                                Spacer(minLength: 8)
                                Text(money(a.balance))
                                    .font(.callout.monospacedDigit())
                                    .lineLimit(1).minimumScaleFactor(0.7)
                                    .foregroundStyle(a.balance < 0 ? Color.red : Color.primary)
                            }
                        }
                    }
                    Section("最近流水") {
                        ForEach(l.recent) { t in
                            HStack {
                                Text(t.date.suffix(5))
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                                Text(t.payee).lineLimit(1)
                                Spacer(minLength: 8)
                                Text(money(t.amount))
                                    .font(.callout.monospacedDigit())
                                    .lineLimit(1).minimumScaleFactor(0.7)
                            }
                        }
                    }
                }
            } else {
                EmptyHint(show: !store.loading, text: "没有账本")
            }
        }
        .navigationTitle("财务")
        .refreshable { await store.reload() }
        .overlay { LoadingHint(isEmpty: store.snapshot.ledger == nil) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .toolbar { SettingsLink() }
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
private func trimZeros(_ v: Double) -> String {
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
                            HStack(spacing: 10) {
                                Avatar(size: 44)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text("Jarvis").font(.headline)
                                    Text("日程、账本、邮件，说一句就办")
                                        .font(.caption).foregroundStyle(.secondary)
                                }
                            }
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
            if !isUser { Spacer(minLength: 40) }
        }
    }
}

struct ThinkingRow: View {
    let steps: [Step]

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Avatar(size: 26)
            ProgressView().controlSize(.small)
            VStack(alignment: .leading, spacing: 2) {
                if let last = steps.last {
                    Text(label(for: last)).font(.caption).foregroundStyle(.secondary)
                } else {
                    Text("想想…").font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer()
        }
    }

    func label(for step: Step) -> String {
        step.kind == "result" ? step.text : toolLabel(step.name)
    }
}

/// 工具名翻成人话。跟桌面端那张表保持一致。
func toolLabel(_ name: String) -> String {
    let labels = [
        "list_agenda": "查看日程", "list_todos": "查看待办", "find_free_slots": "寻找空档",
        "get_item": "查看条目", "schedule_item": "安排时间", "set_deadline": "设置截止",
        "set_todo_state": "修改状态", "set_priority": "设置优先级", "create_item": "新建条目",
        "set_repeat": "设置循环", "delete_item": "删除条目", "add_note": "记笔记",
        "move_item": "移动条目", "demote_item": "降级", "promote_item": "升级",
        "clock_in": "开始计时", "clock_out": "结束计时",
        "list_accounts": "查余额", "list_transactions": "看流水", "add_transaction": "记账",
        "fetch_mail": "收邮件", "list_mail": "看邮件", "read_mail": "读邮件",
        "search_mail": "找邮件", "clean_mail": "清理邮件", "triage_mail": "分拣邮件",
        "draft_reply": "起草回信", "draft_mail": "起草邮件", "send_draft": "发出邮件",
        "list_drafts": "看草稿", "mark_read": "标为已读",
        "remember": "记下来", "forget": "忘掉",
        "plan": "制定计划", "step_done": "标记完成",
    ]
    return labels[name] ?? name
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
            .navigationTitle(toolLabel(ask.tool))
            .navigationBarTitleDisplayMode(.inline)
            .interactiveDismissDisabled()
        }
        .presentationDetents([.medium])
    }
}

struct EmptyHint: View {
    let show: Bool
    let text: String

    var body: some View {
        if show {
            ContentUnavailableView(text, systemImage: "tray")
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

    var body: some View {
        if store.loading && isEmpty {
            VStack(spacing: 10) {
                ProgressView()
                Text("正在读 iCloud…").font(.footnote).foregroundStyle(.secondary)
            }
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
