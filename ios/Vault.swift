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

    /// 读账本；没有就返回空串（界面据此不显示财务那块）。
    func readLedger() throws -> String {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let url = root.appendingPathComponent("ledger.dat")
            guard exists(url) else { return "" }
            return try readCoordinated(url)
        }
    }

    /// 读长期记忆。跟电脑上是同一份——那边记住的事这边也该记得。
    func readMemory() throws -> String {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let url = root.appendingPathComponent("memory.md")
            guard exists(url) else { return "" }
            return try readCoordinated(url)
        }
    }

    // MARK: - 写

    /// 把改动写回去。只写真的变过的那几个文件——多写一次就多一次
    /// iCloud 同步，也多一次跟 Mac 那头撞车的机会。
    func write(orgFiles: [String: String], ledger: String?, memory: String? = nil) throws {
        guard let root else { throw VaultError.notConfigured }
        try withAccess(to: root) {
            let orgDir = root.appendingPathComponent("org", isDirectory: true)
            let dir = FileManager.default.fileExists(atPath: orgDir.path) ? orgDir : root

            for (name, content) in orgFiles {
                // 只取基名，别让 ../ 这种东西跑出目录。
                let url = dir.appendingPathComponent((name as NSString).lastPathComponent)
                try writeCoordinated(url, content)
            }
            if let ledger {
                try writeCoordinated(root.appendingPathComponent("ledger.dat"), ledger)
            }
            if let memory {
                try writeCoordinated(root.appendingPathComponent("memory.md"), memory)
            }
        }
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
