import Charts

// HealthGroup 原来在 HealthTab.swift 里。健康页删掉之后只剩这儿在用
// （VitalDetail 的配色和图标），就跟着搬过来了。它是**界面的东西**——
// 名字由 Go 那张体征表给，颜色和 SF Symbol 是这边自己定的。

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

import SwiftUI

/// 卡片上那条小线。
///
/// 没有坐标轴、没有刻度——这么大的地方标上数字只会挤成一团。它要回答的
/// 只有一个问题：**最近是在往上走还是往下走**。具体多少在卡片上已经写着了，
/// 要看细节点进去。
struct Sparkline: View {
    let points: [HealthPoint]
    var tint: Color = Brand.primary
    /// 累计量（步数、睡眠）画柱子。
    var bars = false

    /// 这条线只有 92pt 宽，塞不下几百个点。
    ///
    /// 步数一天一条，一年就是 365 个——柱子平均 0.25pt，糊成一团黑；
    /// 折线也一样，密到分不出起伏。**只画最近这一段**：卡片上这条线
    /// 要回答的是「最近在往上还是往下」，半年前的事跟这个问题无关。
    private var shown: [HealthPoint] { Array(points.suffix(30)) }

    var body: some View {
        if bars { barline } else { lineline }
    }

    private var barline: some View {
        Chart(shown) { p in
            // **按天分箱**。不给 unit 的话 Swift Charts 按连续时间轴算柱宽，
            // 相邻两天的柱子会互相压上去，看着就是叠在一起的一坨。
            BarMark(x: .value("时间", p.date, unit: .day), y: .value("值", p.v))
                .foregroundStyle(tint.opacity(0.75))
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .frame(height: 28)
        .allowsHitTesting(false)
    }

    private var lineline: some View {
        // 血压一条记录有两个数，两条线都画——只画收缩压的话，
        // 卡片上那条小线跟点进去看到的图对不上。
        let pts = shown
        let paired = pts.contains { $0.v2 > 0 }
        return Chart(pts) { p in
            LineMark(x: .value("时间", p.date), y: .value("值", p.v),
                     series: .value("线", "主"))
                .interpolationMethod(.monotone)
                .lineStyle(.init(lineWidth: 1.6, lineCap: .round))
            AreaMark(x: .value("时间", p.date), y: .value("值", p.v))
                .interpolationMethod(.monotone)
                .foregroundStyle(
                    LinearGradient(colors: [tint.opacity(0.22), tint.opacity(0)],
                                   startPoint: .top, endPoint: .bottom))
            if paired {
                LineMark(x: .value("时间", p.date), y: .value("值", p.v2),
                         series: .value("线", "次"))
                    .interpolationMethod(.monotone)
                    .lineStyle(.init(lineWidth: 1.2, lineCap: .round))
                    .foregroundStyle(tint.opacity(0.5))
            }
        }
        .foregroundStyle(tint)
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        // **纵轴不从 0 起。** 体重在 70 上下浮动两公斤，从 0 画起就是一条
        // 拉平的直线，什么都看不出来。这里要看的是变化，不是绝对量。
        .chartYScale(domain: .automatic(includesZero: false))
        .chartLegend(.hidden)
        .frame(height: 28)
        .allowsHitTesting(false)
    }
}

/// 点开一项指标：一张带坐标的图 + 一条条读数。
struct VitalDetail: View {
    @EnvironmentObject var store: Store
    let vital: HealthVital
    @Environment(\.dismiss) private var dismiss

    private var tint: Color { HealthGroup.of(vital.group).tint }

    /// 看多久。默认一个月——最常问的是「这个月怎么样」。
    @State private var span = Span.month

    /// 正在问「这条是不是录错了」的那一条。
    @State private var asking: HealthPoint?

    enum Span: String, CaseIterable, Identifiable {
        case week = "周", month = "月", half = "6 个月"
        var id: String { rawValue }
        var days: Int {
            switch self {
            case .week:  return 7
            case .month: return 30
            case .half:  return 180
            }
        }
    }

    private var shown: [HealthPoint] {
        guard let cut = Calendar.current.date(byAdding: .day, value: -span.days, to: Date())
        else { return vital.points }
        return vital.points.filter { $0.date >= cut }
    }

    private var askingBinding: Binding<Bool> {
        Binding(get: { asking != nil }, set: { if !$0 { asking = nil } })
    }

    private func void(_ p: HealthPoint) {
        asking = nil
        Task {
            let ok = await store.voidHealth(kind: vital.kind, at: p.date)
            ok ? Haptics.warning() : Haptics.failure()
        }
    }

    /// 血压一条记录有两个数，两条线一起画。
    private var isPaired: Bool { vital.points.contains { $0.v2 > 0 } }

