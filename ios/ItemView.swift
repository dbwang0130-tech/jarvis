import SwiftUI

// 日程和待办共用同一种行。手机屏幕窄，一眼要能分出「什么事、什么时候、
// 急不急」，所以靠图标和颜色分类，细节留到点进去看。

/// 条目的成色：图标 + 颜色 + 一句话说明它属于哪一类。
/// 判断顺序有讲究——做完了就不该再标成红的逾期。
struct ItemLook {
    var icon: String
    var color: Color
    var label: String

    init(_ item: Item) {
        if item.isDone {
            self = .init(icon: "checkmark.circle.fill", color: .green, label: "已完成")
        } else if item.overdue {
            self = .init(icon: "exclamationmark.triangle.fill", color: .red, label: "逾期")
        } else {
            switch item.kind {
            case "截止":
                self = .init(icon: "flag.fill", color: .orange, label: "截止")
            case "事件":
                self = .init(icon: "calendar", color: .purple, label: "事件")
            case "计划":
                self = .init(icon: item.time.isEmpty ? "calendar.badge.clock" : "clock.fill",
                             color: .blue, label: "计划")
            default:
                self = .init(icon: "circle", color: .secondary, label: "待办")
            }
        }
    }

    private init(icon: String, color: Color, label: String) {
        self.icon = icon
        self.color = color
        self.label = label
    }
}

/// 分类的图标。org 的 CATEGORY 没设时取文件名，所以 work.org 就是 work。
///
/// 图标和颜色各管一件事：**图标说这是哪一摊事，颜色说急不急**。
/// 两个维度都用颜色，一眼扫过去就分不清哪个是哪个了。
func categoryIcon(_ category: String) -> String {
    let c = category.lowercased()
    let table: [(keys: [String], icon: String)] = [
        (["work", "工作", "职", "公司", "项目"], "briefcase.fill"),
        (["life", "生活", "日常", "家务"], "house.fill"),
        (["inbox", "收件", "待整理"], "tray.fill"),
        (["study", "learn", "学习", "读书", "thesis", "论文", "课"], "book.fill"),
        (["health", "健康", "运动", "健身", "医"], "heart.fill"),
        (["money", "财务", "账", "理财"], "creditcard.fill"),
        (["family", "家人", "亲"], "person.2.fill"),
        (["travel", "旅行", "出行"], "airplane"),
        (["meeting", "会议", "会"], "person.3.fill"),
        (["idea", "想法", "灵感"], "lightbulb.fill"),
    ]
    for row in table where row.keys.contains(where: { c.contains($0) }) {
        return row.icon
    }
    return "folder.fill"
}

/// 优先级的颜色。A 最急，往下依次降温；没设的不显示。
func priorityColor(_ p: String) -> Color {
    switch p {
    case "A": return .red
    case "B": return .orange
    case "C": return .blue
    default: return .secondary
    }
}

// MARK: - 列表里的一行

/// 一行上能直接做的事。放在这儿是因为列表和详情页要用同一套，
/// 各写各的迟早出现「列表上叫推迟一天、详情页里叫明天」这种。
enum ItemAction {
    static func label(_ op: String) -> (String, String, Color) {
        switch op {
        case "done":     return ("完成", "checkmark", .green)
        case "undone":   return ("没做完", "arrow.uturn.backward", .gray)
        case "start":    return ("开始", "play.fill", .blue)
        case "stop":     return ("停止", "stop.fill", .orange)
        case "postpone": return ("推迟一天", "calendar.badge.plus", .indigo)
        case "cancel":   return ("不做了", "xmark", .red)
        default:         return (op, "questionmark", .gray)
        }
    }
}

/// 给一行挂上左右滑动的操作。列表和待办页共用，各写各的迟早走散。
struct SwipeActions: ViewModifier {
    @EnvironmentObject var store: Store
    let item: Item

