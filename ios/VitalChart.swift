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

    var body: some View {
        Chart(points) { p in
            LineMark(x: .value("时间", p.date), y: .value("值", p.v))
                .interpolationMethod(.monotone)
                .lineStyle(.init(lineWidth: 1.6, lineCap: .round))
            AreaMark(x: .value("时间", p.date), y: .value("值", p.v))
                .interpolationMethod(.monotone)
                .foregroundStyle(
                    LinearGradient(colors: [tint.opacity(0.22), tint.opacity(0)],
                                   startPoint: .top, endPoint: .bottom))
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

    /// 看多久。默认一个月——最常问的是「这个月怎么样」。
    @State private var span = Span.month

    enum Span: String, CaseIterable, Identifiable {
        case week = "近 7 天", month = "近 30 天", half = "近半年"
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
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    Picker("看多久", selection: $span) {
                        ForEach(Span.allCases) { Text($0.rawValue).tag($0) }
                    }
                    .pickerStyle(.segmented)

                    if shown.count >= 2 {
                        chart
                        stats
                    } else {
                        Text("这段时间里只有 \(shown.count) 条记录，画不出趋势。换个长一点的区间看看。")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .center)
                            .padding(.vertical, 40)
                    }

                    SectionLabel(text: "每一次", icon: "list.bullet")
                    readings
                }
                .padding()
            }
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

    private var chart: some View {
        Chart {
            ForEach(shown) { p in
                LineMark(x: .value("时间", p.date), y: .value(vital.kind, p.v),
                         series: .value("线", "主"))
                    .interpolationMethod(.monotone)
                    .foregroundStyle(Brand.primary)
                PointMark(x: .value("时间", p.date), y: .value(vital.kind, p.v))
                    .foregroundStyle(Brand.primary)
                    .symbolSize(pointSize)
            }
            if isPaired {
                ForEach(shown) { p in
                    LineMark(x: .value("时间", p.date), y: .value("舒张压", p.v2),
                             series: .value("线", "次"))
                        .interpolationMethod(.monotone)
                        .foregroundStyle(Brand.accent)
                    PointMark(x: .value("时间", p.date), y: .value("舒张压", p.v2))
                        .foregroundStyle(Brand.accent)
                        .symbolSize(pointSize)
                }
            }
        }
        // 同上：不从 0 起，否则真实的起伏会被压成一条直线。
        .chartYScale(domain: .automatic(includesZero: false))
        .chartYAxis {
            AxisMarks { v in
                AxisGridLine()
                AxisValueLabel {
                    if let d = v.as(Double.self) { Text(trim(d)) }
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

    /// 点密了就把点收小，不然连成一条粗带子。
    private var pointSize: CGFloat { shown.count > 40 ? 8 : 26 }

    // MARK: - 这段时间的概况

    private var stats: some View {
        let vs = shown.map(\.v)
        let first = vs.first ?? 0
        let last = vs.last ?? 0
        return HStack(spacing: 10) {
            stat("最低", trim(vs.min() ?? 0))
            stat("最高", trim(vs.max() ?? 0))
            stat("平均", trim(vs.reduce(0, +) / Double(max(vs.count, 1))))
            stat("这段变化", (last - first >= 0 ? "+" : "-") + trim(abs(last - first)))
        }
    }

    private func stat(_ name: String, _ value: String) -> some View {
        VStack(spacing: 2) {
            Text(name).font(.caption2).foregroundStyle(.secondary)
            Text(value + vital.unit)
                .font(.subheadline.weight(.medium).monospacedDigit())
                .lineLimit(1).minimumScaleFactor(0.6)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 10)
        .glassEffect(.regular, in: .rect(cornerRadius: Brand.smallRadius))
    }

    // MARK: - 一条条读数

    private var readings: some View {
        VStack(spacing: 0) {
            // 新的在上面：要看的多半是最近这几次。
            let rows = Array(shown.reversed().enumerated())
            ForEach(rows, id: \.offset) { i, p in
                if i > 0 { Divider().padding(.leading, 12) }
                HStack {
                    Text(p.date, format: .dateTime.month().day().hour().minute())
                        .font(.caption.monospacedDigit())
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(isPaired ? "\(trim(p.v))/\(trim(p.v2)) \(vital.unit)"
                                  : trim(p.v) + vital.unit)
                        .font(.subheadline.monospacedDigit())
                }
                .padding(.horizontal, 12).padding(.vertical, 9)
            }
        }
        .glassEffect(.regular, in: .rect(cornerRadius: Brand.radius))
    }

    /// 72.50 写成 72.5、8000.00 写成 8000——步数后面挂两位小数不像步数。
    private func trim(_ d: Double) -> String {
        var s = String(format: "%.2f", d)
        while s.hasSuffix("0") { s.removeLast() }
        if s.hasSuffix(".") { s.removeLast() }
        return s
    }
}