    var body: some View {
        // **在这儿算一次，往下都传值。** `shown` 和 `isPaired` 都是计算属性，
        // 写在 chart / stats / readings 里各读一次，就是把整个数组各扫一遍；
        // 更糟的是把它们写进 ForEach 的闭包里——那会变成每画一个点扫一遍全部点。
        let pts = shown
        let paired = isPaired
        let dot: CGFloat = pts.count > 40 ? 8 : 26
        return NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Picker("看多久", selection: $span) {
                        ForEach(Span.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)

                    if pts.count >= 2 {
                        chart(pts, paired: paired, dot: dot)
                        stats(pts)
                    } else {
                        VStack(spacing: 6) {
                            Image(systemName: "chart.xyaxis.line")
                                .font(.title2)
                                .foregroundStyle(.tertiary)
                            Text(noLineHint)
                                .font(.footnote)
                                .foregroundStyle(.secondary)
                                .multilineTextAlignment(.center)
                        }
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 36)
                    }

                    SectionLabel(text: "每一次", icon: "list.bullet")
                    readings(pts, paired: paired)
                }
                .padding()
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle(vital.kind)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("关掉")
                }
            }
            .confirmationDialog("这条录错了？", isPresented: askingBinding,
                                titleVisibility: .visible) {
                if let p = asking {
                    Button("标成录错了", role: .destructive) { void(p) }
                }
                Button("算了", role: .cancel) {}
            } message: {
                Text("记录会留在文件里，只是往后不算数——"
                     + "医疗记录跟账本一样不改历史。")
            }
        }
    }

    // MARK: - 图

    @ViewBuilder
    private func chart(_ pts: [HealthPoint], paired: Bool, dot: CGFloat) -> some View {
        if vital.isTotal {
            barChart(pts)
        } else {
            lineChart(pts, paired: paired, dot: dot)
        }
    }

    /// 累计量（步数、睡眠）画柱子。
    ///
    /// **纵轴从 0 起**——柱子的高度就是量的多少，截断了会把「八千步」和
    /// 「一万步」画得差一倍。这跟折线那边的取舍正好相反，因为要看的东西
    /// 不一样：折线看的是起伏，柱子看的是量。
    private func barChart(_ pts: [HealthPoint]) -> some View {
        Chart(pts) { p in
            // 按天分箱：不给 unit 的话柱宽按连续时间轴算，相邻两天会叠上去。
            BarMark(x: .value("时间", p.date, unit: .day), y: .value(vital.kind, p.v))
                .foregroundStyle(tint.gradient)
        }
        .chartYAxis { AxisMarks { v in
            AxisGridLine()
            AxisValueLabel { if let d = v.as(Double.self) { Text(trimZeros(d)) } }
        } }
        .chartXAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                AxisGridLine()
                AxisValueLabel(format: .dateTime.month(.defaultDigits).day())
            }
        }
        .frame(height: 220)
    }

    private func lineChart(_ pts: [HealthPoint], paired: Bool, dot: CGFloat) -> some View {
        Chart {
            ForEach(pts) { p in
                LineMark(x: .value("时间", p.date), y: .value(vital.kind, p.v),
                         series: .value("线", "主"))
                    .interpolationMethod(.monotone)
                    .foregroundStyle(tint)
                PointMark(x: .value("时间", p.date), y: .value(vital.kind, p.v))
                    .foregroundStyle(tint)
                    .symbolSize(dot)
            }
            if paired {
                ForEach(pts) { p in
                    LineMark(x: .value("时间", p.date), y: .value("舒张压", p.v2),
                             series: .value("线", "次"))
                        .interpolationMethod(.monotone)
                        .foregroundStyle(tint.opacity(0.45))
                    PointMark(x: .value("时间", p.date), y: .value("舒张压", p.v2))
                        .foregroundStyle(tint.opacity(0.45))
                        .symbolSize(dot)
                }
            }
        }
        // 同上：不从 0 起，否则真实的起伏会被压成一条直线。
        .chartYScale(domain: .automatic(includesZero: false))
        .chartYAxis {
            AxisMarks { v in
                AxisGridLine()
                AxisValueLabel {
                    if let d = v.as(Double.self) { Text(trimZeros(d)) }
                }
            }
        }
        .chartXAxis {
            AxisMarks(values: .automatic(desiredCount: 4)) { v in
                AxisGridLine()
                AxisValueLabel(format: .dateTime.month(.defaultDigits).day())
            }
        }
        .frame(height: 220)
    }

    /// 画不出线时说清楚为什么——是这个区间里没有，还是这一项统共就量过
    /// 这么几次。人看到一张空图只会以为坏了。
    private var noLineHint: String {
        if vital.points.count >= 2 {
            return "这个区间里只有 \(shown.count) 条记录。\n换个长一点的区间看看。"
        }
        return "这一项统共只量过 \(vital.points.count) 次，两次起才画得出趋势。\n"
            + "再量一次，或者跟 Jarvis 说一句就记上了。"
    }

    // MARK: - 这段时间的概况

    /// 点密了把点收小（上面那个 dot），不然连成一条粗带子。
    private func stats(_ pts: [HealthPoint]) -> some View {
        let vs = pts.map(\.v)
        let first = vs.first ?? 0
        let last = vs.last ?? 0
        return HStack(spacing: 10) {
            stat("最低", trimZeros(vs.min() ?? 0))
            stat("最高", trimZeros(vs.max() ?? 0))
            stat("平均", trimZeros(vs.reduce(0, +) / Double(max(vs.count, 1))))
            stat("这段变化", (last - first >= 0 ? "+" : "-") + trimZeros(abs(last - first)))
        }
    }

    private func stat(_ name: String, _ value: String) -> some View {
        VStack(spacing: 3) {
            Text(name).font(.caption2).foregroundStyle(.secondary)
            HStack(alignment: .firstTextBaseline, spacing: 1) {
                Text(value)
                    .font(.system(.headline, design: .rounded).weight(.semibold))
                    .monospacedDigit()
                Text(vital.unit).font(.caption2).foregroundStyle(.secondary)
            }
            .lineLimit(1).minimumScaleFactor(0.6)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 11)
        .background(Color(.secondarySystemGroupedBackground),
                    in: .rect(cornerRadius: Brand.smallRadius))
    }

    // MARK: - 一条条读数

    private func readings(_ pts: [HealthPoint], paired: Bool) -> some View {
        VStack(spacing: 0) {
            // 新的在上面：要看的多半是最近这几次。
            let rows = Array(pts.reversed().enumerated())
            ForEach(rows, id: \.offset) { i, p in
                if i > 0 { Divider().padding(.leading, 12) }
                HStack {
                    Text(p.date, format: .dateTime.month().day().hour().minute())
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(paired ? "\(trimZeros(p.v))/\(trimZeros(p.v2)) \(vital.unit)"
                                : trimZeros(p.v) + vital.unit)
                        .font(.subheadline.monospacedDigit())
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
                .contentShape(Rectangle())
                // 体重打错一位、血压记反了，以前没有出路：那条会一直
                // 参与趋势和「最近一次」，只能到电脑上去改文件。
                .contextMenu {
                    Button(role: .destructive) { asking = p } label: {
                        Label("这条录错了", systemImage: "exclamationmark.triangle")
                    }
                }
            }
        }
        .glassEffect(.regular, in: .rect(cornerRadius: Brand.radius))
    }

}