    func body(content: Content) -> some View {
        content
            // 右滑（前缘）是「推进一步」：没做完就完成，做完了就退回来。
            .swipeActions(edge: .leading, allowsFullSwipe: true) {
                if item.isDone {
                    button("undone")
                } else {
                    button("done")
                }
            }
            // 左滑（后缘）是别的选择，全划不触发任何一个——这边都不是
            // 「一划到底」该自动做的事。
            .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                if !item.isDone {
                    button("postpone", days: 1)
                    button(item.running ? "stop" : "start")
                    button("cancel")
                }
            }
    }

    private func button(_ op: String, days: Int = 0) -> some View {
        let (title, icon, color) = ItemAction.label(op)
        return Button {
            Task {
                if await store.act(op, on: item.ref, days: days) {
                    Haptics.done()
                } else {
                    Haptics.failure()
                }
            }
        } label: {
            Label(title, systemImage: icon)
        }
        .tint(color)
    }
}

extension View {
    func itemSwipeActions(_ item: Item) -> some View {
        modifier(SwipeActions(item: item))
    }
}

/// 行只管画。点开谁由列表统一管——每行各挂一个 .sheet 的话，
/// 列表有多少行就建多少套呈现状态，滚起来明显发涩。
struct ItemRow: View {
    let item: Item

    private var look: ItemLook { ItemLook(item) }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            leadingIcon
                .frame(width: 22)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 5) {
                topLine
                titleLine
                bottomLine
            }

            Spacer(minLength: 0)
        }
        .padding(.vertical, 3)
        .contentShape(Rectangle())
    }

    /// 时间、状态、优先级——「什么时候、什么状态、多急」。
    /// 分类图标染上「急不急」的颜色。逾期另挂一个角标——那是最要紧的信息，
    /// 光靠颜色不够响；做完了直接换成绿勾，不必再提它属于哪一摊。
    @ViewBuilder
    private var leadingIcon: some View {
        if item.running {
            // 正在计时的那条换成秒表。不换的话，列表上跟别的条目一模一样，
            // 「哪件事还开着」只能靠自己记。
            Image(systemName: "stopwatch.fill")
                .font(.system(.subheadline))
                .foregroundStyle(.orange)
        } else if item.isDone {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(.subheadline))
                .foregroundStyle(.green)
        } else {
            Image(systemName: categoryIcon(item.category))
                .font(.system(.subheadline))
                .foregroundStyle(look.color)
                .overlay(alignment: .topTrailing) {
                    if item.overdue {
                        Image(systemName: "exclamationmark.circle.fill")
                            .font(.system(size: 9))
                            .foregroundStyle(.red)
                            .background(Circle().fill(Color(.systemBackground)))
                            .offset(x: 5, y: -5)
                    }
                }
        }
    }

    private var topLine: some View {
        HStack(spacing: 6) {
            if !item.time.isEmpty {
                Text(item.time)
                    .font(.caption.monospacedDigit().weight(.medium))
                    .foregroundStyle(look.color)
            } else if !item.dueText.isEmpty {
                // 「今天」「明天」「逾期 3 天」比「待办」两个字有用得多，
                // 也比「2026-09-20」省一次心算。
                Text(item.dueText)
                    .font(.caption.weight(.medium))
                    .foregroundStyle(look.color)
            } else {
                Text(look.label).font(.caption).foregroundStyle(look.color)
            }

            // 图标是猜出来的，认不出就退回 folder——把分类名写出来兜底。
            if !item.category.isEmpty {
                Text(item.category)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }

            if !item.state.isEmpty, !item.isDone {
                Text(item.stateLabel)
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
            }

            if !item.priority.isEmpty {
                Text(item.priority)
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.white)
                    .frame(width: 16, height: 16)
                    .background(priorityColor(item.priority).gradient, in: Circle())
            }
        }
    }

    private var titleLine: some View {
        VStack(alignment: .leading, spacing: 1) {
            if !item.parents.isEmpty {
                Text(item.parents)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                    .lineLimit(1)
            }
            Text(item.title)
                .font(.callout)
                .lineLimit(2)
                .strikethrough(item.isDone, color: .secondary)
                .foregroundStyle(item.isDone ? Color.secondary : Color.primary)
        }
    }

    /// 进度、循环、标签——这些是「附加说明」，没有就不占地方。
    @ViewBuilder
    private var bottomLine: some View {
        if item.total > 0 || !item.repeatRule.isEmpty || !item.tags.isEmpty
            || item.hasNotes {
            HStack(spacing: 8) {
                if item.total > 0 {
                    HStack(spacing: 4) {
                        ProgressBar(done: item.done, total: item.total, color: look.color)
                            .frame(width: 34, height: 4)
                        Text("\(item.done)/\(item.total)")
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                }
                if !item.repeatRule.isEmpty {
                    Label(item.repeatRule, systemImage: "arrow.triangle.2.circlepath")
                        .font(.caption2)
                        .foregroundStyle(.blue)
                        .labelStyle(.tight)
                }
                if item.hasNotes {
                    Image(systemName: "text.alignleft")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                ForEach(Array(item.tags.prefix(3).enumerated()), id: \.offset) { _, tag in
                    Text(tag)
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                        .padding(.horizontal, 5).padding(.vertical, 1)
                        .background(.quaternary, in: Capsule())
                }
            }
        }
    }
}

/// 图标和文字挨紧一点，默认的 Label 间距在小字号下太散。
struct TightLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 2) {
            configuration.icon
            configuration.title
        }
    }
}

