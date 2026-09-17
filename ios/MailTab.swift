import SwiftUI

// 邮件页不是收件箱，是一份「这些信要你做什么」的清单。
//
// 跟传统邮件 app 的区别：
//   - 不按时间排，按处置意见分组：要紧 / 想读 / 留着 / 可清掉
//   - 每行的主角是一句人话（为什么归到这一类），主题降为副行——
//     「Re: Re: FW: 关于…」这种主题本身没什么信息量
//   - 详情页反过来：摘要在上，原文折叠在下
//   - 一整组可以一次清掉，不用一封封划

/// 处置意见。跟 Go 那边 mail.Verdict 的取值对齐。
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
    @State private var syncing = false
    @State private var note = ""
    @State private var selected: MailItem?
    @State private var cleaning: Verdict?

    var body: some View {
        List {
            ForEach(groups, id: \.0) { verdict, mails in
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
                    header(verdict, count: mails.count)
                }
            }
        }
        .navigationTitle("邮件")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await sync() }
        .overlay { EmptyHint(show: items.isEmpty && !syncing, text: emptyText) }
        .overlay {
            if syncing && items.isEmpty {
                VStack(spacing: 10) {
                    ProgressView()
                    Text("正在收信…").font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
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
            SettingsLink()
        }
        .sheet(item: $selected) { MailDetail(mail: $0) }
        .confirmationDialog("清掉这一组？", isPresented: cleaningBinding,
                            titleVisibility: .visible) {
            if let v = cleaning {
                let keys = groups.first { $0.0 == v }?.1.map(\.key) ?? []
                Button("清掉 \(keys.count) 封", role: .destructive) { clean(keys) }
            }
        } message: {
            Text("移进垃圾箱，服务器上还能找回来。")
        }
        .task { reload() }
    }

    /// 拆成单独一个方法，不然编译器要为这一整个表达式推很久，
    /// 直接报「type-check 超时」。
    private func header(_ verdict: Verdict, count: Int) -> some View {
        HStack(spacing: 6) {
            Image(systemName: verdict.icon).font(.caption2)
            Text(verdict.title)
            Text("\(count)")
                .font(.caption2.monospacedDigit())
                .foregroundStyle(.secondary)
            Spacer()
            // 一整组一次清掉。推广和垃圾本来就是成批来的，
            // 一封封划过去是在陪它们耗时间。
            if verdict.cleanable {
                Button("全部清掉") { cleaning = verdict }
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

    /// 按处置意见分组，要紧的在最前。清理过的 Go 那边已经滤掉了。
    private var groups: [(Verdict, [MailItem])] {
        let order: [Verdict] = [.important, .unjudged, .interest, .normal, .promo, .spam]
        return order.compactMap { v in
            let mails = items.filter { Verdict(rawValue: $0.verdict) ?? .unjudged == v }
            return mails.isEmpty ? nil : (v, mails)
        }
    }

    @ViewBuilder
    private var noteBar: some View {
        if !note.isEmpty {
            Text(note)
                .font(.caption)
                .foregroundStyle(.secondary)
                .padding(.horizontal, 14).padding(.vertical, 8)
                .glassEffect(.regular, in: .capsule)
                .padding(.bottom, 8)
        }
    }

    // MARK: - 动作

    /// 只读本地，一个网络请求都不发——打开页面要秒开。
    private func reload() {
        let r = store.mail(op: "list")
        // 错误不能丢。之前这儿把 r.error 扔了，本地库打不开时界面只显示
        // 「没有邮件」，看着像没同步过——实际是读失败了。
        if let err = r.error, !err.isEmpty {
            store.lastError = err
            return
        }
        items = r.items
    }

    private func sync() async {
        guard !store.settings.mail.isEmpty else { return }
        syncing = true
        defer { syncing = false }
        let r = await store.mailAsync(op: "sync")
        if let err = r.error, !err.isEmpty {
            store.lastError = err
        } else {
            items = r.items
            note = r.message
        }
    }

    private func clean(_ keys: [String]) {
        guard !keys.isEmpty else { return }
        cleaning = nil
        Task {
            let r = await store.mailAsync(op: "clean", keys: keys)
            if let err = r.error, !err.isEmpty {
                store.lastError = err
                UINotificationFeedbackGenerator().notificationOccurred(.error)
            } else {
                items = r.items
                note = r.message
                UIImpactFeedbackGenerator(style: .soft).impactOccurred()
            }
        }
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
