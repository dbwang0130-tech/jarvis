import Foundation

/// 一次会话的两份记录。
///
/// **分开存，因为它们是两样东西**：`chat` 是人看的对话，`engine` 是喂给
/// 模型的上下文。后者带着工具调用的完整结果，一轮下来可能几十 KB，还会
/// 被压缩改写；前者只有一问一答，得原样留着——压缩不该让人翻不到自己
/// 说过的话。
struct Session: Codable {
    var chat: [ChatMessage] = []
    var engine: [Message] = []

    init(chat: [ChatMessage] = [], engine: [Message] = []) {
        self.chat = chat
        self.engine = engine
    }

    /// 老存档是一个光秃秃的 [Message] 数组。读得出来就当成引擎那份，
    /// 顺手投影出能显示的几条——升级不该让人丢掉上次聊到一半的对话。
    init(from decoder: Decoder) throws {
        if let c = try? decoder.container(keyedBy: CodingKeys.self),
           c.contains(.chat) || c.contains(.engine) {
            chat = try c.decodeIfPresent([ChatMessage].self, forKey: .chat) ?? []
            engine = try c.decodeIfPresent([Message].self, forKey: .engine) ?? []
            return
        }
        let old = try decoder.singleValueContainer().decode([Message].self)
        engine = old
        chat = old.compactMap { m in
            guard ["user", "assistant"].contains(m.role), !m.content.isEmpty else { return nil }
            return ChatMessage(role: m.role, text: m.content)
        }
    }
}

/// 对话记录存在本机，下次打开接着上次聊。
///
/// **不放 iCloud**：那个目录是 org 和账本，两端同时改已经够麻烦了，
/// 再塞一份每说一句就变的文件进去只会多撞车。对话是这台设备上的事。
enum SessionStore {
    /// 存 Application Support 而不是 Documents——后者开了文件共享会露在
    /// 「文件」app 里，对话记录没必要给人翻。
    private static var url: URL? { Paths.support.appendingPathComponent("session.json") }

    /// 存档上限。engine 那份带着工具调用的完整结果，一轮下来可能几十 KB，
    /// 不设限就会一路涨到读写都卡。
    private static let maxBytes = 2 * 1024 * 1024

    static func load() -> Session {
        guard let url, let data = try? Data(contentsOf: url) else { return Session() }
        return (try? JSONDecoder().decode(Session.self, from: data)) ?? Session()
    }

    static func save(_ session: Session) {
        guard let url else { return }
        if session.chat.isEmpty && session.engine.isEmpty {
            try? FileManager.default.removeItem(at: url)
            return
        }
        guard var data = try? JSONEncoder().encode(session) else { return }
        if data.count > maxBytes {
            var trimmed = session
            // 只砍引擎那份：胖的是它（工具结果全在里面），而人看的对话
            // 砍掉就是真的没了。
            trimmed.engine = trim(session.engine)
            guard let d = try? JSONEncoder().encode(trimmed) else { return }
            data = d
        }
        try? data.write(to: url, options: .atomic)
    }

    static func clear() { save(Session()) }

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
