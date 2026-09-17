import Foundation
import SwiftUI

/// Store 是界面唯一的数据来源：从 iCloud 读文件、交给 Go 解析、
/// 把结果摆出来；对话产生的改动也从这儿写回去。
///
/// 文件读写和 Go 的解析都不轻，全放到后台线程；界面只读 @Published。
@MainActor
final class Store: ObservableObject, AskEvents {
    /// 正等着用户拿主意的那件事。发信这类收不回来的动作会走到这儿。
    @Published var pending: PendingAsk?

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

    init() {
        // 打开就接着上次那段对话。
        messages = SessionStore.load()
    }

    // 手上这份文件内容。对话要用它做入参，也用来判断哪些真的变了。
    private var files: [String: String] = [:]
    private var ledger = ""
    private var memory = ""

    /// 正在跑的那次刷新。下拉、切回前台、首次打开三条路会撞上，
    /// 撞上就跟着等前一个，而不是各读各的文件最后谁写谁赢。
    private var refreshing: Task<Void, Never>?

    /// 秘书干活期间来过刷新请求，等它收工再补一次。
    private var refreshWanted = false

    // MARK: - 读

    func reload() async {
        guard vault.isConfigured else { return }

        // 秘书正拿着改之前的内容在干活。这会儿把文件重读进来，等它回话时
        // 写回的还是基于旧内容算出来的，会把刚同步下来的改动冲掉。
        // 记一笔，等它收工再刷。
        if thinking {
            refreshWanted = true
            return
        }

        if let t = refreshing {
            await t.value
            return
        }
        let t = Task { await doReload() }
        refreshing = t
        await t.value
        refreshing = nil
    }

