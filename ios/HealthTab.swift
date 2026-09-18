import SwiftUI

/// 健康。
///
/// 视觉上跟着系统「健康」App 走：灰底 + 实心白卡片、按类别分区、每类一个
/// 固定颜色和图标、数字排大号单位排小号。这是**有意的**——这一页的内容
/// 跟「健康」里是同一批数据（多半就是从那儿导进来的），长得像那边，
/// 人不用重新学一遍怎么读。
///
/// 别处仍是这个 app 自己的样子（玻璃、品牌蓝）：那些是 Jarvis 的界面，
/// 而这一页是在替系统「健康」转述。
///
/// 这一页只管**看**。记录靠跟 Jarvis 说一句——「体重 72.5」「昨晚睡了
/// 7 小时」比在表单里挑单位、翻日期快得多。
///
/// 底下存的是 HL7 FHIR（NDJSON，一行一条资源）。解析全在 Go 那头，
/// 这边拿到的每个字段都已经是能直接摆上屏幕的字符串——LOINC 查表、
/// 血压分项、单位换算在两边各写一遍，迟早对不上。
struct HealthTab: View {
    @EnvironmentObject var store: Store

    private var health: HealthView? { store.snapshot.health }

    @State private var note = ""
    @State private var picked: HealthVital?

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                if let h = health, !isEmpty {
                    if !h.profile.isEmpty {
                        Text(h.profile)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    // 按类别分区，跟「健康」里「浏览」那一页一个路子。
                    // 十来项平铺下来，分不清哪个跟哪个是一回事。
                    ForEach(HealthGroup.order(in: h.vitals), id: \.self) { group in
                        section(group, h.vitals.filter { $0.group == group.name })
                    }
                    if !h.meals.isEmpty {
                        meals(h.meals)
                    }
                    Text("共 \(h.total) 条记录，存成 HL7 FHIR，跟电脑上是同一份")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity, alignment: .center)
                }
            }
            .padding(.horizontal)
            .padding(.vertical, 12)
        }
        // 灰底衬白卡片，这是「健康」里那套分组列表的底色。
        .background(Color(.systemGroupedBackground))
        .safeAreaInset(edge: .bottom) { importBar }
        .navigationTitle("健康")
        .navigationBarTitleDisplayMode(.inline)
        .overlay { EmptyHint(show: isEmpty && !store.loading, text: emptyText, icon: "heart.text.square") }
        .overlay { LoadingHint(isEmpty: isEmpty) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .refreshable { await store.reload() }
        .sheet(item: $picked) { VitalDetail(vital: $0) }
    }

    private func section(_ group: HealthGroup, _ items: [HealthVital]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            // 分区标题是黑色大字，不是灰色小字——「健康」里就是这样，
            // 它要能在一屏卡片里一眼分出段落来。
            GroupHeader(group: group, text: group.name)
            VStack(spacing: 10) {
                ForEach(items) { v in
                    Button { picked = v } label: { VitalCard(vital: v, tint: group.tint) }
                        .buttonStyle(.plain)
                }
            }
        }
    }

    // MARK: - 饮食

    private func meals(_ items: [HealthEntry]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            GroupHeader(group: .nutrition, text: "饮食")
            VStack(spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.offset) { i, m in
                    if i > 0 { Divider().padding(.leading, 14) }
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(m.when)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                        Text(m.text).font(.subheadline)
                        Spacer(minLength: 0)
                        Text(m.ago).font(.caption2).foregroundStyle(.tertiary)
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 11)
                }
            }
            .background(Color(.secondarySystemGroupedBackground),
                        in: .rect(cornerRadius: Brand.radius))
        }
    }

    // MARK: - 从「健康」导入

    /// 空着的时候这个按钮尤其要在——第一次进来手里已经有几年的数据了，
    /// 不该让人从今天开始重新一条条说。
    @ViewBuilder
    private var importBar: some View {
        if HealthKitImporter.isAvailable {
            VStack(spacing: 6) {
                if !note.isEmpty {
                    Text(note)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                        .padding(.horizontal, 20)
                }
                Button {
                    Task { note = await store.importFromHealthKit() }
                } label: {
                    HStack(spacing: 8) {
                        if store.importingHealth {
                            ProgressView().controlSize(.small)
                        } else {
                            Image(systemName: "heart.circle")
                        }
                        Text(store.importingHealth ? "正在读…" : "从「健康」导入")
                    }
                    .font(.subheadline.weight(.medium))
                    .padding(.horizontal, 16).padding(.vertical, 10)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: .capsule)
                .disabled(store.importingHealth)
            }
            .padding(.bottom, 10)
        }
    }

    private var isEmpty: Bool {
        guard let h = health else { return true }
        return h.vitals.isEmpty && h.meals.isEmpty
    }

    private var emptyText: String {
        HealthKitImporter.isAvailable
            ? "还没有记录。\n可以从下面把「健康」里的数据导进来，"
                + "也可以直接跟 Jarvis 说「体重 72.5」「昨晚睡了 7 小时」。"
            : "还没有记录。\n跟 Jarvis 说「体重 72.5」「昨晚睡了 7 小时」「早上吃了小米粥」就行。"
    }
}

