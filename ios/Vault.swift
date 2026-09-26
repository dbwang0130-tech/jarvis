import Foundation

/// Vault 管着 iCloud 里那个 Jarvis 目录的读写。
///
/// iOS 的沙箱不让 app 随便读 iCloud Drive：得由用户亲自选一次目录，
/// 拿到的是一个「安全作用域」的 URL，用完要还回去；想下次还能用，
/// 得存成书签。iCloud 的文件还可能只是个占位符（没下载到本地），
/// 读之前要先催它下载，而且读写都得经 NSFileCoordinator 协调，
/// 否则跟 Mac 那头同时改会互相踩。
///
/// 这些麻烦全关在这个类里——Go 那边一个文件都不碰，只收内容、吐内容。
final class Vault {
    /// 书签存在这个键下面，换设备要重新选一次目录。
    private static let bookmarkKey = "jarvis.vault.bookmark"

    private(set) var root: URL?

    init() {
#if DEBUG
        // **只给模拟器上看界面用**：给一个目录路径就直接用它，跳过文件选择器。
        //
        // 选择器没法从命令行点（simctl 点不了屏幕），而要检查的恰恰是选完之后
        // 那几屏——标题栏、气泡、可点链接、趋势卡片。真机上走的还是书签那条路。
        // Release 构建里这几行不存在。
        if let p = ProcessInfo.processInfo.environment["JARVIS_UI_VAULT"], !p.isEmpty {
            root = URL(fileURLWithPath: p, isDirectory: true)
            return
        }
#endif
        root = resolveBookmark()
    }

    var isConfigured: Bool { root != nil }

    /// 目录选好之后记下来，下次启动直接用。
    ///
    /// 书签必须在安全作用域**里面**建。选目录拿到的 URL 是带作用域的，
    /// 在作用域外 bookmarkData 建出来的书签下次解出来没有访问权——
    /// 这次用着一切正常，重启之后一个文件都读不到。
    func remember(_ url: URL) throws {
        let data = try withAccess(to: url) {
            try url.bookmarkData(options: [], includingResourceValuesForKeys: nil,
                                 relativeTo: nil)
        }
        UserDefaults.standard.set(data, forKey: Self.bookmarkKey)
        root = url
    }

    func forget() {
        UserDefaults.standard.removeObject(forKey: Self.bookmarkKey)
        root = nil
    }

    private func resolveBookmark() -> URL? {
        guard let data = UserDefaults.standard.data(forKey: Self.bookmarkKey) else { return nil }
        var stale = false
        guard let url = try? URL(resolvingBookmarkData: data, options: [],
                                 relativeTo: nil, bookmarkDataIsStale: &stale) else {
            return nil
        }
        if stale {
            // 目录被挪过或重命名了，重新存一份书签。
            try? remember(url)
        }
        return url
    }

    // MARK: - 读

