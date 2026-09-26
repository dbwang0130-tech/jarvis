import SwiftUI

// 手机上没有独立的邮件界面——收发都在跟 Jarvis 的对话里。这一页只管配账号。

struct MailAccountsView: View {
    @EnvironmentObject var store: Store
    @State private var editing: MailAccount?

    var body: some View {
        List {
            Section {
                ForEach(store.settings.mail) { a in
                    Button { editing = a } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(a.name.isEmpty ? a.user : a.name)
                                    .foregroundStyle(.primary)
                                Text(a.host.isEmpty ? "还没填服务器" : a.host)
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                            Spacer()
                            if a.password.isEmpty {
                                Image(systemName: "key.slash")
                                    .foregroundStyle(.orange)
                                    .accessibilityLabel("还没填密码")
                            }
                            Image(systemName: "chevron.right")
                                .font(.caption).foregroundStyle(.tertiary)
                        }
                    }
                }
                .onDelete(perform: remove)

                Button { editing = MailAccount() } label: {
                    Label("加一个邮箱", systemImage: "plus")
                }
            } footer: {
                Text("密码存在本机钥匙串里，不会跟着 iCloud 走。\n"
                     + "配好之后直接跟 Jarvis 说「看看有什么新邮件」。")
            }
        }
        .navigationTitle("邮箱")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $editing) { MailAccountEditor(account: $0) }
#if DEBUG
        // 跟别的 JARVIS_UI_* 同一个口子：给个地址就直接开编辑器，
        // 好在模拟器里看自动填充填出来的样子（simctl 点不了屏幕）。
        .onAppear {
            if let addr = ProcessInfo.processInfo.environment["JARVIS_UI_MAIL"],
               !addr.isEmpty, editing == nil {
                var a = MailAccount()
                a.user = addr
                editing = a
            }
        }
#endif
    }

    private func remove(_ offsets: IndexSet) {
        // 账号删了，钥匙串里那条密码也一并清掉，别留着。
        for i in offsets {
            store.settings.mail[i].password = ""
        }
        store.settings.mail.remove(atOffsets: offsets)
        store.settings.save()
    }
}

