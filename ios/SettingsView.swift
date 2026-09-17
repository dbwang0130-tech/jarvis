import SwiftUI
import UniformTypeIdentifiers

struct SettingsView: View {
    @EnvironmentObject var store: Store
    @Environment(\.dismiss) private var dismiss

    @State private var provider = ""
    @State private var model = ""
    @State private var baseURL = ""
    @State private var apiKey = ""
    @State private var profile = ""
    @State private var picking = false
    @State private var checking = false
    @State private var check: ModelCheck?

    /// 跟桌面端那份预设表一致。
    private let providers = ["deepseek", "qwen", "kimi", "minimax", "zhipu",
                             "doubao", "spark", "baichuan", "ollama"]

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 12) {
                        Avatar(size: 52)
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Jarvis").font(.headline)
                            Text("日程、账本、邮件都跟电脑上是同一份")
                                .font(.caption).foregroundStyle(.secondary)
                        }
                    }
                    .padding(.vertical, 4)
                    .listRowBackground(Brand.soft)
                }

                Section {
                    Picker("服务商", selection: $provider) {
                        ForEach(providers, id: \.self) { Text($0).tag($0) }
                    }
                    TextField("模型名（留空用预设）", text: $model)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    SecureField("密钥", text: $apiKey)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                    TextField("接口地址（留空用预设）", text: $baseURL)
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                        .keyboardType(.URL)
                    Button {
                        runCheck()
                    } label: {
                        HStack(spacing: 8) {
                            if checking {
                                ProgressView().controlSize(.small)
                            } else {
                                Image(systemName: "bolt.horizontal.circle")
                            }
                            Text(checking ? "连着看看…" : "测试连接")
                            Spacer()
                            if let c = check, !checking {
                                Label(c.message,
                                      systemImage: c.ok ? "checkmark.circle.fill"
                                                        : "exclamationmark.circle.fill")
                                    .font(.caption)
                                    .foregroundStyle(c.ok ? Color.green : Color.orange)
                                    .labelStyle(.tight)
                                    .lineLimit(2)
                                    .multilineTextAlignment(.trailing)
                            }
                        }
                    }
                    .disabled(checking || apiKey.isEmpty)
                } header: {
                    Text("模型")
                } footer: {
                    Text("密钥存在本机钥匙串里，不会跟着 iCloud 走。\n"
                         + "填完点一下「测试连接」，别等到第一次提问才发现填错了。")
                }

                Section("关于你") {
                    TextField("一句话介绍自己，秘书据此判断什么算要紧",
                              text: $profile, axis: .vertical)
                        .lineLimit(2...6)
                }

                Section {
                    NavigationLink {
                        MailAccountsView()
                    } label: {
                        HStack {
                            Label("邮箱", systemImage: "envelope")
                            Spacer()
                            Text(store.settings.mail.isEmpty
                                 ? "没配" : "\(store.settings.mail.count) 个")
                                .foregroundStyle(.secondary)
                        }
                    }
                } footer: {
                    Text("手机上没有单独的邮件界面，收发都在跟 Jarvis 的对话里。")
                }

                Section("数据") {
                    HStack {
                        Text("iCloud 目录")
                        Spacer()
                        Text(store.vault.root?.lastPathComponent ?? "没选")
                            .foregroundStyle(.secondary)
                    }
                    Button("换一个目录") { picking = true }
                }

                Section {
                    HStack {
                        Text("日程")
                        Spacer()
                        Text("\(store.snapshot.days.reduce(0) { $0 + $1.items.count }) 条")
                            .foregroundStyle(.secondary)
                    }
                    HStack {
                        Text("待办")
                        Spacer()
                        Text("\(store.snapshot.todos.count) 条").foregroundStyle(.secondary)
                    }
                    if let l = store.snapshot.ledger {
                        HStack {
                            Text("账户")
                            Spacer()
                            Text("\(l.accounts.count) 个").foregroundStyle(.secondary)
                        }
                    }
                } header: {
                    Text("现在读到的")
                }

                if let err = store.lastError {
                    Section {
                        Text(err).font(.footnote).foregroundStyle(.red)
                    }
                }
            }
            .navigationTitle("设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: {
                        Image(systemName: "xmark")
                    }
                    .accessibilityLabel("取消")
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button { save() } label: {
                        Image(systemName: "checkmark")
                    }
                    .accessibilityLabel("存")
                }
            }
            .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
                if case .success(let url) = result {
                    Task { await store.chooseVault(url) }
                }
            }
        }
        .onAppear(perform: load)
    }

    /// 拿界面上当前填的值去试，不是存过的那份——填到一半就能验。
    private func runCheck() {
        var m = store.settings.model
        m.provider = provider
        m.model = model.trimmed
        m.baseURL = baseURL.trimmed
        m.apiKey = apiKey.trimmed
        checking = true
        check = nil
        Task {
            let r = await Task.detached(priority: .userInitiated) {
                Bridge.checkModel(m)
            }.value
            check = r
            checking = false
            UINotificationFeedbackGenerator().notificationOccurred(r.ok ? .success : .warning)
        }
    }

    private func load() {
        let s = store.settings
        provider = s.model.provider.isEmpty ? "deepseek" : s.model.provider
        model = s.model.model
        baseURL = s.model.baseURL
        profile = s.profile
        apiKey = s.apiKey
    }

    private func save() {
        var s = store.settings
        s.model.provider = provider
        s.model.model = model.trimmingCharacters(in: .whitespaces)
        s.model.baseURL = baseURL.trimmingCharacters(in: .whitespaces)
        s.profile = profile
        s.apiKey = apiKey.trimmingCharacters(in: .whitespaces)
        s.save()
        store.settings = s
        dismiss()
    }
}