extension LabelStyle where Self == TightLabelStyle {
    static var tight: TightLabelStyle { TightLabelStyle() }
}

struct ProgressBar: View {
    let done: Int
    let total: Int
    var color: Color = .accentColor

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(.quaternary)
                Capsule().fill(color)
                    .frame(width: geo.size.width * ratio)
            }
        }
    }

    private var ratio: CGFloat {
        total > 0 ? min(1, CGFloat(done) / CGFloat(total)) : 0
    }
}

// MARK: - 详情

struct ItemDetail: View {
    let item: Item
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    @State private var data: DetailData?
    @State private var askDelete = false
    @State private var deleting = false

    /// 这一条**现在**的样子。
    ///
    /// 传进来的 item 是打开那一刻的拷贝。改完优先级、换了状态之后
    /// snapshot 会更新，但那份拷贝不会——胶囊上仍显示旧值，看着像没生效。
    /// 所以按 ref 去 snapshot 里取当前这份，取不到（比如刚被删掉）
    /// 就退回原来那份。
    private var live: Item {
        store.snapshot.todos.first { $0.ref == item.ref }
            ?? store.snapshot.days.flatMap(\.items).first { $0.ref == item.ref }
            ?? item
    }

    private var look: ItemLook { ItemLook(live) }

    var body: some View {
        NavigationStack {
            List {
                Section { actions }
                    .listRowInsets(EdgeInsets(top: 6, leading: 12,
                                              bottom: 6, trailing: 12))
                    .listRowBackground(Color.clear)

                Section {
                    header
                        .listRowInsets(EdgeInsets(top: 12, leading: 12,
                                                  bottom: 12, trailing: 12))
                        .listRowBackground(
                            RoundedRectangle(cornerRadius: Brand.radius)
                                .fill(Brand.soft)
                                .padding(.vertical, 2))
                }

                if let data {
                    // 时间这两行**摆出来就该能改**。以前只能靠「推迟一天」
                    // 往后挪整天，排到「下周三下午三点」或者取消排期，
                    // 都得退出去跟 Jarvis 说一句。
                    Section {
                        TimeRow(kind: .scheduled, value: data.scheduled,
                                item: live, data: $data)
                        TimeRow(kind: .deadline, value: data.deadline,
                                item: live, data: $data)
                        ForEach(facts(data), id: \.0) { fact in
                            FactRow(icon: fact.1, name: fact.0, value: fact.2, tint: fact.3)
                        }
                    }

                    if !data.boxes.isEmpty {
                        Section("清单") {
                            ForEach(Array(data.boxes.enumerated()), id: \.offset) { idx, box in
                                CheckboxRow(box: box, ref: item.ref,
                                            index: idx + 1, data: $data)
                            }
                        }
                    }

                    if !data.notes.isEmpty {
                        Section("正文") {
                            Text(data.notes).font(.callout).textSelection(.enabled)
                        }
                    }

                    // 「做过什么」这一节一直只能看：LOGBOOK 里的记录都列出来了，
                    // 却没地方往里添一笔。空着的时候也要有入口——一件事刚开始
                    // 做，正是最该记两句的时候。
                    Section("做过什么") {
                        ForEach(Array(data.log.enumerated()), id: \.offset) { LogRow(entry: $1) }
                        NoteButton(ref: live.ref, data: $data)
                    }

                    if let err = data.error, !err.isEmpty {
                        Section { Text(err).font(.footnote).foregroundStyle(.red) }
                    }
                } else {
                    Section {
                        HStack { ProgressView().controlSize(.small); Text("读取中…") }
                            .foregroundStyle(.secondary)
                            .font(.footnote)
                    }
                }

                // 删掉一条一直没有出路：「做完了」该标完成，可「记错了」
                // 「这条本来就不该在这儿」以前只能到电脑上去改文件。
                //
                // 摆在最底下、红色、要点头——它跟上面那排动作不是一类，
                // 上面那些都能再改回来。
                Section {
                    Button(role: .destructive) { askDelete = true } label: {
                        Label("删掉这一条", systemImage: "trash")
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .disabled(deleting || store.thinking)
                }

                Section {
                    LabeledContent("来自", value: item.file)
                    LabeledContent("引用", value: item.ref)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .navigationTitle("条目")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("关掉")
                }
            }
            .confirmationDialog("删掉「\(live.title)」？", isPresented: $askDelete,
                                titleVisibility: .visible) {
                Button("删掉", role: .destructive) { remove() }
                Button("算了", role: .cancel) {}
            } message: {
                Text("这条连同它的正文、清单、记录一起没了，撤不回来。"
                     + "做完了的话标完成就行。")
            }
        }
        // 正文和勾选项不在列表里，点开才去 Go 那边取这一条。
        .task(id: item.ref) { data = await store.detail(of: item.ref) }
    }

    /// 删掉这一条。
    ///
    /// 带上标题：delete_item 拿它跟 ref 指着的那条核对，对不上就拒绝。
    /// ref 是行号，文件在别处被改过之后同一个行号可能已经是另一条了——
    /// 这道保险不能因为「界面上点的就是那一条」而绕过去。
    private func remove() {
        deleting = true
        Task {
            let ok = await store.act("delete", on: live.ref, title: live.title)
            deleting = false
            if ok {
                Haptics.warning()
                dismiss()
            } else {
                Haptics.failure()
            }
        }
    }

    /// 常用动作摆在最上面。这些都不经过模型——界面已经握着 ref 了，
    /// 为「标完成」跑一趟模型是好几秒加一次 API 调用。
    private var actions: some View {
        GlassEffectContainer(spacing: 8) {
            HStack(spacing: 8) {
                if live.isDone {
                    ActionButton(op: "undone", item: live) { dismiss() }
                } else {
                    ActionButton(op: "done", item: live) { dismiss() }
                    // 正在走的时候要给的是「停止」。以前这儿永远是「开始」，
                    // 点下去只会再开一段计时，想收尾只能标完成——
                    // 一件没做完的事因此没有别的出路。
                    ActionButton(op: live.running ? "stop" : "start", item: live)
                    PostponeButton(item: live)
                    // 「不做了」原先只在列表左滑里有。点进详情看完才决定
                    // 不做的，得先退出去、找到那一行、再左滑——而刚才
                    // 就在这一条上。
                    ActionButton(op: "cancel", item: live) { dismiss() }
                }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            // 状态和优先级**摆出来就该能改**。以前这两块是纯标签：看得见
            // 「等回复」「优先级 A」，想动它得退出去跟 Jarvis 说一句，
            // 而人就站在这一条上。
            HStack(spacing: 8) {
                Image(systemName: look.icon).foregroundStyle(look.color)
                Text(look.label).font(.subheadline.weight(.medium)).foregroundStyle(look.color)
                StatePicker(item: live, states: data?.states ?? [], data: $data)
                PriorityPicker(item: live, data: $data)
                Spacer(minLength: 0)
            }

            if !item.parents.isEmpty {
                Text(item.parents).font(.caption).foregroundStyle(.tertiary)
            }
            Text(live.title)
                .font(.title3.weight(.semibold))
                .strikethrough(live.isDone, color: .secondary)
                .textSelection(.enabled)

            if live.total > 0 {
                HStack(spacing: 6) {
                    ProgressBar(done: live.done, total: live.total, color: look.color)
                        .frame(height: 5)
                    Text("\(live.done)/\(live.total)")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
            }

            if !item.tags.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "tag").font(.caption2).foregroundStyle(.tertiary)
                    // 同一个条目上重复的标签会撞 id，按下标来。
                    ForEach(Array(item.tags.enumerated()), id: \.offset) { _, tag in
                        Text(tag)
                            .font(.caption2)
                            .padding(.horizontal, 6).padding(.vertical, 2)
                            .background(.quaternary, in: Capsule())
                    }
                }
            }
        }
        .padding(.vertical, 4)
    }

