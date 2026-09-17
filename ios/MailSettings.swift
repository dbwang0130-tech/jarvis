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

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    field("服务器", $host, placeholder: "imap.example.com", kind: .url)
                    field("端口", $port, placeholder: "993", kind: .number)
                    field("账号", $user, placeholder: "you@example.com", kind: .email)
                    SecureField("密码", text: $password)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                } header: {
                    Text("收件 (IMAP)")
                } footer: {
                    Text("多数邮箱要用「授权码」而不是登录密码。")
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
