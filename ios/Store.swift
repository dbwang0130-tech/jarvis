import Foundation
import SwiftUI

/// Store 是界面唯一的数据来源：从 iCloud 读文件、交给 Go 解析、
/// 把结果摆出来；对话产生的改动也从这儿写回去。
///
/// 文件读写和 Go 的解析都不轻，全放到后台线程；界面只读 @Published。
@MainActor
final class Store: ObservableObject {
    @Published var snapshot = Snapshot.empty
    @Published var loading = false
    @Published var lastError: String?

    /// 对话记录。界面显示用，同时原样带给 Go 做多轮上下文。
    @Published var messages: [Message] = []
    @Published var steps: [Step] = []
    @Published var thinking = false

    @Published var settings = Settings.load()

    let vault = Vault()

    var isConfigured: Bool { vault.isConfigured }

    // 手上这份文件内容。对话要用它做入参，也用来判断哪些真的变了。
    private var files: [String: String] = [:]
    private var ledger = ""

    // MARK: - 读

    func reload() async {
        guard vault.isConfigured else { return }
        loading = true
        lastError = nil
        defer { loading = false }

        do {
            let (f, l) = try await Task.detached(priority: .userInitiated) { [vault] in
                (try vault.readOrgFiles(), try vault.readLedger())
            }.value
            files = f
            ledger = l

            let snap = await Task.detached(priority: .userInitiated) { [f, l] in
                Bridge.snapshot(files: f, ledger: l)
            }.value
            snapshot = snap
            if let err = snap.error, !err.isEmpty {
                lastError = err
            }
        } catch {
            lastError = error.localizedDescription
        }
    }

    // MARK: - 对话

    func send(_ question: String) async {
        let text = question.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, !thinking else { return }
        guard settings.model.provider.isEmpty == false,
              settings.apiKey.isEmpty == false else {
            lastError = "还没配模型，去设置里填一下"
            return
        }

        messages.append(Message(role: "user", content: text))
        steps = []
        thinking = true
        defer { thinking = false }

        var model = settings.model
        model.apiKey = settings.apiKey

        let input = AskInput(files: files, ledger: ledger, question: text,
                             history: messages.filter { $0.role != "__local" },
                             model: model, profile: settings.profile)

        let out = await Task.detached(priority: .userInitiated) { Bridge.ask(input) }.value

        if let err = out.error, !err.isEmpty {
            lastError = err
            messages.append(Message(role: "assistant", content: "没办成：\(err)"))
            return
        }

        messages = out.history
        steps = out.steps ?? []

        // 秘书改了东西就写回 iCloud，然后重新解析一遍。
        if (out.files?.isEmpty == false) || (out.ledger?.isEmpty == false) {
            do {
                let changed = out.files ?? [:]
                let newLedger = out.ledger
                try await Task.detached(priority: .userInitiated) { [vault] in
                    try vault.write(orgFiles: changed, ledger: newLedger)
                }.value

                for (name, content) in changed { files[name] = content }
                if let newLedger { ledger = newLedger }
                await reparse()
            } catch {
                lastError = "改动没写回去：\(error.localizedDescription)"
            }
        }
    }

    func clearChat() {
        messages = []
        steps = []
    }

    /// 手上的内容变了，重新解析一遍给界面。
    private func reparse() async {
        let f = files, l = ledger
        snapshot = await Task.detached(priority: .userInitiated) {
            Bridge.snapshot(files: f, ledger: l)
        }.value
    }

    // MARK: - 目录

    func chooseVault(_ url: URL) async {
        do {
            try vault.remember(url)
            await reload()
        } catch {
            lastError = "目录记不住：\(error.localizedDescription)"
        }
    }
}

/// Settings 是本机的设置。密钥单独存钥匙串，不进 UserDefaults。
struct Settings: Codable {
    var model = ModelConfig()
    var profile = ""

    var apiKey: String {
        get { Keychain.read(key: "jarvis.apiKey") ?? "" }
        nonmutating set { Keychain.write(key: "jarvis.apiKey", value: newValue) }
    }

    private static let storeKey = "jarvis.settings"

    enum CodingKeys: String, CodingKey {
        case model, profile
    }

    static func load() -> Settings {
        guard let data = UserDefaults.standard.data(forKey: storeKey),
              let s = try? JSONDecoder().decode(Settings.self, from: data) else {
            return Settings()
        }
        return s
    }

    func save() {
        guard let data = try? JSONEncoder().encode(self) else { return }
        UserDefaults.standard.set(data, forKey: Self.storeKey)
    }
}