    /// 有值才摆出来，空行比没有更碍眼。
    private func facts(_ d: DetailData) -> [(String, String, String, Color)] {
        var out: [(String, String, String, Color)] = []
        // 计划和截止不在这儿——它们是可以改的，走 TimeRow。
        if !item.repeatRule.isEmpty {
            out.append(("循环", "arrow.triangle.2.circlepath", item.repeatRule, .blue))
        }
        if !d.effort.isEmpty {
            out.append(("预估", "hourglass", d.effort, .secondary))
        }
        if d.clocked > 0 {
            out.append(("已计时", "stopwatch", duration(d.clocked), .secondary))
        }
        if !item.category.isEmpty {
            out.append(("分类", categoryIcon(item.category), item.category, .secondary))
        }
        return out
    }

    private func duration(_ minutes: Int) -> String {
        minutes < 60 ? "\(minutes) 分钟"
                     : "\(minutes / 60) 小时 \(minutes % 60) 分"
    }
}

/// 清单里的一项，**能点**。
///
/// 以前这儿是个纯展示的方框：画着复选框却按不动，看着像坏了。org 那边
/// 一直有勾选的能力（ToggleCheckbox），只是一直没接出来——工具、桥、界面
/// 三层都缺一段。
///
/// 点的时候先把框自己改掉，不等文件写回来。改的是 iCloud 里的文件，
/// 一来一回有零点几秒，那段时间里方框一动不动，人会以为没点上又点一次。
/// 写回失败就弹回原样，并由 ErrorBar 说明缘由。
struct CheckboxRow: View {
    @EnvironmentObject var store: Store
    let box: Checkbox
    let ref: String
    let index: Int
    /// 勾完要把整条重新取一遍——进度、别的框、标题上的 [n/m] 都可能跟着变，
    /// 所以直接把详情那份数据交给这一行去换。
    @Binding var data: DetailData?

