import Foundation

/// 会话存档：一个会话一个 `.jsonl` 文件，放在金库的 `sessions/` 下。
///
/// **为什么是 jsonl 而不是一个大 JSON**：跟 `health.ndjson` 同一个理由——
/// 一行一条记录，追加得上，坏了一行也只坏那一行，`tail`、`grep`、`jq` 都能
/// 直接对付。这些是用户说过的话，该跟日程账本一样是他能自己打开看的文件。
///
/// 格式：第一行是元信息，后面每行一条消息。
///
/// ```jsonl
/// {"type":"meta","started":"2026-09-26T22:33:00+08:00","title":"周会挪到周五"}
/// {"type":"chat","role":"user","text":"把周会挪到周五"}
/// {"type":"chat","role":"assistant","text":"挪好了，先生。"}
/// {"type":"engine",...}
/// ```
///
/// `chat` 是人看的对话，`engine` 是喂给模型的上下文——两样东西，分开存的
/// 理由见 `Session`。读回来时按 type 分派，认不得的 type 直接跳过，
/// **这样以后加字段不会让老版本读不出旧存档**。
/// 存档要的那几样文件操作。
///
/// **抽成协议是为了能测**：编解码是真逻辑（jsonl 拼错、字段漏映射，表现就是
/// 「历史读不出来」或者读回来少了半段），而 `Vault` 背后是安全作用域书签、
/// iCloud 占位符、写协调那一堆，测试里根本起不来。`make ios-ui` 拿纯 swiftc
/// 编这个文件加一个内存实现就能跑，不需要模拟器。
protocol SessionFiles {
    func sessionFiles() throws -> [String]
    func readSession(_ name: String) throws -> String
    func writeSession(_ name: String, _ content: String) throws
    func deleteSession(_ name: String) throws
}

enum SessionArchive {

    /// 存档列表里的一条。
    struct Entry: Identifiable, Hashable {
        /// 文件名，同时也是 id（时间戳生成，天然唯一且可排序）。
        let id: String
        let started: Date?
        /// 第一句用户说的话，截断过。列表上靠它认出是哪一段对话。
        let title: String
        let count: Int

        /// 列表上显示的日期。存档文件名本身就带时间，解不出来就不显示——
        /// 别编一个「现在」出来，那会让所有旧存档看起来都是刚才存的。
        var subtitle: String {
            guard let started else { return "\(count) 条" }
            let f = DateFormatter()
            f.locale = Locale(identifier: "zh_CN")
            f.dateFormat = Calendar.current.isDateInToday(started)
                ? "今天 HH:mm" : "M月d日 HH:mm"
            return "\(f.string(from: started)) · \(count) 条"
        }
    }

    // MARK: - 写

    /// 把一段会话归档成一个新文件，返回文件名。
    ///
    /// 文件名用开始时间：`20260926-2233.jsonl`。**排序即时序**，列表不必
    /// 逐个打开文件读元信息就能按时间排好。
    @discardableResult
    static func archive(_ session: Session, to vault: SessionFiles,
                        at when: Date = Date()) throws -> String? {
        guard !session.chat.isEmpty else { return nil }   // 空的不留

        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.dateFormat = "yyyyMMdd-HHmmss"
        let name = f.string(from: when) + ".jsonl"

        var lines: [String] = []
        let iso = ISO8601DateFormatter()
        lines.append(encode([
            "type": "meta",
            "started": iso.string(from: when),
            "title": title(of: session),
        ]))
        for m in session.chat {
            lines.append(encode(["type": "chat", "role": m.role, "text": m.text]))
        }
        for m in session.engine {
            // name / toolCallID 是 Optional：**「没有」和「空串」是两回事**，
            // 原样带着走，别在这儿替它们补一个空串（那会让 omitempty 失效，
            // 回传给模型时多出几个空字段）。
            var row: [String: Any] = ["type": "engine", "role": m.role]
            if !m.content.isEmpty { row["content"] = m.content }
            if let n = m.name, !n.isEmpty { row["name"] = n }
            if let t = m.toolCallID, !t.isEmpty { row["tool_call_id"] = t }
            // 工具调用整块塞进去。它是 Codable，单独编一次再当字符串带着走——
            // 比在这儿把结构拆开重拼可靠，那种拼法每加一个字段就漏一次。
            if let calls = m.toolCalls, !calls.isEmpty,
               let d = try? JSONEncoder().encode(calls),
               let s = String(data: d, encoding: .utf8) {
                row["tool_calls"] = s
            }
            lines.append(encode(row))
        }
        try vault.writeSession(name, lines.joined(separator: "\n") + "\n")
        return name
    }

