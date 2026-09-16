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

    /// 跟桌面端那份预设表一致。
    private let providers = ["deepseek", "qwen", "kimi", "minimax", "zhipu",
                             "doubao", "spark", "baichuan", "ollama"]

    var body: some View {
        NavigationStack {
            Form {
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
                } header: {
                    Text("模型")
                } footer: {
                    Text("密钥存在本机钥匙串里，不会跟着 iCloud 走。")
                }

                Section("关于你") {
                    TextField("一句话介绍自己，秘书据此判断什么算要紧",
                              text: $profile, axis: .vertical)
                        .lineLimit(2...6)
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
                    Button("取消") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("存") { save() }
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
