import Foundation
import UIKit

/// 用户发过的图，存在本机。
///
/// **图不进会话存档。** 存档是每说一句就要整个读出来、改完再整个写回去的，
/// 塞几张几百 KB 的图进去，它就不再是个能随手读写的小文件了；而且那份存档
/// 还要经 Go 转一趟（`messages = out.history`），base64 在路上来回搬运纯属
/// 白费力气。存档里只留一个编号，图单独躺在 Application Support 底下。
///
/// 同样不进 iCloud：跟邮件正文一个道理，体积大、变化频繁，塞进去只会拖慢
/// 同步还容易撞车。换台设备看不到以前发的图，这是有意的取舍。
enum ImageStore {
    private static let dir: URL = {
        let url = Paths.support.appendingPathComponent("images", isDirectory: true)
        try? FileManager.default.createDirectory(at: url, withIntermediateDirectories: true)
        return url
    }()

    /// 存一张图，返回它的编号。存不下就返回 nil——回显是锦上添花，
    /// 不能因为写文件失败把这句话也发不出去。
    static func save(_ data: Data) -> String? {
        let ref = UUID().uuidString
        do {
            try data.write(to: dir.appendingPathComponent(ref), options: .atomic)
            return ref
        } catch {
            return nil
        }
    }

    /// 按编号读回来。图被清掉了就返回 nil，界面那边退回占位。
    static func load(_ ref: String) -> UIImage? {
        guard let data = try? Data(contentsOf: dir.appendingPathComponent(ref)) else {
            return nil
        }
        return UIImage(data: data)
    }

    /// 清掉不在 keep 里的那些。
    ///
    /// 会话清空、或者老消息被压缩掉之后，那些图再没人引用，留着只是占地方。
    /// 按「谁还被引用着」来删而不是按时间——正在屏幕上显示的图不能被删掉，
    /// 哪怕它是上个月发的。
    static func sweep(keep: Set<String>) {
        guard let all = try? FileManager.default.contentsOfDirectory(
            at: dir, includingPropertiesForKeys: nil) else { return }
        for url in all where !keep.contains(url.lastPathComponent) {
            try? FileManager.default.removeItem(at: url)
        }
    }
}
