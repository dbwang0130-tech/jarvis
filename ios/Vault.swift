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
    func remember(_ url: URL) throws {
        let data = try url.bookmarkData(options: [], includingResourceValuesForKeys: nil,
                                        relativeTo: nil)
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

            let urls = try FileManager.default.contentsOfDirectory(
                at: dir, includingPropertiesForKeys: nil)
                .filter { $0.pathExtension == "org" }

            var out: [String: String] = [:]
            for url in urls {
                out[url.lastPathComponent] = try readCoordinated(url)
            }
            return out
        }
    }

    /// 读账本；没有就返回空串（界面据此不显示财务那块）。
    func readLedger() throws -> String {
        guard let root else { throw VaultError.notConfigured }
        return try withAccess(to: root) {
            let url = root.appendingPathComponent("ledger.dat")
            guard FileManager.default.fileExists(atPath: url.path) else { return "" }
            return try readCoordinated(url)
        }
    }

    // MARK: - 写

    /// 把改动写回去。只写真的变过的那几个文件——多写一次就多一次
    /// iCloud 同步，也多一次跟 Mac 那头撞车的机会。
    func write(orgFiles: [String: String], ledger: String?) throws {
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
        }
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
        let values = try? url.resourceValues(forKeys: [.ubiquitousItemDownloadingStatusKey])
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
