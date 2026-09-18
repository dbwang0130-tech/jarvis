import Foundation

#if canImport(Mobile)
import Mobile
#endif

/// 秘书干活过程中回头找界面的那条线。Store 实现它。
///
/// **两个方法都在后台线程上被调用**，`confirm` 还会一直阻塞到用户点了为止。
protocol AskEvents: AnyObject {
    func onStep(kind: String, name: String, label: String, text: String)
    /// tool 是 Go 查好的**中文标签**，直接显示。
    func onConfirm(tool: String, title: String, detail: String, irreversible: Bool) -> Bool
}

#if canImport(Mobile)
/// 把 Go 那头的 Host 协议接到 Swift 的 AskEvents 上。
///
/// 协议在 Swift 里叫 `MobileHostProtocol` 而不是 `MobileHost`——gomobile
/// 同时生成了同名的 class 和 protocol，Swift 碰到重名会给协议加后缀。
/// 写成 MobileHost 会被当成那个 class，报「多重继承」。
///
/// 单独隔一层是为了让界面代码不用碰 gomobile 生成的类型——macOS 上做语法
/// 检查时没有 Mobile 这个模块，碰了就编不过。
final class MobileHostAdapter: NSObject, MobileHostProtocol {
    private weak var events: AskEvents?

    init(_ events: AskEvents) {
        self.events = events
        super.init()
    }

    func step(_ kind: String?, name: String?, label: String?, text: String?) {
        events?.onStep(kind: kind ?? "", name: name ?? "",
                       label: label ?? "", text: text ?? "")
    }

    func confirm(_ tool: String?, title: String?, detail: String?,
                 irreversible: Bool) -> Bool {
        events?.onConfirm(tool: tool ?? "", title: title ?? "",
                          detail: detail ?? "", irreversible: irreversible) ?? false
    }
}
#endif

/// 一件等着用户拿主意的事。
struct PendingAsk: Identifiable {
    let id = UUID()
    var tool: String
    var title: String
    var detail: String
    /// 做了收不回来（发信这类）。可逆的动作根本不会走到这儿——
    /// 自动整理档下它们直接就做了。
    var irreversible: Bool
    /// 点完之后叫它，把答复送回还在等着的那个后台线程。
    var answer: (Bool) -> Void
}

/// 跨线程传一个答复。confirm 在后台线程上等，界面在主线程上点。
final class AnswerBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value = false

    func set(_ v: Bool) {
        lock.lock(); value = v; lock.unlock()
    }

    func get() -> Bool {
        lock.lock(); defer { lock.unlock() }
        return value
    }
}
