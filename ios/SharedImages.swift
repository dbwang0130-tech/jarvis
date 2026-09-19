import SwiftUI
import UIKit

/// 接住别的 app 分享过来的图片。
///
/// 分享进来的文件系统会先拷进本 app 的 `Documents/Inbox/`，把 URL 投给
/// `onOpenURL`。那份拷贝**要自己收拾**——不删的话，每分享一张就在沙箱里
/// 留一份，装久了白占地方，而它已经被搬进输入框了。
///
/// 为什么不做 Share Extension：图最终要存进 iCloud，而那需要用户授权的
/// 安全作用域书签——只有主 app 握着，扩展进程够不着。何况分享一张小票
/// 多半要跟着说一句话，那个小窗也容纳不了。
enum SharedImages {
    /// 界面还没起来时先攒着。
    ///
    /// 冷启动时 onOpenURL 会比 RootView 的 onAppear 还早——那会儿 store
    /// 还是 nil，直接扔给它就丢了。
    private static var pending: [Data] = []
    static weak var store: Store?

    static func take(_ url: URL) {
        // 分享进来的文件在自己的沙箱里，不需要安全作用域；但如果是从
        // 「文件」App 那类地方来的，就得开一下。开不了也照常试着读——
        // 多数情况下它就在 Inbox 里。
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }

        guard let raw = try? Data(contentsOf: url),
              let image = UIImage(data: raw),
              let shrunk = CameraShot.Coordinator.jpeg(image) else { return }

        // 收拾掉那份拷贝。删不掉也不要紧，不值得为它报错打扰人。
        try? FileManager.default.removeItem(at: url)

        Task { @MainActor in
            if let store {
                store.sharedShots.append(shrunk)
            } else {
                pending.append(shrunk)
            }
        }
    }

    /// 界面起来之后把攒下的交出去。
    @MainActor
    static func flush(to store: Store) {
        self.store = store
        guard !pending.isEmpty else { return }
        store.sharedShots.append(contentsOf: pending)
        pending = []
    }
}
