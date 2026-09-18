import Charts
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

    var body: some View {
        if bars { barline } else { lineline }
    }

    private var barline: some View {
        Chart(points) { p in
            BarMark(x: .value("时间", p.date), y: .value("值", p.v))
                .foregroundStyle(tint.opacity(0.7))
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
        let paired = points.contains { $0.v2 > 0 }
        return Chart(points) { p in
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
    let vital: HealthVital
    @Environment(\.dismiss) private var dismiss

    private var tint: Color { HealthGroup.of(vital.group).tint }

    /// 看多久。默认一个月——最常问的是「这个月怎么样」。
    @State private var span = Span.month

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
            BarMark(x: .value("时间", p.date), y: .value(vital.kind, p.v))
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
            }
        }
        .glassEffect(.regular, in: .rect(cornerRadius: Brand.radius))
    }

}