    /// 把 org 目录下的 .org 全读出来，键是文件名。
    func readOrgFiles() throws -> [String: String] {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let orgDir = root.appendingPathComponent("org", isDirectory: true)
            let dir = FileManager.default.fileExists(atPath: orgDir.path) ? orgDir : root

            let entries = try FileManager.default.contentsOfDirectory(
                at: dir, includingPropertiesForKeys: nil)

            var out: [String: String] = [:]
            for entry in entries {
                guard let name = orgName(entry) else { continue }
                // 一律按真名去读，readCoordinated 会先把它下下来。
                out[name] = try readCoordinated(dir.appendingPathComponent(name))
            }
            return out
        }
    }

    /// 读根目录下某一份共用文件；没有就返回空串。
    ///
    /// **四个读法原先各抄一遍这七行。** 账本、健康、记忆、人设只差一个文件名，
    /// 而抄的那几行恰恰是 iCloud 的全部难处所在——沙箱授权（`withAccess`）、
    /// 占位符（`exists` 内部会先确保下载）、`NSFileCoordinator` 协调读。
    /// 哪天这里要多一步（比如占位符下载超时后重试一次），四份拷贝里改一份、
    /// 另外三份照旧，而没有任何地方会响。
    private func readShared(_ name: String) throws -> String {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let url = root.appendingPathComponent(name)
            guard exists(url) else { return "" }
            return try readCoordinated(url)
        }
    }

    /// 读账本；没有就返回空串（界面据此不显示财务那块）。
    func readLedger() throws -> String { try readShared("ledger.dat") }

    /// 读健康记录。FHIR NDJSON，一行一条资源，跟电脑上共用一份。
    func readHealth() throws -> String { try readShared("health.ndjson") }

    /// 读长期记忆。跟电脑上是同一份——那边记住的事这边也该记得。
    func readMemory() throws -> String { try readShared("memory.md") }

    /// 读人设。跟记忆一样两端共用——手机上说「以后别叫我先生」，
    /// 电脑上那位也得照着改。
    func readPersona() throws -> String { try readShared("persona.md") }

    /// 读 skills/ 下每一份扩展技能的原文。
    ///
    /// 只读不解析——frontmatter 怎么算由 Go 那边定（skills.Parse）。
    /// 读不出来的那份跳过：少一项扩展能力，不该让这一轮问不出话。
    func readSkills() throws -> [RawSkill] {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let dir = root.appendingPathComponent("skills", isDirectory: true)
            guard exists(dir) else { return [] }
            let subs = (try? FileManager.default.contentsOfDirectory(
                at: dir, includingPropertiesForKeys: [.isDirectoryKey])) ?? []
            var out: [RawSkill] = []
            for sub in subs {
                // 点开头的不扫，跟 Go 那边一致。
                if sub.lastPathComponent.hasPrefix(".") { continue }
                let md = sub.appendingPathComponent("SKILL.md")
                guard exists(md), let text = try? readCoordinated(md) else { continue }
                out.append(RawSkill(dir: sub.lastPathComponent, content: text))
            }
            return out
        }
    }

    // MARK: - 写

    /// 把改动写回去。只写真的变过的那几个文件——多写一次就多一次
    /// iCloud 同步，也多一次跟 Mac 那头撞车的机会。
    /// 把改动写回去。
    ///
    /// `expecting` 是我们**动手改之前**读到的那份内容。写之前先对一遍：
    /// 盘上的还是那份，才敢覆盖；已经变了说明电脑那头在这期间也改过，
    /// 这时候整文件写回去就会把人家的改动抹掉。
    ///
    /// 这不是假想的风险——真发生过一次：手机上加了一条出差行程，电脑端的
    /// jarvis 手里拿着加之前的内容，一保存就把它冲没了，而手机自己内存里
    /// 还留着，界面照常显示，看不出任何异样。
    ///
    /// 冲突的那几个文件不写，名字报回去让界面说清楚——宁可让人重做一次
    /// 改动，也不能悄悄吃掉另一头的。
    @discardableResult
    func write(orgFiles: [String: String], ledger: String?, memory: String? = nil,
               health: String? = nil, persona: String? = nil,
               expecting: [String: String] = [:]) throws -> [String] {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let orgDir = root.appendingPathComponent("org", isDirectory: true)
            let dir = FileManager.default.fileExists(atPath: orgDir.path) ? orgDir : root

            var conflicts: [String] = []
            for (name, content) in orgFiles {
                // 只取基名，别让 ../ 这种东西跑出目录。
                let base = (name as NSString).lastPathComponent
                let url = dir.appendingPathComponent(base)

                if let before = expecting[name], changedUnderneath(url, since: before) {
                    conflicts.append(base)
                    continue
                }
                try writeCoordinated(url, content)
            }
            // 账本和记忆同样要对。电脑那头记一笔账是**追加**写的，
            // 手机这边却是整文件写回——拿着旧内容一覆盖，那笔就没了。
            // 记忆两端都是整文件写，更是直接互相吃。
            for (name, content, before) in [
                ("ledger.dat", ledger, expecting["ledger.dat"]),
                ("memory.md", memory, expecting["memory.md"]),
                ("health.ndjson", health, expecting["health.ndjson"]),
                ("persona.md", persona, expecting["persona.md"]),
            ] {
                guard let content else { continue }
                let url = root.appendingPathComponent(name)
                if let before, changedUnderneath(url, since: before) {
                    conflicts.append(name)
                    continue
                }
                try writeCoordinated(url, content)
            }
            return conflicts
        }
    }

    /// 盘上的内容跟我们动手前读到的那份是不是已经不一样了。
    ///
    /// 分两种「读不出来」，结论正好相反：
    ///
    /// - **文件真的不在了**（被删了、一次都没建过）：不算冲突，照写。
    ///   拦着只会把改动卡死在手机里。
    /// - **文件在（哪怕只是个 `.icloud` 占位符）却读不出来**：算冲突。
    ///   这正是出差那条行程的处境——iCloud 还没把内容同步下来，我们手里
    ///   那份是空的，一覆盖就把整个文件抹成空。宁可让人重做一次改动。
    private func changedUnderneath(_ url: URL, since before: String) -> Bool {
        guard let now = try? readCoordinated(url) else { return exists(url) }
        return now != before
    }

    // MARK: - iCloud 的占位符

    /// 还没下载下来的 iCloud 文件，在目录里的名字是 `.真名.icloud`。
    /// 按扩展名挑 `.org` 会把它们整个漏掉——界面上就少了几个文件，
    /// 而且不报错，看起来像是那些日程本来就不存在。
    ///
    /// 返回这一项对应的真实文件名，不是 org 文件就返回 nil。
    private func orgName(_ url: URL) -> String? {
        let name = url.lastPathComponent
        guard name.hasPrefix("."), name.hasSuffix(".icloud") else {
            return url.pathExtension == "org" ? name : nil
        }
        let real = String(name.dropFirst().dropLast(".icloud".count))
        return real.hasSuffix(".org") ? real : nil
    }

    /// 文件在不在。没下载的只有占位符，直接 fileExists 会说不在。
    private func exists(_ url: URL) -> Bool {
        let fm = FileManager.default
        if fm.fileExists(atPath: url.path) { return true }
        let placeholder = url.deletingLastPathComponent()
            .appendingPathComponent(".\(url.lastPathComponent).icloud")
        return fm.fileExists(atPath: placeholder.path)
    }

    // MARK: - 小票

    /// 小票放哪儿：`Jarvis/receipts/2026-09/`。
    ///
    /// **按年月分文件夹**：一个月几十张，一年几百张，全堆在一个目录里，
    /// 在「文件」App 里根本翻不动。年月是最自然的那一刀——找小票的时候，
    /// 人记得的是「上个月那笔」。
    static let receiptDir = "receipts"

    /// 存一张小票，返回它相对 Jarvis 目录的路径。
    ///
    /// **相对路径**是有讲究的：这条路径要写进账本（`; 小票: receipts/...`），
    /// 而账本在电脑和手机上都会被读。存绝对路径的话，两边的容器路径完全
    /// 不同，等于只有存的那一端找得到。
    ///
    /// 文件名带上日期时间和商家：`2026-09-19-1430-沙县小吃.jpg`。在「文件」
    /// App 里不用挨个打开就知道是哪一张——这些图是用户自己的资产，得让他
    /// 在没有这个 app 的时候也能用。
    func saveReceipt(_ data: Data, payee: String, at when: Date) throws -> String {
        guard let root = resolveBookmark() else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let stamp = DateFormatter()
            stamp.locale = Locale(identifier: "en_US_POSIX")
            stamp.dateFormat = "yyyy-MM"
            let month = stamp.string(from: when)

            stamp.dateFormat = "yyyy-MM-dd-HHmm"
            var name = stamp.string(from: when)
            let who = payee.trimmingCharacters(in: .whitespacesAndNewlines)
            if !who.isEmpty {
                name += "-" + safeName(who)
            }

            let dir = root.appendingPathComponent(Self.receiptDir)
                .appendingPathComponent(month)
            try FileManager.default.createDirectory(at: dir,
                                                    withIntermediateDirectories: true)

            // 同一分钟拍两张也不能互相覆盖——那会悄无声息地弄丢一张小票。
            var file = dir.appendingPathComponent(name + ".jpg")
            var n = 2
            while FileManager.default.fileExists(atPath: file.path) {
                file = dir.appendingPathComponent("\(name)-\(n).jpg")
                n += 1
            }

            var coordError: NSError?
            var writeError: Error?
            NSFileCoordinator().coordinate(writingItemAt: file, options: .forReplacing,
                                           error: &coordError) { u in
                do { try data.write(to: u, options: .atomic) } catch { writeError = error }
            }
            if let coordError { throw coordError }
            if let writeError { throw writeError }

            return "\(Self.receiptDir)/\(month)/\(file.lastPathComponent)"
        }
    }

    /// 读回一张小票。账本里记的是相对路径，拿它去取图。
    func readReceipt(_ relative: String) throws -> Data {
        guard let root = resolveBookmark() else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let url = root.appendingPathComponent(relative)
            try ensureDownloaded(url)
            var coordError: NSError?
            var result: Result<Data, Error>!
            NSFileCoordinator().coordinate(readingItemAt: url, options: [], error: &coordError) { u in
                result = Result { try Data(contentsOf: u) }
            }
            if let coordError { throw coordError }
            return try result.get()
        }
    }

    /// 文件名里不能有的字符换成短横。
    ///
    /// 商家名里带 `/` 是常事（「麦当劳/王府井店」），直接拿去当文件名会被
    /// 当成路径分隔符，文件就跑到别的目录去了。
    private func safeName(_ s: String) -> String {
        let bad = CharacterSet(charactersIn: "/\\:?%*|\"<>")
        let cleaned = s.components(separatedBy: bad).joined(separator: "-")
        return String(cleaned.prefix(20))   // 太长的名字在「文件」里会被截断显示
    }

    // MARK: - 沙箱与 iCloud 的那些讲究

    /// 安全作用域要成对开关，中间才能碰这个目录。
    private func withAccess<T>(to url: URL, _ body: () throws -> T) throws -> T {
        let ok = url.startAccessingSecurityScopedResource()
        defer { if ok { url.stopAccessingSecurityScopedResource() } }
        return try body()
    }

    /// 读一个文件。iCloud 的文件可能还没下载下来，先催一下再等它。
    private func readCoordinated(_ url: URL) throws -> String {
        try ensureDownloaded(url)

        var coordError: NSError?
        var result: Result<String, Error>!
        NSFileCoordinator().coordinate(readingItemAt: url, options: [], error: &coordError) { u in
            result = Result { try String(contentsOf: u, encoding: .utf8) }
        }
        if let coordError { throw coordError }
        return try result.get()
    }

    private func writeCoordinated(_ url: URL, _ content: String) throws {
        var coordError: NSError?
        var writeError: Error?
        NSFileCoordinator().coordinate(writingItemAt: url, options: .forReplacing,
                                       error: &coordError) { u in
            do {
                try content.write(to: u, atomically: true, encoding: .utf8)
            } catch {
                writeError = error
            }
        }
        if let coordError { throw coordError }
        if let writeError { throw writeError }
    }

    /// iCloud 里的文件可能只是个占位符，得先让它下下来。
    private func ensureDownloaded(_ url: URL) throws {
        let values = try? url.resourceValues(
            forKeys: [.isUbiquitousItemKey, .ubiquitousItemDownloadingStatusKey])

        // **不是 iCloud 托管的文件就直接走**。本机文件、模拟器里的文件、
        // 状态读不出来的文件，它们的 downloadingStatus 永远不会变成 .current，
        // 原来不管三七二十一都等满十秒——三个 org 文件加账本加记忆，
        // 一次刷新白等五十秒，界面就一直空着。
        guard values?.isUbiquitousItem == true else { return }
        if values?.ubiquitousItemDownloadingStatus == .current { return }

        try? FileManager.default.startDownloadingUbiquitousItem(at: url)

        // 等它下完。手机上文件都不大，最多等几秒；
        // 等不到就按现有内容读，总比卡住界面强。
        let deadline = Date().addingTimeInterval(10)
        while Date() < deadline {
            let v = try? url.resourceValues(forKeys: [.ubiquitousItemDownloadingStatusKey])
            if v?.ubiquitousItemDownloadingStatus == .current { return }
            Thread.sleep(forTimeInterval: 0.2)
        }
    }
}

enum VaultError: LocalizedError {
    case notConfigured

    var errorDescription: String? {
        switch self {
        case .notConfigured:
            return "还没选 iCloud 里的 Jarvis 目录"
        }
    }
}
