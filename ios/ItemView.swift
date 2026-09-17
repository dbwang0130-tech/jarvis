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

struct ItemRow: View {
    let item: Item
    @State private var showDetail = false

    private var look: ItemLook { ItemLook(item) }

    var body: some View {
        Button { showDetail = true } label: { content }
            .buttonStyle(.plain)
            .sheet(isPresented: $showDetail) { ItemDetail(item: item) }
    }

    private var content: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: look.icon)
                .font(.system(size: 15))
                .foregroundStyle(look.color)
                .frame(width: 20)
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
    private var topLine: some View {
        HStack(spacing: 6) {
            if !item.time.isEmpty {
                Text(item.time)
                    .font(.caption.monospacedDigit().weight(.medium))
                    .foregroundStyle(look.color)
            } else {
                Text(look.label).font(.caption).foregroundStyle(look.color)
            }

            if !item.state.isEmpty, !item.isDone {
                Text(item.state)
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(.secondary)
            }

            if !item.priority.isEmpty {
                Text(item.priority)
                    .font(.caption2.weight(.bold))
                    .foregroundStyle(.white)
                    .frame(width: 15, height: 15)
                    .background(priorityColor(item.priority), in: Circle())
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
            || !item.notes.isEmpty {
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
                if !item.notes.isEmpty {
                    Image(systemName: "text.alignleft")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                }
                ForEach(item.tags.prefix(3), id: \.self) { tag in
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
    @Environment(\.dismiss) private var dismiss

    private var look: ItemLook { ItemLook(item) }

    var body: some View {
        NavigationStack {
            List {
                Section { header }

                if !facts.isEmpty {
                    Section {
                        ForEach(facts, id: \.0) { fact in
                            FactRow(icon: fact.1, name: fact.0, value: fact.2, tint: fact.3)
                        }
                    }
                }

                if !item.boxes.isEmpty {
                    Section("清单") {
                        ForEach(item.boxes) { box in
                            HStack(alignment: .top, spacing: 8) {
                                Image(systemName: box.done ? "checkmark.square.fill" : "square")
                                    .foregroundStyle(box.done ? Color.green : Color.secondary)
                                Text(box.text)
                                    .strikethrough(box.done, color: .secondary)
                                    .foregroundStyle(box.done ? Color.secondary : Color.primary)
                            }
                            .font(.callout)
                        }
                    }
                }

                if !item.notes.isEmpty {
                    Section("正文") {
                        Text(item.notes)
                            .font(.callout)
                            .textSelection(.enabled)
                    }
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
                        .accessibilityLabel("关闭")
                }
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: look.icon).foregroundStyle(look.color)
                Text(look.label).font(.subheadline.weight(.medium)).foregroundStyle(look.color)
                if !item.state.isEmpty {
                    Text(item.state)
                        .font(.caption.weight(.semibold))
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(.quaternary, in: Capsule())
                }
                if !item.priority.isEmpty {
                    Text("优先级 \(item.priority)")
                        .font(.caption.weight(.semibold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 7).padding(.vertical, 2)
                        .background(priorityColor(item.priority), in: Capsule())
                }
            }

            if !item.parents.isEmpty {
                Text(item.parents).font(.caption).foregroundStyle(.tertiary)
            }
            Text(item.title)
                .font(.title3.weight(.semibold))
                .strikethrough(item.isDone, color: .secondary)
                .textSelection(.enabled)

            if item.total > 0 {
                HStack(spacing: 6) {
                    ProgressBar(done: item.done, total: item.total, color: look.color)
                        .frame(height: 5)
                    Text("\(item.done)/\(item.total)")
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
            }

            if !item.tags.isEmpty {
                HStack(spacing: 6) {
                    Image(systemName: "tag").font(.caption2).foregroundStyle(.tertiary)
                    ForEach(item.tags, id: \.self) { tag in
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
    private var facts: [(String, String, String, Color)] {
        var out: [(String, String, String, Color)] = []
        if !item.scheduled.isEmpty {
            out.append(("计划", "clock", item.scheduled, .blue))
        }
        if !item.deadline.isEmpty {
            out.append(("截止", "flag", item.deadline, item.overdue ? .red : .orange))
        }
        if !item.repeatRule.isEmpty {
            out.append(("循环", "arrow.triangle.2.circlepath", item.repeatRule, .blue))
        }
        if !item.effort.isEmpty {
            out.append(("预估", "hourglass", item.effort, .secondary))
        }
        if item.clocked > 0 {
            out.append(("已计时", "stopwatch", duration(item.clocked), .secondary))
        }
        if !item.category.isEmpty {
            out.append(("分类", "folder", item.category, .secondary))
        }
        return out
    }

    private func duration(_ minutes: Int) -> String {
        minutes < 60 ? "\(minutes) 分钟"
                     : "\(minutes / 60) 小时 \(minutes % 60) 分"
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
