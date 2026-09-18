import SwiftUI

/// 健康。
///
/// 这一页只管**看**：最近一次量的是多少、比上次差多少、这几天吃了什么。
/// 记录全靠跟 Jarvis 说一句——「体重 72.5」「昨晚睡了 7 小时」比在
/// 表单里挑单位、翻日期快得多，而且这个 app 本来就是干这个的。
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
            LazyVStack(alignment: .leading, spacing: 16) {
                if let h = health, !h.vitals.isEmpty || !h.meals.isEmpty {
                    if !h.profile.isEmpty {
                        Text(h.profile)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 4)
                    }
                    if !h.vitals.isEmpty {
                        vitals(h.vitals)
                    }
                    if !h.meals.isEmpty {
                        meals(h.meals)
                    }
                    Text("共 \(h.total) 条记录，存成 HL7 FHIR，跟电脑上是同一份")
                        .font(.caption2)
                        .foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity, alignment: .center)
                        .padding(.top, 4)
                }
            }
            .padding()
        }
        .safeAreaInset(edge: .bottom) { importBar }
        .navigationTitle("健康")
        .navigationBarTitleDisplayMode(.inline)
        .overlay { EmptyHint(show: isEmpty && !store.loading, text: emptyText, icon: "heart.text.square") }
        .overlay { LoadingHint(isEmpty: isEmpty) }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .refreshable { await store.reload() }
        .sheet(item: $picked) { VitalDetail(vital: $0) }
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
                .glassEffect(.regular.tint(Brand.soft).interactive(), in: .capsule)
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

    // MARK: - 指标

    private func vitals(_ items: [HealthVital]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            SectionLabel(text: "最近一次", icon: "waveform.path.ecg")
            // 两列。一行一个太空，三列在小屏上数字会被挤断行。
            LazyVGrid(columns: [GridItem(.flexible(), spacing: 10),
                                GridItem(.flexible(), spacing: 10)], spacing: 10) {
                ForEach(items) { v in
                    // **不禁用只有一条记录的卡片。** 灰着点不动，人只会以为
                    // 坏了——而真正的原因（这一项只量过一次）在界面上
                    // 一个字都没说。点进去让详情页把话讲明白。
                    Button { picked = v } label: { VitalCard(vital: v) }
                        .buttonStyle(.plain)
                }
            }
        }
    }

    // MARK: - 饮食

    private func meals(_ items: [HealthEntry]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            SectionLabel(text: "吃了什么", icon: "fork.knife")
            VStack(spacing: 0) {
                ForEach(Array(items.enumerated()), id: \.offset) { i, m in
                    if i > 0 { Divider().padding(.leading, 12) }
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Text(m.when)
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                        Text(m.text).font(.subheadline)
                        Spacer(minLength: 0)
                        Text(m.ago).font(.caption2).foregroundStyle(.tertiary)
                    }
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                }
            }
            .glassEffect(.regular, in: .rect(cornerRadius: Brand.radius))
        }
    }
}

/// 一项指标。
struct VitalCard: View {
    let vital: HealthVital

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(vital.kind)
                .font(.caption)
                .foregroundStyle(.secondary)
            Text(vital.value)
                .font(.title3.weight(.semibold).monospacedDigit())
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            HStack(spacing: 6) {
                if !vital.trend.isEmpty {
                    Text(vital.trend)
                        .font(.caption2.monospacedDigit().weight(.medium))
                        .foregroundStyle(trendColor)
                }
                Text(vital.ago)
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
                Spacer(minLength: 0)
                if vital.hasTrendLine {
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.tertiary)
                }
            }
            if vital.hasTrendLine {
                Sparkline(points: vital.points)
                    .padding(.top, 2)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .glassEffect(.regular, in: .rect(cornerRadius: Brand.smallRadius))
    }

    /// 只区分「涨了 / 落了 / 持平」，**不判断好坏**。
    /// 体重掉了未必是好事，血压高一点也未必要紧——那是医生的活，
    /// 涂成红绿两色等于替人下了结论。
    private var trendColor: Color {
        vital.trend == "持平" ? .secondary : Brand.primary
    }
}

/// 小标题。日程、财务、健康几处都在用同一个样子。
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