    @State private var optimistic: Bool?
    @State private var working = false

    private var done: Bool { optimistic ?? box.done }

    var body: some View {
        Button {
            guard !working else { return }
            working = true
            optimistic = !done
            Haptics.done()
            Task {
                let ok = await store.act("check", on: ref, index: index)
                if ok {
                    data = await store.detail(of: ref)
                } else {
                    optimistic = nil        // 没成，弹回去
                    Haptics.failure()
                }
                working = false
            }
        } label: {
            HStack(alignment: .top, spacing: 8) {
                Image(systemName: done ? "checkmark.square.fill" : "square")
                    .foregroundStyle(done ? Color.green : Color.secondary)
                    .contentTransition(.symbolEffect(.replace))
                Text(box.text)
                    .strikethrough(done, color: .secondary)
                    .foregroundStyle(done ? Color.secondary : Color.primary)
                Spacer(minLength: 0)
            }
            .font(.callout)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(store.thinking)
        .animation(.snappy(duration: 0.2), value: done)
        // 整行是一个开关，读屏时别拆成「勾」和一段文字两下念。
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(box.text)
        .accessibilityValue(done ? "已勾选" : "未勾选")
        .accessibilityAddTraits(.isButton)
    }
}

/// 往 LOGBOOK 里记一笔。
///
/// 记的是「做的过程中发生了什么」，跟条目正文不是一回事：正文是这件事
/// 本身的说明，笔记是带时间戳的流水——org 那边也是分开存的（- Note taken on）。
struct NoteButton: View {
    @EnvironmentObject var store: Store
    let ref: String
    @Binding var data: DetailData?

    @State private var writing = false
    @State private var text = ""
    @State private var working = false
    @FocusState private var focused: Bool

