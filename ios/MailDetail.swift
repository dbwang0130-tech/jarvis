import SwiftUI

/// 邮件详情。信息层级跟传统邮件 app 反过来：
/// 上面是「这是怎么回事、要你做什么」，原文折叠在下面。
///
/// 传统 app 一打开就是一整屏原文，而原文里九成是签名、免责声明、
/// 引用的上一封——真正要看的那两句埋在中间。
struct MailDetail: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss
    let mail: MailItem

    @State private var body_: String?
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

                    if showRaw, let text = body_ {
                        Text(text)
                            .font(.footnote)
                            .textSelection(.enabled)
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
                        .accessibilityLabel("关闭")
                }
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
            // 分拣理由排在主题前面——它说的是「这封信是怎么回事」，
            // 而主题常常是「Re: Re: FW:」。
            if !mail.reason.isEmpty {
                Text(mail.reason)
                    .font(.title3.weight(.semibold))
                    .textSelection(.enabled)
            }
            Text(mail.subject)
                .font(mail.reason.isEmpty ? .title3.weight(.semibold) : .callout)
                .foregroundStyle(mail.reason.isEmpty ? Color.primary : .secondary)
                .textSelection(.enabled)
        }
        .padding(.vertical, 4)
    }

    /// 正文优先读本地。存过就不再连服务器——同步过一次就该一直看得到。
    private func load() {
        guard body_ == nil, !loading else { return }
        loading = true
        Task {
            let r = await store.mailAsync(op: "read", key: mail.key)
            loading = false
            if let err = r.error, !err.isEmpty {
                body_ = "读不出来：" + err
            } else {
                body_ = r.body ?? "（这封是空的）"
            }
        }
    }
}