    /// 标题取第一句用户说的话。没有就退回第一条。
    private static func title(of session: Session) -> String {
        let first = session.chat.first { $0.role == "user" } ?? session.chat.first
        let raw = (first?.text ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        let oneLine = raw.replacingOccurrences(of: "\n", with: " ")
        return oneLine.count > 40 ? String(oneLine.prefix(40)) + "…" : oneLine
    }

    private static func encode(_ obj: [String: Any]) -> String {
        guard let d = try? JSONSerialization.data(withJSONObject: obj),
              let s = String(data: d, encoding: .utf8) else { return "{}" }
        return s
    }

    // MARK: - 读

    /// 列出所有存档。
    ///
    /// **只读每个文件的头几行**就够了——列表要的是标题和条数，而 engine 那份
    /// 一轮可能几十 KB。全读进来再丢掉，翻一次列表就是几 MB 的无用功。
    static func list(from vault: SessionFiles) -> [Entry] {
        let names = (try? vault.sessionFiles()) ?? []
        return names.compactMap { name in
            guard let text = try? vault.readSession(name), !text.isEmpty else { return nil }
            var started: Date?
            var title = ""
            var count = 0
            let iso = ISO8601DateFormatter()
            for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
                guard let row = row(String(line)) else { continue }
                switch row["type"] as? String {
                case "meta":
                    title = row["title"] as? String ?? ""
                    if let s = row["started"] as? String { started = iso.date(from: s) }
                case "chat":
                    count += 1
                default:
                    break
                }
            }
            return Entry(id: name, started: started,
                         title: title.isEmpty ? "（没有标题）" : title, count: count)
        }
    }

    /// 把一份存档读回成会话。
    static func load(_ id: String, from vault: SessionFiles) -> Session? {
        guard let text = try? vault.readSession(id), !text.isEmpty else { return nil }
        var session = Session()
        for line in text.split(separator: "\n", omittingEmptySubsequences: true) {
            guard let row = row(String(line)) else { continue }
            switch row["type"] as? String {
            case "chat":
                session.chat.append(ChatMessage(role: row["role"] as? String ?? "",
                                                text: row["text"] as? String ?? ""))
            case "engine":
                var m = Message(role: row["role"] as? String ?? "",
                                content: row["content"] as? String ?? "")
                m.name = row["name"] as? String
                m.toolCallID = row["tool_call_id"] as? String
                if let s = row["tool_calls"] as? String, let d = s.data(using: .utf8) {
                    m.toolCalls = try? JSONDecoder().decode([ToolCall].self, from: d)
                }
                session.engine.append(m)
            default:
                // 认不得的 type 跳过，以后加了新东西旧版本也读得动。
                break
            }
        }
        return session.chat.isEmpty && session.engine.isEmpty ? nil : session
    }

    static func delete(_ id: String, from vault: SessionFiles) {
        try? vault.deleteSession(id)
    }

    /// 解一行。**坏行跳过不抛**——一整段对话不该因为中间某一行写坏了就全读不出来，
    /// 这正是一行一条的好处。
    private static func row(_ line: String) -> [String: Any]? {
        guard let d = line.data(using: .utf8),
              let o = try? JSONSerialization.jsonObject(with: d) as? [String: Any]
        else { return nil }
        return o
    }
}