    var body: some View {
        Button {
            text = ""
            writing = true
        } label: {
            HStack(spacing: 10) {
                if working {
                    ProgressView().controlSize(.small).frame(width: 18)
                } else {
                    Image(systemName: "square.and.pencil")
                        .font(.footnote)
                        .foregroundStyle(Brand.primary)
                        .frame(width: 18)
                }
                Text("记一笔").foregroundStyle(Brand.primary)
                Spacer(minLength: 0)
            }
            .font(.callout)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(working || store.thinking)
        .sheet(isPresented: $writing) { editor }
    }

    private var editor: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("这会儿是什么情况", text: $text, axis: .vertical)
                        .lineLimit(3...8)
                        .focused($focused)
                } footer: {
                    Text("记进这条的 LOGBOOK，带上时间。")
                }
            }
            .navigationTitle("记一笔")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("算了") { writing = false }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("记下") { save() }
                        .disabled(text.trimmed.isEmpty)
                }
            }
            .onAppear { focused = true }
        }
        .presentationDetents([.medium])
    }

    private func save() {
        let what = text.trimmed
        guard !what.isEmpty else { return }
        writing = false
        working = true
        Task {
            let ok = await store.act("note", on: ref, text: what)
            working = false
            if ok {
                Haptics.done()
                data = await store.detail(of: ref)
            } else {
                Haptics.failure()
            }
        }
    }
}

/// 计划 / 截止那一行，点开能改。
///
/// 以前这两行是纯展示：日期摆在那儿，想挪只能靠「推迟一天」一天天点，
/// 或者退出去跟 Jarvis 说一句。而 org 里这两项本来就各是一条可以直接
/// 写回去的时间戳。
///
/// **带不带时刻是两回事**：不带时刻的条目在日程里是整天的事，不会到点
/// 弹提醒——「买牛奶」不需要在某个精确时刻打断你。所以选择器里那个
/// 「几点」是可以关掉的，关掉就只写日期。
struct TimeRow: View {
    enum Kind {
        case scheduled, deadline

        var name: String { self == .scheduled ? "计划" : "截止" }
        var icon: String { self == .scheduled ? "clock" : "flag" }
        var op: String { self == .scheduled ? "schedule" : "deadline" }
    }

    @EnvironmentObject var store: Store
    let kind: Kind
    let value: String
    let item: Item
    @Binding var data: DetailData?

    @State private var picking = false
    @State private var at = Date()
    @State private var withTime = false
    @State private var working = false

    private var tint: Color {
        if kind == .deadline { return item.overdue ? .red : .orange }
        return .blue
    }

    var body: some View {
        Button {
            at = parsed ?? defaultTime
            withTime = value.contains(":")
            picking = true
        } label: {
            HStack(spacing: 10) {
                Image(systemName: kind.icon)
                    .font(.footnote)
                    .foregroundStyle(value.isEmpty ? Color.secondary : tint)
                    .frame(width: 18)
                Text(kind.name).foregroundStyle(.secondary)
                Spacer()
                if working {
                    ProgressView().controlSize(.small)
                } else {
                    Text(value.isEmpty ? "没定" : value)
                        .monospacedDigit()
                        .foregroundStyle(value.isEmpty ? Color.secondary : Color.primary)
                }
                Image(systemName: "chevron.right")
                    .font(.caption2).foregroundStyle(.tertiary)
            }
            .font(.callout)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(working || store.thinking)
        .sheet(isPresented: $picking) { picker }
    }

    private var picker: some View {
        NavigationStack {
            Form {
                Section {
                    DatePicker("哪天", selection: $at,
                               displayedComponents: withTime ? [.date, .hourAndMinute] : [.date])
                    Toggle("定个时刻", isOn: $withTime)
                } footer: {
                    Text(withTime ? "到点会提醒你。"
                                  : "不定时刻就是整天的事，不会到点打断你。")
                }

                if !value.isEmpty {
                    Section {
                        Button("去掉\(kind.name)", role: .destructive) { save(clear: true) }
                    }
                }
            }
            .navigationTitle(kind.name)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("算了") { picking = false }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("存") { save(clear: false) }
                }
            }
        }
        .presentationDetents([.medium])
    }

    /// 没定过的时候默认落在哪儿：计划落今天，截止落一周后。
    /// 都给「现在」的话，截止时间一设上就已经过期了。
    private var defaultTime: Date {
        kind == .scheduled ? Date() : Date().addingTimeInterval(7 * 86400)
    }

    /// 把 org 里那串读回来，好让选择器从当前值开始。认不出就算了。
    private var parsed: Date? {
        guard !value.isEmpty else { return nil }
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        for fmt in ["yyyy-MM-dd HH:mm", "yyyy-MM-dd"] {
            f.dateFormat = fmt
            if let d = f.date(from: String(value.prefix(fmt.count))) { return d }
        }
        return nil
    }

    private func save(clear: Bool) {
        picking = false
        working = true
        let text = clear ? "" : orgTime
        Task {
            let ok = await store.act(kind.op, on: item.ref, scheduled: text)
            working = false
            if ok {
                Haptics.done()
                data = await store.detail(of: item.ref)
            } else {
                Haptics.failure()
            }
        }
    }

    /// 写成 org 认得的样子。org 那边收 "2006-01-02" 或 "2006-01-02 15:04"。
    private var orgTime: String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = withTime ? "yyyy-MM-dd HH:mm" : "yyyy-MM-dd"
        return f.string(from: at)
    }
}

