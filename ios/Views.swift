import SwiftUI
import UniformTypeIdentifiers

// 界面分四块：日程、待办、财务、对话。
// 手机屏幕小，每块只放最要紧的东西，详情靠点进去看。

struct RootView: View {
    @StateObject private var store = Store()

    var body: some View {
        Group {
            if store.isConfigured {
                MainTabs()
            } else {
                WelcomeView()
            }
        }
        .environmentObject(store)
        .task {
            if store.isConfigured { await store.reload() }
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

    var body: some View {
        List {
            ForEach(store.snapshot.days) { day in
                Section {
                    if day.items.isEmpty {
                        Text("空着").foregroundStyle(.tertiary).font(.footnote)
                    }
                    ForEach(day.items) { ItemRow(item: $0) }
                } header: {
                    HStack {
                        Text("\(day.date.suffix(5)) \(day.weekday)")
                        if day.today {
                            Text("今天")
                                .font(.caption2.bold())
                                .padding(.horizontal, 6).padding(.vertical, 2)
                                .background(.tint, in: Capsule())
                                .foregroundStyle(.white)
                        }
                    }
                }
            }
        }
        .navigationTitle("日程")
        .refreshable { await store.reload() }
        .overlay { EmptyHint(show: store.snapshot.days.isEmpty, text: "这几天没有安排") }
        .toolbar { SettingsLink() }
    }
}

// MARK: - 待办

struct TodosView: View {
    @EnvironmentObject var store: Store
    @State private var keyword = ""

    var shown: [Item] {
        let k = keyword.trimmingCharacters(in: .whitespaces)
        guard !k.isEmpty else { return store.snapshot.todos }
        return store.snapshot.todos.filter {
            $0.title.localizedCaseInsensitiveContains(k) ||
            $0.tags.contains { $0.localizedCaseInsensitiveContains(k) }
        }
    }

    var body: some View {
        List(shown) { ItemRow(item: $0) }
            .searchable(text: $keyword, prompt: "找点什么")
            .navigationTitle("待办")
            .refreshable { await store.reload() }
            .overlay { EmptyHint(show: shown.isEmpty, text: "没有待办") }
            .toolbar { SettingsLink() }
    }
}

// MARK: - 条目行

struct ItemRow: View {
    let item: Item

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                if !item.time.isEmpty {
                    Text(item.time)
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(item.overdue ? Color.red : Color.secondary)
                } else if item.kind != "待办" {
                    Text(item.kind).font(.caption)
                        .foregroundStyle(item.overdue ? Color.red : Color.secondary)
                }
                if !item.state.isEmpty {
                    Text(item.state)
                        .font(.caption2.bold())
                        .foregroundStyle(item.state == "DONE" ? Color.secondary : Color.orange)
                }
                if !item.priority.isEmpty {
                    Text("[#\(item.priority)]").font(.caption2).foregroundStyle(.pink)
                }
            }

            HStack(spacing: 4) {
                if !item.parents.isEmpty {
                    Text(item.parents + " /")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                }
                Text(item.title).lineLimit(2)
            }

            HStack(spacing: 8) {
                if let p = item.progressText {
                    Text(p).font(.caption2).foregroundStyle(.secondary)
                }
                if !item.repeatRule.isEmpty {
                    Text("↻\(item.repeatRule)").font(.caption2).foregroundStyle(.blue)
                }
                ForEach(item.tags, id: \.self) { tag in
                    Text(tag)
                        .font(.caption2)
                        .padding(.horizontal, 5).padding(.vertical, 1)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
        .padding(.vertical, 2)
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
                        HStack {
                            Stat(name: "净资产", value: l.net, emphasize: true)
                            Divider()
                            Stat(name: "资产", value: l.assets)
                            Divider()
                            Stat(name: "负债", value: -l.debts)
                        }
                    }
                    Section("账户") {
                        ForEach(l.accounts) { a in
                            HStack {
                                Text(a.name).font(.callout).lineLimit(1)
                                Spacer()
                                Text(money(a.balance))
                                    .font(.callout.monospacedDigit())
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
                                Spacer()
                                Text(money(t.amount)).font(.callout.monospacedDigit())
                            }
                        }
                    }
                }
            } else {
                EmptyHint(show: true, text: "没有账本")
            }
        }
        .navigationTitle("财务")
        .refreshable { await store.reload() }
        .toolbar { SettingsLink() }
    }
}

struct Stat: View {
    let name: String
    let value: Double
    var emphasize = false

    var body: some View {
        VStack(spacing: 2) {
            Text(name).font(.caption).foregroundStyle(.secondary)
            Text(money(value))
                .font(emphasize ? .headline.monospacedDigit() : .callout.monospacedDigit())
        }
        .frame(maxWidth: .infinity)
    }
}

func money(_ v: Double) -> String {
    String(format: "%.2f", v == 0 ? 0 : v) // 把 -0.00 拍平成 0.00
}

// MARK: - 对话

struct ChatView: View {
    @EnvironmentObject var store: Store
    @State private var draft = ""
    @State private var openSettings = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 0) {
            transcript
            inputBar
        }
        .navigationTitle("Jarvis")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                Button("清空") { store.clearChat() }
                    .disabled(store.messages.isEmpty)
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
                        Starters(pick: startWith)
                            .padding(.top, 24)
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
            // 恢复出来的对话直接落在底部，不然打开看到的是最早那几句。
            .defaultScrollAnchor(.bottom)
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
            Divider()
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
            HStack(alignment: .bottom, spacing: 8) {
                // 想说的话可能不止一行，让它自己长，最多六行。
                TextField("说点什么", text: $draft, axis: .vertical)
                    .lineLimit(1...6)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 9)
                    .background(.quaternary, in: RoundedRectangle(cornerRadius: 20))
                    .focused($focused)

                Button(action: submit) {
                    Image(systemName: store.thinking ? "ellipsis" : "arrow.up")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 34, height: 34)
                        .background(canSend ? AnyShapeStyle(.tint)
                                            : AnyShapeStyle(.quaternary), in: Circle())
                }
                .disabled(!canSend)
                .animation(.easeInOut(duration: 0.15), value: canSend)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
        }
        .background(.bar)
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
                        .padding(.horizontal, 12).padding(.vertical, 8)
                        .background(.quaternary, in: Capsule())
                }
                .buttonStyle(.plain)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct Bubble: View {
    let message: Message

    var isUser: Bool { message.role == "user" }

    var body: some View {
        HStack {
            if isUser { Spacer(minLength: 40) }
            Group {
                if isUser {
                    Text(message.content)
                } else {
                    MarkdownText(text: message.content)
                }
            }
                .padding(10)
                .background(isUser ? AnyShapeStyle(.tint) : AnyShapeStyle(.quaternary),
                            in: RoundedRectangle(cornerRadius: 12))
                .foregroundStyle(isUser ? Color.white : Color.primary)
                .textSelection(.enabled)
            if !isUser { Spacer(minLength: 40) }
        }
    }
}

struct ThinkingRow: View {
    let steps: [Step]

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
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
        "plan": "制定计划", "step_done": "标记完成",
    ]
    return labels[name] ?? name
}

// MARK: - 零碎

struct EmptyHint: View {
    let show: Bool
    let text: String

    var body: some View {
        if show {
            ContentUnavailableView(text, systemImage: "tray")
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
