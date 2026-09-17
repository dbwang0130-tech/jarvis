import Foundation

/// app 能往哪儿写。
///
/// iOS 的容器根目录**不可写**，只有 Documents/、Library/、tmp/ 能写。
/// 之前 Go 那边按 ~/.config 算路径，一直在静默失败——邮件同步进度存不下来，
/// 每次收信都当成第一次全量重拉，而错误没人看见。
enum Paths {
    /// Application Support：本机数据，不进 iCloud，也不露在「文件」app 里。
    static var support: URL {
        let url = (try? FileManager.default.url(
            for: .applicationSupportDirectory, in: .userDomainMask,
            appropriateFor: nil, create: true))
            ?? URL(fileURLWithPath: NSTemporaryDirectory())
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }
}