/// 状态那颗胶囊，点开能换。
///
/// 可选的关键字由 Go 给（`DetailData.states`）——那是每份 org 文件用
/// `#+TODO:` 自己定的，写死一份「TODO/NEXT/DONE」的话，用了自定义状态
/// 的人在手机上只能在别人的状态之间切。
struct StatePicker: View {
    @EnvironmentObject var store: Store
    let item: Item
    let states: [String]
    @Binding var data: DetailData?

    var body: some View {
        // 还没取到关键字表（详情正在读）就先摆个不能点的，别闪。
        if states.isEmpty {
            if !item.state.isEmpty { label(item.stateLabel, dim: true) }
        } else {
            Menu {
                ForEach(states, id: \.self) { st in
                    Button {
                        change(st)
                    } label: {
                        if st == item.state {
                            Label(st, systemImage: "checkmark")
                        } else {
                            Text(st)
                        }
                    }
                }
            } label: {
                label(item.state.isEmpty ? "没有状态" : item.stateLabel, dim: false)
            }
            .disabled(store.thinking)
        }
    }

    private func label(_ text: String, dim: Bool) -> some View {
        Text(text)
            .font(.caption.weight(.semibold))
            .foregroundStyle(dim ? AnyShapeStyle(.secondary) : AnyShapeStyle(Color.primary))
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(.quaternary, in: Capsule())
    }

    private func change(_ state: String) {
        guard state != item.state else { return }
        Task {
            let ok = await store.act("state", on: item.ref, state: state)
            if ok {
                Haptics.done()
                data = await store.detail(of: item.ref)
            } else {
                Haptics.failure()
            }
        }
    }
}

/// 优先级那颗胶囊，点开能改，也能去掉。
struct PriorityPicker: View {
    @EnvironmentObject var store: Store
    let item: Item
    @Binding var data: DetailData?

    var body: some View {
        Menu {
            ForEach(["A", "B", "C"], id: \.self) { p in
                Button {
                    change(p)
                } label: {
                    if p == item.priority {
                        Label("优先级 \(p)", systemImage: "checkmark")
                    } else {
                        Text("优先级 \(p)")
                    }
                }
            }
            if !item.priority.isEmpty {
                Divider()
                Button("去掉优先级", role: .destructive) { change("") }
            }
        } label: {
            if item.priority.isEmpty {
                Text("定个优先级")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 7).padding(.vertical, 2)
                    .background(.quaternary, in: Capsule())
            } else {
                Text("优先级 \(item.priority)")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.white)
                    .padding(.horizontal, 7).padding(.vertical, 2)
                    .background(priorityColor(item.priority), in: Capsule())
            }
        }
        .disabled(store.thinking)
    }

    private func change(_ priority: String) {
        guard priority != item.priority else { return }
        Task {
            let ok = await store.act("priority", on: item.ref, priority: priority)
            if ok {
                Haptics.done()
                data = await store.detail(of: item.ref)
            } else {
                Haptics.failure()
            }
        }
    }
}

/// 推迟。**点一下推一天，按住挑几天。**
///
/// 以前这儿写死推一天：想推到周末，要么点五下（每点一下都是一次读盘写盘），
/// 要么开口跟 Jarvis 说一句。而 Act 本来就收 days，只是界面没把它给出来。
///
/// 点一下仍然是推一天——最常用的那下不该多花一步；别的天数收在长按菜单里，
/// 不占那一行本来就不宽的地方。
struct PostponeButton: View {
    @EnvironmentObject var store: Store
    let item: Item

