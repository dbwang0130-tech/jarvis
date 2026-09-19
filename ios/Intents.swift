import AppIntents
import SwiftUI

/// 从 Siri、快捷指令、锁屏、控制中心进来的入口。
///
/// 这个 app 叫「说一句，剩下的交给我」，可在这之前得先解锁、打开 app、
/// 点输入框、打字——那四步把「说一句」变成了「操作一遍」。
///
/// 两类意图，走两条完全不同的路：
///
/// - **问一句**（今天有什么）：不打开 app。小组件已经在往共享容器写今天的
///   快照了（见 WidgetShared），Siri 直接读它，念出来就完了。为看一眼
///   还要启动 app 是最不划算的事。
/// - **说一件事**（记一笔、安排一下）：打开 app 交给秘书。这类事要改文件，
///   而改 iCloud 里的文件得有用户授权的安全作用域书签——那是主 app
///   才握着的东西，后台跑不了。

// MARK: - 问一句

/// 「今天还剩什么」——不打开 app，直接答。
struct TodayIntent: AppIntent {
    static let title: LocalizedStringResource = "今天还剩什么"
    static let description = IntentDescription("问问今天还有几件事没做。")

    /// 不打开 app：答一句话而已，为它启动一次 app 太重了。
    static let openAppWhenRun = false

    func perform() async throws -> some IntentResult & ProvidesDialog {
        guard let snap = WidgetBox.load() else {
            return .result(dialog: "还没读到你的日程，先打开一次 Jarvis。")
        }
        return .result(dialog: IntentDialog(stringLiteral: snap.spoken))
    }
}

extension WidgetSnapshot {
    /// 念出来的说法。
    ///
    /// 跟小组件上那几行不一样：屏幕上「3 件 · 2 件截止」扫一眼就懂，
    /// 念出来得是一句人话。正在做的那件优先说——问「今天还剩什么」的人，
    /// 多半正想知道手头这件干了多久。
    var spoken: String {
        if let run = running {
            var s = "正在做：\(run.title)"
            if items.count > 1 {
                s += "。另外还有 \(items.count - 1) 件"
            }
            return s
        }
        guard !items.isEmpty else {
            return emptyHint.isEmpty ? "今天没什么事了" : emptyHint
        }
        var s = "还剩 \(items.count) 件"
        if let first = items.first {
            s += "。接下来：" + (first.time.isEmpty ? first.title
                                                  : "\(first.time) \(first.title)")
        }
        return s
    }
}

// MARK: - 说一件事

/// 「跟 Jarvis 说……」——打开 app，把这句话交给秘书。
///
/// 为什么非要打开 app：这类事要改 iCloud 里的文件，而那需要用户授权的
/// 安全作用域书签——只有主 app 握着。在后台偷偷办不成，不如老老实实
/// 打开，人也看得见它在干什么。
struct TellIntent: AppIntent {
    static let title: LocalizedStringResource = "跟 Jarvis 说"
    static let description = IntentDescription("说一件事，让它去办。")

    static let openAppWhenRun = true

    @Parameter(title: "说什么", requestValueDialog: "要我办什么？")
    var text: String

    func perform() async throws -> some IntentResult {
        // 放进信箱，app 起来后 ChatView 会拿走并发出去。
        //
        // 不在这儿直接调 Bridge：那需要 Store、Vault、还有安全作用域书签，
        // 而这会儿 app 可能还没起来。交给界面那头，它本来就要处理
        // 「从别处带进来一句话」这件事。
        IntentInbox.put(.say(text))
        return .result()
    }
}

/// 「记一笔账」——打开 app 并把记账表单摆好。
///
/// 不直接把金额写进账本：科目得挑（手打错一个字就凭空多出一个账户），
/// 而挑科目需要看到账本里已有哪些。所以这条只负责把人送到那张表单前，
/// 金额先填上。
struct SpendIntent: AppIntent {
    static let title: LocalizedStringResource = "记一笔账"
    static let description = IntentDescription("打开记账，金额先填好。")

    static let openAppWhenRun = true

    @Parameter(title: "多少钱", requestValueDialog: "花了多少？")
    var amount: Double

    func perform() async throws -> some IntentResult {
        IntentInbox.put(.spend(amount))
        return .result()
    }
}

// MARK: - 信箱

/// 从 Siri / 快捷指令带进来的那一件事。
///
/// **为什么要个信箱**：Intent 跑完的时候 app 可能才刚被叫起来，界面还
/// 没建出来，没法直接把话递给谁。写进共享容器，界面起来了自己来取——
/// 跟小组件读快照是同一个路子。
///
/// 用 App Group 而不是 UserDefaults.standard：Intent 有可能在 app 的
/// 另一个进程里跑，普通 defaults 两边不通。
enum IntentInbox {
    enum Task: Codable {
        case say(String)
        case spend(Double)
    }

    private static let key = "jarvis.intent.pending"

    static func put(_ task: Task) {
        guard let d = UserDefaults(suiteName: WidgetBox.group),
              let data = try? JSONEncoder().encode(task) else { return }
        d.set(data, forKey: key)
    }

    /// 取走并清掉。**取一次就没了**——不清的话，每次切回前台都会再办一遍。
    static func take() -> Task? {
        guard let d = UserDefaults(suiteName: WidgetBox.group),
              let data = d.data(forKey: key) else { return nil }
        d.removeObject(forKey: key)
        return try? JSONDecoder().decode(Task.self, from: data)
    }
}

// MARK: - 现成的短语

/// 装上就能用的几句话，不用自己去快捷指令里配。
///
/// 短语里**必须带 app 名**（系统的要求，也讲道理——「今天还剩什么」
/// 这种话，Siri 得知道问的是谁）。
struct JarvisShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(
            intent: TodayIntent(),
            phrases: [
                "\(.applicationName) 今天还剩什么",
                "问问 \(.applicationName) 今天有什么",
            ],
            shortTitle: "今天还剩什么",
            systemImageName: "checklist")

        AppShortcut(
            intent: TellIntent(),
            phrases: [
                "跟 \(.applicationName) 说",
                "让 \(.applicationName) 办件事",
            ],
            shortTitle: "跟 Jarvis 说",
            systemImageName: "bubble.left.and.text.bubble.right")

        AppShortcut(
            intent: SpendIntent(),
            phrases: ["\(.applicationName) 记一笔账"],
            shortTitle: "记一笔账",
            systemImageName: "yensign.circle")
    }
}
