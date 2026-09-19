import SwiftUI

/// 辅助工具。
///
/// 原来这四样是并排的标签页，Jarvis 占第一个。标签栏最多摆五个，
/// 再加一样就要被折进「更多」里——而功能只会越来越多。更要紧的是，
/// 标签栏把「说一句」和「翻一翻」摆成了平级的五选一，可这个 app 的
/// 主交互从来只有一个：说一句话。
///
/// 现在对话是全屏的底座，这几样浮在输入框上面一排，点开是一张盖上来的
/// 卡片，看完往下一划就回到对话。加一样功能就是这里多一行。
enum Tool: String, CaseIterable, Identifiable {
    case planner, health, mail, ledger

    var id: String { rawValue }

    var title: String {
        switch self {
        case .planner: return "日程"
        case .health:  return "健康"
        case .mail:    return "邮件"
        case .ledger:  return "财务"
        }
    }

    var icon: String {
        switch self {
        case .planner: return "calendar"
        case .health:  return "heart.text.square"
        case .mail:    return "envelope"
        case .ledger:  return "yensign.circle"
        }
    }

    /// 功能色，不跟品牌色走——这一排要靠颜色一眼认出来是哪一个。
    var tint: Color {
        switch self {
        case .planner: return .blue
        case .health:  return .pink
        case .mail:    return .orange
        case .ledger:  return .green
        }
    }
}

/// 输入框上方那一排入口。
///
/// 横着滚：工具多到一屏放不下时自己能滑，不用改布局也不用折叠成菜单。
struct ToolStrip: View {
    @EnvironmentObject var store: Store
    let pick: (Tool) -> Void

    @ScaledMetric(relativeTo: .subheadline) private var stripHeight: CGFloat = 44

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 8) {
                ForEach(Tool.allCases) { tool in
                    Button { Haptics.tap(); pick(tool) } label: {
                        HStack(spacing: 6) {
                            Image(systemName: tool.icon)
                                .font(.system(.footnote, weight: .semibold))
                            Text(tool.title).font(.subheadline)
                            if let n = badge(tool), n > 0 {
                                Text("\(n)")
                                    .font(.caption2.monospacedDigit().weight(.bold))
                                    .foregroundStyle(.white)
                                    .padding(.horizontal, 5).padding(.vertical, 1)
                                    .background(tool.tint, in: Capsule())
                            }
                        }
                        .foregroundStyle(tool.tint)
                        .padding(.horizontal, 12)
                        .padding(.vertical, 7)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: .capsule)
                    // 角标是个光秃秃的数字，念出来是「邮件 3」，
                    // 听不出这 3 是什么。
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(spoken(tool))
                }
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
        }
        .scrollIndicators(.hidden)
        // 一排胶囊而已，别让它把整条滚动区域撑成一屏高。
        // 但高度得跟着字号走——写死 44 的话，字调到无障碍档位就被切掉半行。
        .frame(height: stripHeight)
    }

    /// 念出来的说法。数字得带上单位，不然听着像编号。
    private func spoken(_ tool: Tool) -> String {
        guard let n = badge(tool), n > 0 else { return tool.title }
        switch tool {
        case .mail:    return "\(tool.title)，\(n) 封未读"
        case .planner: return "\(tool.title)，\(n) 条逾期"
        default:       return "\(tool.title)，\(n)"
        }
    }

    /// 需要留神的数目。没有就不显示——常年挂着个 0 反而让人不看它了。
    private func badge(_ tool: Tool) -> Int? {
        switch tool {
        case .mail:    return store.mailUnread
        case .planner: return store.overdueCount
        default:       return nil
        }
    }
}

/// 点开一样工具，盖一张卡片上来。
///
/// 用 sheet 而不是 push：这几样之间没有从属关系，是平级的四件东西，
/// push 进去会让人觉得自己「在日程里面」，还得找返回键。往下一划就回来，
/// 手势本身就说明了它是临时的。
struct ToolSheet: View {
    let tool: Tool
    /// Siri 说「记一笔账」时带来的金额，一路传到那张表单。
    var spendAmount: Double?
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            content
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button { dismiss() } label: { Image(systemName: "xmark") }
                            .accessibilityLabel("关掉")
                    }
                }
        }
    }

    @ViewBuilder
    private var content: some View {
        switch tool {
        case .planner: PlannerView()
        case .health:  HealthTab()
        case .mail:    MailTab()
        case .ledger:  LedgerTab(spendAmount: spendAmount)
        }
    }
}
