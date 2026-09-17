import Foundation

/// 对话记录存在本机，下次打开接着上次聊。
///
/// **不放 iCloud**：那个目录是 org 和账本，两端同时改已经够麻烦了，
/// 再塞一份每说一句就变的文件进去只会多撞车。对话是这台设备上的事。
enum SessionStore {
    /// 存 Application Support 而不是 Documents——后者开了文件共享会露在
    /// 「文件」app 里，对话记录没必要给人翻。
    private static var url: URL? { Paths.support.appendingPathComponent("session.json") }

    /// 存档上限。history 里带着工具调用的完整结果，一轮下来可能几十 KB，
    /// 不设限就会一路涨到读写都卡。
    private static let maxBytes = 2 * 1024 * 1024

    static func load() -> [Message] {
        guard let url, let data = try? Data(contentsOf: url) else { return [] }
        return (try? JSONDecoder().decode([Message].self, from: data)) ?? []
    }

    static func save(_ messages: [Message]) {
        guard let url else { return }
        if messages.isEmpty {
            try? FileManager.default.removeItem(at: url)
            return
        }
        guard var data = try? JSONEncoder().encode(messages) else { return }
        if data.count > maxBytes {
            let trimmed = trim(messages)
            guard let d = try? JSONEncoder().encode(trimmed) else { return }
            data = d
        }
        try? data.write(to: url, options: .atomic)
    }

    static func clear() { save([]) }

    /// 超了就从前面砍，但**只能在 user 那条上下刀**——assistant 的 tool_calls
    /// 和跟在后面的 tool 结果是成对的，从中间切开，下次发回模型就是残的。
    private static func trim(_ messages: [Message]) -> [Message] {
        var start = 0
        while start < messages.count {
            // 找下一个 user 边界
            var next = start + 1
            while next < messages.count, messages[next].role != "user" { next += 1 }
            guard next < messages.count else { break }
            start = next
            let rest = Array(messages[start...])
            if let d = try? JSONEncoder().encode(rest), d.count <= maxBytes {
                return rest
            }
        }
        // 连最后一轮都超了，那就只留最后一轮，能存多少是多少。
        return Array(messages.suffix(2))
    }
}