    private func doReload() async {
        loading = true
        lastError = nil
        defer { loading = false }

        do {
            let (f, l, m) = try await Task.detached(priority: .userInitiated) { [vault] in
                (try vault.readOrgFiles(), try vault.readLedger(), try vault.readMemory())
            }.value
            files = f
            ledger = l
            memory = m

            let snap = await Task.detached(priority: .userInitiated) { [f, l] in
                Bridge.snapshot(files: f, ledger: l)
            }.value
            snapshot = snap
            if let err = snap.error, !err.isEmpty {
                lastError = err
            }
        } catch {
            lastError = "读不到 iCloud 里的内容：\(error.localizedDescription)"
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

        // 这条只为让界面立刻有反应。传给 Go 的 history 必须是**这轮之前**的——
        // agent.Ask 自己会把提问追加进去，把它一起传过去就成了两条。
        let history = messages.filter { $0.role != "__local" }
        messages.append(Message(role: "user", content: text))
        steps = []
        thinking = true
        defer { thinking = false }

        var model = settings.model
        model.apiKey = settings.apiKey

        let input = AskInput(files: files, ledger: ledger, question: text,
                             history: history, model: model, profile: settings.profile,
                             memory: memory, accounts: settings.mailAccounts())

        let out = await Task.detached(priority: .userInitiated) { [weak self] in
            Bridge.ask(input, events: self)
        }.value

        if let err = out.error, !err.isEmpty {
            lastError = err
            messages.append(Message(role: "__local", content: "没办成：\(err)"))
            persist()
            await refreshIfMissed()
            return
        }

        messages = out.history
        steps = out.steps ?? []
        persist()

        // 秘书改了东西就写回 iCloud，然后重新解析一遍。
        if (out.files?.isEmpty == false) || (out.ledger?.isEmpty == false)
            || (out.memory?.isEmpty == false) {
            do {
                let changed = out.files ?? [:]
                let newLedger = out.ledger
                let newMemory = out.memory
                try await Task.detached(priority: .userInitiated) { [vault] in
                    try vault.write(orgFiles: changed, ledger: newLedger, memory: newMemory)
                }.value

                for (name, content) in changed { files[name] = content }
                if let newLedger { ledger = newLedger }
                if let newMemory { memory = newMemory }
                await reparse()
            } catch {
                lastError = "改动没写回去：\(error.localizedDescription)"
            }
        }

        await refreshIfMissed()
    }

    /// 干活期间来过的刷新请求，收工后补上。
    /// 得先把 thinking 放开——defer 还没轮到，不放开 reload 会以为它还在忙。
    private func refreshIfMissed() async {
        guard refreshWanted else { return }
        refreshWanted = false
        thinking = false
        await reload()
    }

    /// 干完一步就报上来，不用等整轮结束。
    nonisolated func onStep(kind: String, name: String, text: String) {
        let step = Step(kind: kind, name: name, text: text)
        Task { @MainActor [weak self] in self?.steps.append(step) }
    }

    /// 秘书要做一件收不回来的事，问用户。
    ///
    /// **这个方法在后台线程上跑，而且会一直等**——Go 那边 emit 返回后立刻
    /// 就阻塞着收答复，这里不给答案就是整轮卡死。所以无论如何都要 signal：
    /// 弹窗挂了、Store 没了，都算不批。
    nonisolated func onConfirm(tool: String, title: String, detail: String,
                               irreversible: Bool) -> Bool {
        let sem = DispatchSemaphore(value: 0)
        let box = AnswerBox()
        Task { @MainActor [weak self] in
            guard let self else { box.set(false); sem.signal(); return }
            self.pending = PendingAsk(tool: tool, title: title, detail: detail,
                                      irreversible: irreversible) { ok in
                box.set(ok)
                sem.signal()
            }
        }
        sem.wait()
        return box.get()
    }

    /// 用户点了。收起弹窗，把答复送回还在等着的那个后台线程。
    func answerPending(_ ok: Bool) {
        guard let p = pending else { return }
        pending = nil
        p.answer(ok)
    }

    func clearChat() {
        messages = []
        steps = []
        persist()
    }

    /// 存档可能有几百 KB，别占着主线程。
    private func persist() {
        let now = messages
        Task.detached(priority: .utility) { SessionStore.save(now) }
    }

    /// 手上的内容变了，重新解析一遍给界面。
    private func reparse() async {
        let f = files, l = ledger
        snapshot = await Task.detached(priority: .userInitiated) {
            Bridge.snapshot(files: f, ledger: l)
        }.value
    }

    /// 取一条条目的详情。列表里没有正文，点开才去问 Go。
    func detail(of ref: String) async -> DetailData {
        let f = files
        return await Task.detached(priority: .userInitiated) {
            Bridge.detail(files: f, ref: ref)
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
    /// 邮箱账号。密码不在这儿，单独进钥匙串。
    var mail: [MailAccount] = []

    var apiKey: String {
        get { Keychain.read(key: "jarvis.apiKey") ?? "" }
        nonmutating set { Keychain.write(key: "jarvis.apiKey", value: newValue) }
    }

    private static let storeKey = "jarvis.settings"

    enum CodingKeys: String, CodingKey {
        case model, profile, mail
    }

    /// 传给 Go 的那份：现从钥匙串取密码填上，只带配全了的账号。
    func mailAccounts() -> [MailAccountInput] {
        mail.compactMap { a in
            let pw = a.password
            guard !a.host.isEmpty, !a.user.isEmpty, !pw.isEmpty else { return nil }
            return MailAccountInput(
                name: a.name.isEmpty ? a.user : a.name,
                host: a.host, port: a.port, user: a.user, password: pw,
                smtpHost: a.smtpHost, smtpPort: a.smtpPort, from: a.from)
        }
    }

    init() {}

    // 老存档里没有 mail 这一项，缺了就当没配，别让整份设置解不开。
    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        model = try c.decodeIfPresent(ModelConfig.self, forKey: .model) ?? ModelConfig()
        profile = try c.decodeIfPresent(String.self, forKey: .profile) ?? ""
        mail = try c.decodeIfPresent([MailAccount].self, forKey: .mail) ?? []
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
