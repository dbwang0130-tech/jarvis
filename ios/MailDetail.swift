import SwiftUI

// Verdict 原来在 MailTab.swift 里。邮件页删掉之后只剩这张卡片在用，
// 就跟着搬过来了——它带 Color 和 SF Symbol 名字，是界面的东西，
// 放不进 Models.swift（那边是两端共用的线上契约，不 import SwiftUI，
// 还要被 ios-contract 单独编一遍）。

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

/// 邮件详情。信息层级跟传统邮件 app 反过来：
/// 上面是「这是怎么回事、要你做什么」，原文折叠在下面。
///
/// 传统 app 一打开就是一整屏原文，而原文里九成是签名、免责声明、
/// 引用的上一封——真正要看的那两句埋在中间。
struct MailDetail: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss
    let mail: MailItem

    @State private var content: MailBody?
    @State private var loading = false
    @State private var showRaw = false

    private var verdict: Verdict { Verdict(rawValue: mail.verdict) ?? .unjudged }

    var body: some View {
        NavigationStack {
            List {
                Section { header }

                if !mail.snippet.isEmpty {
                    Section("说的是") {
                        Text(mail.snippet)
                            .font(.callout)
                            .textSelection(.enabled)
                    }
                }

                Section {
                    Button {
                        withAnimation(.snappy) { showRaw.toggle() }
                        if showRaw { load() }
                    } label: {
                        HStack {
                            Label("原文", systemImage: "doc.plaintext")
                            Spacer()
                            if loading { ProgressView().controlSize(.small) }
                            Image(systemName: showRaw ? "chevron.up" : "chevron.down")
                                .font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                    .buttonStyle(.plain)

                    if showRaw, let content {
                        MailBodyView(content: content)
                    }
                }

                if let atts = content?.attachments, !atts.isEmpty {
                    Section("附件") {
                        // 按下标，不按内容算 id。两个同名同大小的附件
                        // （比如两张都叫「未命名.png」的图）会撞出同一个 id，
                        // List 的差分就会算错行数，直接崩：
                        // _Bug_Detected_In_Client_Of_UICollectionView_Invalid_Number_Of_Items
                        ForEach(Array(atts.enumerated()), id: \.offset) { _, a in
                            HStack {
                                Image(systemName: "paperclip").foregroundStyle(.secondary)
                                Text(a.name).lineLimit(1)
                                Spacer()
                                Text(a.sizeText)
                                    .font(.caption.monospacedDigit())
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }

                Section {
                    LabeledContent("发件人", value: mail.addr)
                    LabeledContent("账号", value: mail.account)
                    LabeledContent("时间", value: mail.date)
                }
                .font(.caption)
                .foregroundStyle(.secondary)
            }
            .navigationTitle(mail.from)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("关掉")
                }
            }
            // 翻开就算读过，跟别的邮件 app 一个规矩。以前没有这一步：
            // 正文读出来了那封信还是未读，工具条上的角标一直挂着，
            // 人已经读过了数字却不降。
            .task {
                guard mail.unread else { return }
                await store.markRead(mail.key)
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Image(systemName: verdict.icon).foregroundStyle(verdict.tint)
                Text(verdict.title)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(verdict.tint)
            }
            Text(mail.subject.isEmpty ? "（没有主题）" : mail.subject)
                .font(.title3.weight(.semibold))
                .textSelection(.enabled)

            // Jarvis 的判断单独一行并标出来，别让人误以为是原文里的话。
            if !mail.reason.isEmpty {
                HStack(alignment: .top, spacing: 5) {
                    Image(systemName: "sparkles").font(.caption2)
                    Text(mail.reason).font(.callout)
                }
                .foregroundStyle(verdict.tint)
            }
        }
        .padding(.vertical, 4)
    }

    /// 正文优先读本地。存过就不再连服务器——同步过一次就该一直看得到。
    private func load() {
        guard content == nil, !loading else { return }
        loading = true
        Task {
            let r = await store.mailAsync(op: "read", key: mail.key)
            loading = false
            if let err = r.error, !err.isEmpty {
                content = MailBody(text: "读不出来：" + err)
            } else if let b = r.body, !b.isEmpty {
                content = b
            } else {
                content = MailBody(text: "（这封是空的）")
            }
        }
    }
}