/// 分区标题：彩色图标 + 黑色粗体。
struct GroupHeader: View {
    let group: HealthGroup
    let text: String

    var body: some View {
        HStack(spacing: 7) {
            Image(systemName: group.icon)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(group.tint)
            Text(text).font(.title3.weight(.bold))
        }
    }
}

/// 一类指标。名字由 Go 那张体征表给（`HealthVital.group`），
/// 这边只负责它长什么样——颜色和图标是界面的事，不是 FHIR 的事。
///
/// 配色跟着系统「健康」：身体测量紫、心脏红、活动橙、睡眠青、呼吸蓝绿、
/// 化验靛蓝、饮食绿。认不出来的走 `.other`，是灰的但**不会消失**——
/// Go 那边新加一类，这儿最差也就是没上色，不至于整段不显示。
enum HealthGroup: String, CaseIterable, Hashable {
    case body = "身体测量"
    case heart = "心脏"
    case activity = "活动"
    case sleep = "睡眠"
    case respiratory = "呼吸"
    case lab = "化验"
    case nutrition = "饮食"
    case other = "其他"

    var name: String { rawValue }

    var tint: Color {
        switch self {
        case .body:        return .purple
        case .heart:       return .red
        case .activity:    return .orange
        case .sleep:       return .teal
        case .respiratory: return .cyan
        case .lab:         return .indigo
        case .nutrition:   return .green
        case .other:       return .secondary
        }
    }

    var icon: String {
        switch self {
        case .body:        return "figure.stand"
        case .heart:       return "heart.fill"
        case .activity:    return "figure.walk"
        case .sleep:       return "bed.double.fill"
        case .respiratory: return "lungs.fill"
        case .lab:         return "drop.fill"
        case .nutrition:   return "fork.knife"
        case .other:       return "square.grid.2x2"
        }
    }

    static func of(_ name: String) -> HealthGroup {
        HealthGroup(rawValue: name) ?? .other
    }

    /// 这批数据里出现过的类别，按枚举顺序。
    static func order(in vitals: [HealthVital]) -> [HealthGroup] {
        let present = Set(vitals.map { HealthGroup.of($0.group) })
        return allCases.filter { present.contains($0) }
    }
}

/// 一项指标。
///
/// 排版照着「健康」里那种卡片：第一行是项目名和时间，第二行是**大号数字 +
/// 小号单位**。数字和单位同一个字号的话，一眼扫过去分不出「72.5」和
/// 「kg」哪个是重点。
struct VitalCard: View {
    let vital: HealthVital
    var tint: Color = .secondary

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(vital.kind)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(tint)
                Spacer(minLength: 0)
                Text(vital.ago)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                if vital.hasTrendLine {
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.tertiary)
                }
            }

            HStack(alignment: .firstTextBaseline, spacing: 3) {
                Text(vital.number)
                    .font(.system(.title, design: .rounded).weight(.semibold))
                    .monospacedDigit()
                Text(vital.unit)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                if !vital.trend.isEmpty {
                    // 只说涨了落了，**不判断好坏**。体重掉了未必是好事，
                    // 血压高一点也未必要紧——那是医生的活，涂成红绿两色
                    // 等于替人下了结论。
                    Text(vital.trend)
                        .font(.caption.monospacedDigit().weight(.medium))
                        .foregroundStyle(.secondary)
                        .padding(.leading, 4)
                }
                Spacer(minLength: 8)
                if vital.hasTrendLine {
                    Sparkline(points: vital.points, tint: tint, bars: vital.isTotal)
                        .frame(width: 92)
                }
            }
            .lineLimit(1)
            .minimumScaleFactor(0.7)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color(.secondarySystemGroupedBackground),
                    in: .rect(cornerRadius: Brand.radius))
    }
}
