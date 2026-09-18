import SwiftUI

// 邮件页不是收件箱，是一份「这些信要你做什么」的清单。
//
// 跟传统邮件 app 的区别：
//   - 不按时间排，按处置意见分组：要紧 / 想读 / 留着 / 可清掉
//   - 每行的主角是一句人话（为什么归到这一类），主题降为副行——
//     「Re: Re: FW: 关于…」这种主题本身没什么信息量
//   - 详情页反过来：摘要在上，原文折叠在下
//   - 一整组可以一次清掉，不用一封封划

/// 处置意见。取值跟 `internal/mail/session.go` 的 `mail.Verdict` 一一对应，
/// **那边加一档要同步加这里**——rawValue 匹配不上会静默落进 .unjudged，
/// 界面上显示成「还没看过」，查起来毫无线索。
///
/// 为什么不从 Go 那边把标签传过来：为了几个词让界面每渲染一行都过一次
/// JSON 桥不划算，而且 icon 和 tint 是 Go 那边没有的东西。
/// 跟 toolLabel 那张表同样的取舍。
enum Verdict: String, CaseIterable {
    case important, interest, normal, promo, spam
    case unjudged = ""

    var title: String {
        switch self {
        case .important: return "要紧"
        case .interest:  return "想读"
        case .normal:    return "留着"
        case .promo:     return "推广"
        case .spam:      return "垃圾"
        case .unjudged:  return "还没看过"
        }
    }

    var icon: String {
        switch self {
        case .important: return "exclamationmark.circle.fill"
        case .interest:  return "bookmark.fill"
        case .normal:    return "tray.fill"
        case .promo:     return "megaphone.fill"
        case .spam:      return "xmark.bin.fill"
        case .unjudged:  return "questionmark.circle"
        }
    }

    var tint: Color {
        switch self {
        case .important: return .red
        case .interest:  return .blue
        case .normal:    return .secondary
        case .promo:     return .orange
        case .spam:      return .brown
        case .unjudged:  return .secondary
        }
    }

    /// 这一类适合整组清掉。
    var cleanable: Bool { self == .promo || self == .spam }
}

struct MailTab: View {
    @EnvironmentObject var store: Store
    @State private var items: [MailItem] = []
    /// 分好组的结果。写成计算属性的话，Store 上任何一个 @Published 变化
    /// （聊天每走一步、下拉刷新、甚至别的 tab 在动）都会让这一页重算 body，
    /// 而重算一次就要把整份邮件按六个类别各过一遍。只在 items 真变了时分一次。
    @State private var groups: [(verdict: Verdict, mails: [MailItem])] = []
    @State private var syncing = false
    @State private var note = ""
    @State private var selected: MailItem?
    /// 要整组清掉的那些键。存键而不是存 Verdict——点「全部清掉」时本来就
    /// 握着那一组，再反查一次是白找。
    @State private var cleaning: [String]?