/// 挂在回答底下的那张趋势卡片。
///
/// **它是回答的一部分，不是一个要去翻的地方。** 秘书说「体重 77.6，比上周
/// 轻 0.4」——这句话里唯一说不清的就是走势，而那恰恰是一张图一眼就给的东西。
/// 原来要看它得进健康页、找到那张卡、点开；现在它在话底下。
///
/// 图表是**附加的**：没有这项体征、或者只有一个点画不出线，就什么都不画，
/// 绝不在回答里留一句「暂无数据」——那句话没有任何用处，还让人以为出错了。
/// 点一下进 VitalDetail 看更细的（换时间跨度、逐条核对）。
struct ChartCard: View {
    @EnvironmentObject var store: Store
    let kind: String
    /// 链接上那几个字。桌面端只剩它，所以模型写的就该是人话（「体重趋势」）。
    let label: String

    @State private var detail: HealthVital?

    private var vital: HealthVital? {
        store.snapshot.health?.vitals.first { $0.kind == kind }
    }

    var body: some View {
        if let v = vital, v.hasTrendLine {
            Button {
                Haptics.tap()
                detail = v
            } label: {
                card(v)
            }
            .buttonStyle(.plain)
            .sheet(item: $detail) { VitalDetail(vital: $0) }
            // 卡片里已经有体征名、数字和趋势，读屏软件不必再把图念一遍。
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(spoken(v))
            .accessibilityHint("点开看更细的")
        }
    }

    private func card(_ v: HealthVital) -> some View {
        let tint = HealthGroup.of(v.group).tint
        return VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(v.kind)
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(tint)
                Spacer(minLength: 4)
                // 数字大、单位小：同一个字号的话一眼分不出哪个是重点。
                Text(v.number)
                    .font(.callout.weight(.semibold).monospacedDigit())
                Text(v.unit)
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                if !v.trend.isEmpty {
                    Text(v.trend)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(.secondary)
                }
            }
            Sparkline(points: v.points, tint: tint, bars: v.isTotal)
        }
        // **不给它自己的框和底。** 气泡本身就是容器了，里面再套一个圆角盒
        // 就是盒中盒——跟标题栏那个「头像的圆套在玻璃圆里」是同一个毛病。
        // 分段靠上面那点间距和折线本身的颜色，够了。
        .padding(.top, 2)
    }

    private func spoken(_ v: HealthVital) -> String {
        var s = "\(v.kind) \(v.number) \(v.unit)"
        if !v.trend.isEmpty { s += "，较上次 \(v.trend)" }
        if !v.ago.isEmpty { s += "，\(v.ago)" }
        return s
    }
}