struct MailAccountEditor: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    let account: MailAccount

    @State private var name = ""
    @State private var host = ""
    @State private var port = "993"
    @State private var user = ""
    @State private var password = ""
    @State private var smtpHost = ""
    @State private var smtpPort = ""
    @State private var from = ""

    @State private var checking = false
    @State private var check: MailCheck?
    /// 按当前邮箱地址查出来的那一份预设，nil 表示地址还没打完。
    @State private var preset: MailProviderInfo?

    var body: some View {
        NavigationStack {
            Form {
                // **邮箱地址排在最前，因为剩下的多半能由它推出来。**
                // 服务器和端口是固定的公开常识（imap.qq.com:993 十年没变过），
                // 却要用户自己去搜；搜错一个字母，换来的是一句 IMAP 的英文超时。
                Section {
                    field("邮箱地址", $user, placeholder: "you@example.com", kind: .email)
                        .onChange(of: user) { _, addr in autofill(addr) }
                    SecureField("密码 / 授权码", text: $password)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                } header: {
                    Text("账号")
                } footer: {
                    // 这一句是整页唯一真正会卡住人的一步，所以认得出是哪家时
                    // 就说那家的原话，连去哪儿开都给出来。
                    if let p = preset, !p.note.isEmpty {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(p.note)
                            if let link = URL(string: p.noteURL), !p.noteURL.isEmpty {
                                Link("去开授权码", destination: link)
                                    .font(.footnote.weight(.medium))
                            }
                        }
                    } else {
                        Text("多数邮箱要用「授权码」而不是登录密码。")
                    }
                }

                Section {
                    field("服务器", $host, placeholder: "imap.example.com", kind: .url)
                    field("端口", $port, placeholder: "993", kind: .number)
                } header: {
                    Text("收件 (IMAP)")
                } footer: {
                    // 认得的说认得，猜的说猜的——两者的可信度差得远，
                    // 含糊过去的话，猜错时用户不知道该先怀疑这两项。
                    if let p = preset {
                        Text(p.known
                             ? "已识别为\(p.name)，收发服务器和端口都按它的参数填好了。"
                             : "没收录这个域名，按常见约定填的。连不上就自己改。")
                    } else {
                        Text("填完邮箱地址，这两项一般会自动填好。")
                    }
                }

                Section {
                    // 模型那边早有「测试连接」，邮箱一直没有：服务器、端口、
                    // 授权码三样里错一样都连不上，而填错了要等到点「收信」
                    // 才发现，报的还是一句 IMAP 的英文错误。
                    Button {
                        Task { await test() }
                    } label: {
                        HStack(spacing: 8) {
                            if checking {
                                ProgressView().controlSize(.small)
                            } else {
                                Image(systemName: "bolt.horizontal.circle")
                            }
                            Text(checking ? "连着看看…" : "测一下")
                            Spacer(minLength: 0)
                            if let c = check, !checking {
                                Image(systemName: c.ok ? "checkmark.circle.fill"
                                                       : "exclamationmark.circle.fill")
                                    .foregroundStyle(c.ok ? Color.green : Color.orange)
                            }
                        }
                    }
                    .disabled(checking || host.isEmpty || user.isEmpty || password.isEmpty)

                    if let c = check, !checking {
                        Text(c.message)
                            .font(.footnote)
                            .foregroundStyle(c.ok ? Color.secondary : Color.orange)
                    }
                }

                Section("发件 (SMTP)") {
                    field("服务器", $smtpHost, placeholder: "留空按收件服务器推导", kind: .url)
                    field("端口", $smtpPort, placeholder: "留空用 465", kind: .number)
                    field("显示名", $from, placeholder: "发信时对方看到的名字")
                }

                Section("这个账号叫什么") {
                    field("备注", $name, placeholder: "留空就用账号名")
                }
            }
            .navigationTitle(account.host.isEmpty ? "加邮箱" : "改邮箱")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: "xmark") }
                        .accessibilityLabel("取消")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { save() } label: { Image(systemName: "checkmark") }
                        .accessibilityLabel("存")
                        .disabled(host.isEmpty || user.isEmpty)
                }
            }
        }
        .onAppear(perform: load)
    }

    private enum Kind { case url, number, email, plain }

    @ViewBuilder
    private func field(_ label: String, _ text: Binding<String>,
                       placeholder: String = "", kind: Kind = .plain) -> some View {
        HStack {
            Text(label).frame(width: 64, alignment: .leading)
            TextField(placeholder, text: text)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .keyboardType(keyboard(kind))
                .multilineTextAlignment(.trailing)
        }
    }

    private func keyboard(_ k: Kind) -> UIKeyboardType {
        switch k {
        case .url: return .URL
        case .number: return .numberPad
        case .email: return .emailAddress
        case .plain: return .default
        }
    }

    private func load() {
        name = account.name
        host = account.host
        port = String(account.port)
        user = account.user
        password = account.password
        smtpHost = account.smtpHost
        smtpPort = account.smtpPort == 0 ? "" : String(account.smtpPort)
        from = account.from
        // 改已有账号时也查一份：那几个框早填好了，autofill 不会动它们，
        // 但「已识别为 QQ 邮箱」和那句授权码提示得显示出来。
        // 只填了地址、服务器还空着的，顺手补全——autofill 自己就只动空框。
        if !user.isEmpty {
            autofill(user)
        }
    }

    /// 邮箱地址一变就按域名查一份预设填上。
    ///
    /// **只填空着的框**：人自己改过服务器就别再覆盖回去，否则改一个字
    /// 就被弹回预设值，界面像在跟人较劲。判据是「跟上一份预设一模一样」
    /// ——那说明这几项是我们填的，不是人填的，换一家时该跟着换。
    private func autofill(_ address: String) {
        let old = preset
        let p = Bridge.mailProvider(address)
        guard !p.host.isEmpty else {
            preset = nil
            return
        }
        preset = p
        if host.trimmed.isEmpty || host.trimmed == old?.host {
            host = p.host
        }
        if port.trimmed.isEmpty || Int(port.trimmed) == old?.port {
            port = String(p.port)
        }
        if smtpHost.trimmed.isEmpty || smtpHost.trimmed == old?.smtpHost {
            smtpHost = p.smtpHost
        }
        // 端口这一项必须跟着预设走：苹果的 SMTP 在 587，留空让 Go 那边
        // 推导会落到 465，收信正常、发信一直失败，最难查。
        if smtpPort.trimmed.isEmpty || Int(smtpPort.trimmed) == old?.smtpPort {
            smtpPort = String(p.smtpPort)
        }
    }

    /// 拿眼下填的这套参数真连一次，不必先保存——填错了当场就知道，
    /// 不用存下来再去点收信。
    private func test() async {
        checking = true
        check = nil
        defer { checking = false }

        let input = MailAccountInput(
            name: name.isEmpty ? user : name,
            host: host.trimmingCharacters(in: .whitespaces),
            port: Int(port) ?? 993,
            user: user.trimmingCharacters(in: .whitespaces),
            password: password,
            smtpHost: smtpHost, smtpPort: Int(smtpPort) ?? 0, from: from)

        check = await Task.detached(priority: .userInitiated) {
            Bridge.checkMail(input)
        }.value
        check?.ok == true ? Haptics.success() : Haptics.warning()
    }

    private func save() {
        var a = account
        a.name = name.trimmed
        a.host = host.trimmed
        a.port = Int(port.trimmed) ?? 993
        a.user = user.trimmed
        a.smtpHost = smtpHost.trimmed
        a.smtpPort = Int(smtpPort.trimmed) ?? 0
        a.from = from.trimmed
        a.password = password  // 存钥匙串，不进 UserDefaults

        if let i = store.settings.mail.firstIndex(where: { $0.id == a.id }) {
            store.settings.mail[i] = a
        } else {
            store.settings.mail.append(a)
        }
        store.settings.save()
        dismiss()
    }
}

extension String {
    var trimmed: String { trimmingCharacters(in: .whitespacesAndNewlines) }
}
