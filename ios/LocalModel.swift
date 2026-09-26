import Foundation
import FoundationModels

/// 机器自己脑子里那个小模型。
///
/// **只让它做提取，不让它做决定。**
///
/// 端上这个模型比云端那个小两个数量级，多步推理、工具编排都不该指望它。
/// 但「午饭 35」→ `{金额: 35, 类别: 餐饮}` 这种把一句话拆成几个字段的活，
/// 它又快又准，而且：
///
/// - **不要钱**。记一笔账为此调一次云端大模型，是拿大炮打蚊子。
/// - **没网也能用**。地铁里刚买完东西，正是要记账的时候。
/// - **一个字不上传**。跟「数据是你自己的文件」这个立场一致——而账本和
///   日程恰恰是最私人的两样东西。
///
/// 提取完**一定要人过目**：填进表单，人看一眼再按下确认。模型把「35」听成
/// 「350」的时候，那一眼是唯一的拦截点。所以这里从不直接落盘。
enum LocalModel {
    /// 这台机器能不能用。
    ///
    /// 用不了的情况不少（设备不支持、Apple Intelligence 没开、正在下模型、
    /// 低电量模式），所以界面必须能在没有它的时候照常工作——快速填充只是
    /// 省几下打字，不是唯一的入口。
    static var available: Bool {
        if case .available = SystemLanguageModel.default.availability { return true }
        return false
    }

    /// 用不了的时候是为什么。说清楚，别只给一句「不可用」。
    static var unavailableReason: String? {
        switch SystemLanguageModel.default.availability {
        case .available:
            return nil
        case .unavailable(.deviceNotEligible):
            return "这台设备不支持设备端模型"
        case .unavailable(.appleIntelligenceNotEnabled):
            return "先到「设置 → Apple 智能与 Siri」里打开"
        case .unavailable(.modelNotReady):
            return "模型还在下载，稍后再试"
        case .unavailable:
            return "设备端模型暂时不可用"
        @unknown default:
            return "设备端模型暂时不可用"
        }
    }

    // MARK: - 记一条

    @Generable
    struct Task {
        @Guide(description: "要做的事，不带时间词。例如「跟老张过方案」")
        var title: String

        @Guide(description: "日期，格式 yyyy-MM-dd。没提到日期就留空字符串")
        var date: String

        @Guide(description: "时刻，格式 HH:mm。没提到具体几点就留空字符串")
        var time: String
    }

    /// 从一句话里拆出「做什么、什么时候」。
    ///
    /// 把今天的日期告诉它：模型自己不知道今天几号，不给的话「明天」
    /// 会被算成训练数据里的某一天。
    static func parseTask(_ said: String) async -> Task? {
        guard available else { return nil }
        let today = dayString(Date())
        let session = LanguageModelSession(instructions: """
            你把用户随口说的一件事拆成结构化字段。今天是 \(today)。
            「明天」「后天」「下周三」这类都要换算成具体日期。
            没提到的字段留空字符串，不要猜。
            """)
        return try? await session.respond(to: said, generating: Task.self).content
    }

    // MARK: - 记一笔

    @Generable
    struct Spend {
        @Guide(description: "金额，只要数字。没说金额就填 0")
        var amount: Double

        @Guide(description: "买的什么 / 这笔钱给了谁，例如「午饭」「打车」")
        var payee: String

        @Guide(description: "归到哪类开销：餐饮、交通、购物、居住、医疗、娱乐、其他")
        var category: String

        @Guide(description: "这笔是不是进账（工资、退款、红包）。花钱填 false")
        var income: Bool
    }

    /// 从一句话里拆出「多少钱、买什么、算哪类」。
    ///
    /// **不让它猜账户**：科目名是账本里那套层级字符串（`Expenses:餐饮`），
    /// 模型编一个出来就会凭空多一个账户，而且要很久才被发现。类别只给个
    /// 大方向，最后挑哪个科目由界面拿账本里已有的去匹配。
    static func parseSpend(_ said: String) async -> Spend? {
        guard available else { return nil }
        let session = LanguageModelSession(instructions: """
            你把用户随口说的一笔账拆成结构化字段。
            金额只取数字，不带货币符号。没说的字段给默认值，不要猜。
            """)
        return try? await session.respond(to: said, generating: Spend.self).content
    }

    private static func dayString(_ d: Date) -> String {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyy-MM-dd EEEE"
        return f.string(from: d)
    }
}