    var body: some View {
        List {
            ForEach(groups, id: \.verdict) { verdict, mails in
                Section {
                    // 同上：key 理论上唯一（账号:UID），但万一解出空串，
                    // 所有行的 id 就全一样，List 差分一样会崩。按下标稳当。
                    ForEach(Array(mails.enumerated()), id: \.offset) { _, mail in
                        Button { selected = mail } label: { MailRow(mail: mail) }
                            .buttonStyle(.plain)
                            .swipeActions(edge: .trailing) {
                                Button(role: .destructive) {
                                    clean([mail.key])
                                } label: { Label("清掉", systemImage: "trash") }
                            }
                    }
                } header: {
                    header(verdict, mails: mails)
                }
            }
        }
        .navigationTitle("邮件")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await sync() }
        .overlay { EmptyHint(show: items.isEmpty && !syncing, text: emptyText, icon: emptyIcon) }
        .overlay { LoadingHint(isEmpty: items.isEmpty, busy: syncing, text: "正在收信…") }
        .safeAreaInset(edge: .top, spacing: 0) { ErrorBar() }
        .safeAreaInset(edge: .bottom) { noteBar }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button { Task { await sync() } } label: {
                    Image(systemName: "arrow.clockwise")
                }
                .disabled(syncing)
                .accessibilityLabel("收信")
            }
        }
        .sheet(item: $selected) { MailDetail(mail: $0) }
        .confirmationDialog("清掉这一组？", isPresented: cleaningBinding,
                            titleVisibility: .visible) {
            if let keys = cleaning {
                Button("清掉 \(keys.count) 封", role: .destructive) { clean(keys) }
            }
        } message: {
            Text("移进垃圾箱，服务器上还能找回来。")
        }
        .task { reload() }
    }

    /// 拆成单独一个方法，不然编译器要为这一整个表达式推很久，
    /// 直接报「type-check 超时」。
    private func header(_ verdict: Verdict, mails: [MailItem]) -> some View {
        HStack(spacing: 6) {
            Image(systemName: verdict.icon).font(.caption2)
            Text(verdict.title)
            Text("\(mails.count)")
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.secondary)
            Spacer()
            // 一整组一次清掉。推广和垃圾本来就是成批来的，
            // 一封封划过去是在陪它们耗时间。
            if verdict.cleanable {
                Button("全部清掉") { cleaning = mails.map(\.key) }
                    .font(.caption)
                    .textCase(nil)
            }
        }
        .foregroundStyle(verdict.tint)
    }

    private var cleaningBinding: Binding<Bool> {
        Binding(get: { cleaning != nil }, set: { if !$0 { cleaning = nil } })
    }

    private var emptyText: String {
        store.settings.mail.isEmpty ? "还没配邮箱，去设置里加一个" : "没有邮件，下拉收一次"
    }

    private var emptyIcon: String {
        store.settings.mail.isEmpty ? "envelope.badge.shield.half.filled" : "envelope"
    }

    /// 按处置意见分组，要紧的在最前。清理过的 Go 那边已经滤掉了。
    /// 一趟分桶就够——按六个类别各 filter 一次是把整份邮件过六遍。
    private static func group(_ items: [MailItem]) -> [(verdict: Verdict, mails: [MailItem])] {
        let byVerdict = Dictionary(grouping: items) {
            Verdict(rawValue: $0.verdict) ?? .unjudged
        }
        let order: [Verdict] = [.important, .unjudged, .interest, .normal, .promo, .spam]
        return order.compactMap { v in
            guard let mails = byVerdict[v], !mails.isEmpty else { return nil }
            return (v, mails)
        }
    }

    @ViewBuilder
    private var noteBar: some View {
        if !note.isEmpty { HintPill(text: note) }
    }

    // MARK: - 动作

    /// 只读本地，一个网络请求都不发——打开页面要秒开。
    private func reload() {
        apply(store.mail(op: "list"))
    }

    private func sync() async {
        guard !store.settings.mail.isEmpty else { return }
        syncing = true
        defer { syncing = false }
        apply(await store.mailAsync(op: "sync"))
    }

    private func clean(_ keys: [String]) {
        guard !keys.isEmpty else { return }
        cleaning = nil
        Task { apply(await store.mailAsync(op: "clean", keys: keys), haptic: true) }
    }

    /// 三个动作的收尾是同一件事：出错就报出来，否则换上新清单。
    /// 各写一遍的话，哪天想加个「出错时保留旧清单」就要改三处。
    private func apply(_ r: MailResult, haptic: Bool = false) {
        if let err = r.error, !err.isEmpty {
            store.lastError = err
            if haptic { Haptics.failure() }
            return
        }
        items = r.items
        groups = Self.group(r.items)
        if !r.message.isEmpty { note = r.message }
        if haptic { Haptics.done() }
    }
}

/// 一行：主题是标题，发件人和 Jarvis 的判断排在下面。
struct MailRow: View {
    let mail: MailItem

    private var verdictTint: Color {
        (Verdict(rawValue: mail.verdict) ?? .unjudged).tint
    }

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Circle()
                .fill(mail.unread ? Brand.primary : Color.clear)
                .frame(width: 7, height: 7)
                .padding(.top, 6)

            VStack(alignment: .leading, spacing: 3) {
                // 主题当标题。之前拿分拣理由顶上来，结果一眼认不出是哪封信——
                // 主题再烂也是这封信的身份，理由只是「它是怎么回事」。
                Text(mail.subject.isEmpty ? "（没有主题）" : mail.subject)
                    .font(.callout)
                    .fontWeight(mail.unread ? .semibold : .regular)
                    .lineLimit(2)

                Text(mail.from).font(.caption2).foregroundStyle(.secondary).lineLimit(1)

                // 摘要另起一行，带个小标记表明这是 Jarvis 的判断，不是原文。
                if !mail.reason.isEmpty {
                    HStack(spacing: 4) {
                        Image(systemName: "sparkles").font(.system(size: 9))
                            .accessibilityHidden(true)
                        Text(mail.reason).lineLimit(2)
                    }
                    .font(.caption2)
                    .foregroundStyle(verdictTint)
                }
            }

            Spacer(minLength: 6)

            Text(mail.date.suffix(11).prefix(5))   // 只留 MM-DD
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.tertiary)
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
    }
}