    @State private var working = false

    /// 挑得最多的几档。「一周后」按 7 天算而不是「下周一」——org 里
    /// SCHEDULED 是个日期，说七天后最不容易理解错。
    private let choices: [(String, Int)] = [
        ("明天", 1), ("后天", 2), ("三天后", 3), ("一周后", 7), ("两周后", 14),
    ]

    var body: some View {
        let (title, icon, color) = ItemAction.label("postpone")
        Menu {
            ForEach(Array(choices.enumerated()), id: \.offset) { _, c in
                Button(c.0) { push(c.1) }
            }
        } label: {
            VStack(spacing: 4) {
                if working {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: icon).font(.system(.subheadline, weight: .semibold))
                }
                Text(title).font(.caption2)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .foregroundStyle(color)
        } primaryAction: {
            // 直接点就是推一天，跟以前一样。
            push(1)
        }
        .buttonStyle(.plain)
        .disabled(working || store.thinking)
        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: Brand.smallRadius))
        .accessibilityLabel("推迟一天")
        .accessibilityHint("长按可以挑几天")
    }

    private func push(_ days: Int) {
        working = true
        Task {
            let ok = await store.act("postpone", on: item.ref, days: days)
            working = false
            ok ? Haptics.done() : Haptics.failure()
        }
    }
}

/// 一个动作按钮。做完了给个触感反馈——改的是文件，看不见摸不着，
/// 不给点回应会让人不确定到底有没有生效。
struct ActionButton: View {
    @EnvironmentObject var store: Store
    let op: String
    let item: Item
    var done: (() -> Void)?

    @State private var working = false

    var body: some View {
        let (title, icon, color) = ItemAction.label(op)
        Button {
            working = true
            Task {
                let ok = await store.act(op, on: item.ref, days: 1)
                working = false
                if ok {
                    Haptics.done()
                    done?()
                } else {
                    Haptics.failure()
                }
            }
        } label: {
            VStack(spacing: 4) {
                if working {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: icon).font(.system(.subheadline, weight: .semibold))
                }
                Text(title).font(.caption2)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 10)
            .foregroundStyle(color)
        }
        .buttonStyle(.plain)
        .disabled(working || store.thinking)
        .glassEffect(.regular.interactive(), in: .rect(cornerRadius: Brand.smallRadius))
    }
}

/// LOGBOOK 里的一条。计时、状态变更、笔记混在一条时间线上——
/// 看的人不关心 org 把它们分开存在哪儿，只想知道这件事都发生过什么。
struct LogRow: View {
    let entry: LogItem

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: icon)
                .font(.footnote)
                .foregroundStyle(color)
                .frame(width: 18)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    if entry.kind == "clock" {
                        Text(entry.duration)
                            .font(.callout.weight(.medium).monospacedDigit())
                            .foregroundStyle(entry.running ? Color.orange : Color.primary)
                    } else if !entry.text.isEmpty {
                        Text(entry.text)
                            .font(.callout)
                            .textSelection(.enabled)
                    }
                }
                HStack(spacing: 6) {
                    if !entry.when.isEmpty {
                        Text(entry.when)
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                    if entry.kind == "clock", !entry.text.isEmpty {
                        Text(entry.text).font(.caption2).foregroundStyle(.tertiary)
                    }
                }
            }
        }
        .padding(.vertical, 1)
    }

    private var icon: String {
        switch entry.kind {
        case "clock": return entry.running ? "stopwatch.fill" : "stopwatch"
        case "state": return "arrow.triangle.branch"
        case "note":  return "text.quote"
        default:      return "circle.dotted"
        }
    }

    private var color: Color {
        switch entry.kind {
        case "clock": return entry.running ? .orange : .blue
        case "state": return .green
        case "note":  return .secondary
        // Color 没有 tertiary（那是 ShapeStyle 上的），拿次要色淡一档代替。
        default:      return .secondary.opacity(0.6)
        }
    }
}

struct FactRow: View {
    let icon: String
    let name: String
    let value: String
    var tint: Color = .secondary

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: icon)
                .font(.footnote)
                .foregroundStyle(tint)
                .frame(width: 18)
            Text(name).foregroundStyle(.secondary)
            Spacer()
            Text(value).monospacedDigit()
        }
        .font(.callout)
    }
}
