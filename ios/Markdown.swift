import SwiftUI

// 秘书的回复是 markdown。SwiftUI 的 Text 只认行内语法（粗体、代码、链接），
// 标题、列表、代码块、表格这些块级结构一概不管，原样显示成一堆井号和星号。
// 所以这里按行切成块，每种块各画各的。
//
// 跟桌面端 internal/chat/markdown.go 是同一套取舍：够用就行，不追求完整的
// CommonMark——秘书实际会吐的就那么几种。

// MARK: - 链接

/// 话里那个 `jarvis://` 链接。
///
/// **一个 scheme 只该有一张解析表。** 早先块切分只认 `chart`、点击处理只认
/// `item`/`mail`（`default: break`），于是 `jarvis://chart/体重` 夹在句子里就成了
/// 一个点了没反应的蓝字——而「点了没反应的蓝字比不可点更糟」正是这套链接的
/// 设计前提。加一种链接要记得改两处，漏了那一处没有任何东西会响。
///
/// 现在**行的形状只决定「画成卡片还是行内一个词」**，能不能点由这一张表回答。
struct JarvisLink {
    enum Kind: String {
        /// 一条日程，id 是 ref（`work.org:12`）。
        case item
        /// 一封邮件，id 是本地索引里的 key（`账号:UID`）。
        case mail
        /// 一项体征，id 是体征名（`体重`）。
        case chart
    }

    let kind: Kind
    let id: String

    /// 从 `jarvis://item/work.org:12` 这样的 URL 解析。
    ///
    /// id 走 `url.path`（已经解码过），所以 Go 那边对中文账号名做的
    /// percent 转义在这儿自动还原，不用自己再解一遍。
    ///
    /// **少写了 `item/` 也认。** 模型偶尔会写成 `jarvis://work.org:12`——真模型
    /// 评测里就抓到过一次（同一轮里另外三条写对了，这一条漏了 host）。不认的话
    /// 那一下是个点了没反应的蓝字，而「点了没反应比不可点更糟」正是这套链接的
    /// 前提。日程 ref 的形状很认得出来（`文件名.org:行号`），认出来就当条目用。
    init?(_ url: URL) {
        guard url.scheme == "jarvis" else { return nil }
        if let host = url.host, let kind = Kind(rawValue: host) {
            let id = String(url.path.dropFirst())   // 去掉开头那个 /
            guard !id.isEmpty else { return nil }
            self.kind = kind
            self.id = id
            return
        }
        // 兜 `jarvis://work.org:12` 这种漏了 host 的写法。
        let rest = url.absoluteString.dropFirst("jarvis://".count)
        let ref = String(rest)
        guard ref.contains(".org:") else { return nil }
        self.kind = .item
        self.id = ref
    }

    /// 从 markdown 链接里 `jarvis://` 之后那一截解析，如 `chart/体重`。
    init?(path: String) {
        guard let slash = path.firstIndex(of: "/"),
              let kind = Kind(rawValue: String(path[path.startIndex..<slash])) else { return nil }
        let id = String(path[path.index(after: slash)...])
        // 这几个字符出现了就说明这不是我们认的那种形状。
        guard !id.isEmpty, !id.contains("/"), !id.contains("(") else { return nil }
        self.kind = kind
        self.id = id
    }
}

// MARK: - 块

enum Block: Identifiable {
    case heading(Int, String)
    case paragraph(String)
    case list([Item])
    case code(String)
    case quote([String])
    case table([[String]])
    case rule
    /// 一张体征趋势图。`kind` 是体征名（「体重」），`label` 是链接上的字。
    ///
    /// 它是**块**而不是行内链接：图要占一整行，挂在那句话下面。做成可点的
    /// 行内链接的话，看趋势得先点一下——而「最近是往上还是往下」恰恰是
    /// 一眼就该看到的东西，值不上一次点击。点开是更细的（VitalDetail）。
    case chart(kind: String, label: String)

    struct Item {
        var marker: String     // 「•」或「1.」，勾选项是「☐」「☑」
        var text: String
        var indent: Int
    }

    var id: String {
        switch self {
        case .heading(let l, let t): return "h\(l):\(t)"
        case .paragraph(let t): return "p:\(t)"
        case .list(let i): return "l:\(i.map(\.text).joined(separator: "\u{1}"))"
        case .code(let c): return "c:\(c)"
        case .quote(let l): return "q:\(l.joined(separator: "\u{1}"))"
        case .table(let r): return "t:\(r.flatMap { $0 }.joined(separator: "\u{1}"))"
        case .rule: return "hr:\(UUID().uuidString)"
        case .chart(let k, let l): return "ch:\(k):\(l)"
        }
    }
}

// MARK: - 切块

enum Markdown {
    static func blocks(_ text: String) -> [Block] {
        var out: [Block] = []
        let lines = text.components(separatedBy: .newlines)
        var i = 0

        while i < lines.count {
            let line = lines[i]
            let trimmed = line.trimmingCharacters(in: .whitespaces)

            if trimmed.isEmpty { i += 1; continue }

            // 代码块：围栏之间原样保留，不许里面的星号井号被当成语法。
            if isFence(trimmed) {
                var body: [String] = []
                i += 1
                while i < lines.count, !isFence(lines[i].trimmingCharacters(in: .whitespaces)) {
                    body.append(lines[i])
                    i += 1
                }
                i += 1  // 吃掉收尾围栏；没有收尾就是读到末尾，也算完
                out.append(.code(body.joined(separator: "\n")))
                continue
            }

            if trimmed.hasPrefix("#"), let h = heading(trimmed) {
                out.append(.heading(h.0, h.1)); i += 1; continue
            }

            if trimmed == "---" || trimmed == "***" || trimmed == "___" {
                out.append(.rule); i += 1; continue
            }

            // 独占一行的图表链接：[体重趋势](jarvis://chart/体重)。
            // 别的种类（条目、邮件）不占整行——它们是句子里的一个词。
            //
            // **必须整行只有它**。夹在句子中间的话那句话就得拆成三块，
            // 而图是横着占满一行的东西，塞在半句话里排不出来。
            // 桌面端没有图，那边照常渲染成一个下划线的「体重趋势」，
            // 读起来也通——所以提示两端可以共用一份。
            if let c = soleLink(trimmed), c.link.kind == .chart {
                out.append(.chart(kind: c.link.id, label: c.label)); i += 1; continue
            }

            // 表格：本行和下一行都得是管道行，且下一行是 |---|---| 那种分隔行。
            if trimmed.hasPrefix("|"), i + 1 < lines.count,
               isTableSeparator(lines[i + 1].trimmingCharacters(in: .whitespaces)) {
                var rows = [cells(trimmed)]
                i += 2
                while i < lines.count {
                    let t = lines[i].trimmingCharacters(in: .whitespaces)
                    guard t.hasPrefix("|") else { break }
                    rows.append(cells(t)); i += 1
                }
                out.append(.table(rows))
                continue
            }

            if trimmed.hasPrefix(">") {
                var body: [String] = []
                while i < lines.count {
                    let t = lines[i].trimmingCharacters(in: .whitespaces)
                    guard t.hasPrefix(">") else { break }
                    body.append(String(t.dropFirst()).trimmingCharacters(in: .whitespaces))
                    i += 1
                }
                out.append(.quote(body))
                continue
            }

            if let first = listItem(line) {
                var items = [first]
                i += 1
                while i < lines.count, let it = listItem(lines[i]) {
                    items.append(it); i += 1
                }
                out.append(.list(items))
                continue
            }

            // 段落：连着的非空行合成一段，保留手写的换行。
            var body = [trimmed]
            i += 1
            while i < lines.count {
                let t = lines[i].trimmingCharacters(in: .whitespaces)
                if t.isEmpty || isFence(t) || t.hasPrefix("#") || t.hasPrefix(">")
                    || t.hasPrefix("|") || listItem(lines[i]) != nil { break }
                body.append(t); i += 1
            }
            out.append(.paragraph(body.joined(separator: "\n")))
        }
        return out
    }

    private static func isFence(_ s: String) -> Bool {
        s.hasPrefix("```") || s.hasPrefix("~~~")
    }

    private static func heading(_ s: String) -> (Int, String)? {
        var level = 0
        var rest = Substring(s)
        while rest.first == "#", level < 6 { level += 1; rest = rest.dropFirst() }
        guard level > 0, rest.first == " " else { return nil }
        return (level, rest.trimmingCharacters(in: .whitespaces))
    }

    /// `|---|:---:|` 这种分隔行。至少一个格子、每格只有 - : 空格。
    private static func isTableSeparator(_ s: String) -> Bool {
        guard s.hasPrefix("|") else { return false }
        let parts = cells(s)
        guard !parts.isEmpty else { return false }
        return parts.allSatisfy { c in
            !c.isEmpty && c.allSatisfy { $0 == "-" || $0 == ":" || $0 == " " } && c.contains("-")
        }
    }

    private static func cells(_ s: String) -> [String] {
        var t = Substring(s)
        if t.hasPrefix("|") { t = t.dropFirst() }
        if t.hasSuffix("|") { t = t.dropLast() }
        return t.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
    }

    /// 列表项。顺带认 org 那套勾选框——秘书聊日程时常会吐 `- [ ] 写初稿`。
    private static func listItem(_ line: String) -> Block.Item? {
        let indent = line.prefix { $0 == " " || $0 == "\t" }.count
        let t = line.trimmingCharacters(in: .whitespaces)

        for bullet in ["- ", "* ", "+ "] where t.hasPrefix(bullet) {
            var rest = String(t.dropFirst(2))
            for (box, glyph) in [("[ ] ", "☐"), ("[x] ", "☑"), ("[X] ", "☑")]
            where rest.hasPrefix(box) {
                rest = String(rest.dropFirst(box.count))
                return .init(marker: glyph, text: rest, indent: indent)
            }
            return .init(marker: "•", text: rest, indent: indent)
        }

        // 有序列表：`1. 内容`
        let digits = t.prefix { $0.isNumber }
        if !digits.isEmpty {
            let after = t.dropFirst(digits.count)
            if after.hasPrefix(". ") || after.hasPrefix(") ") {
                return .init(marker: "\(digits).",
                             text: String(after.dropFirst(2)), indent: indent)
            }
        }
        return nil
    }

    /// 行内语法交给系统：粗体、斜体、`代码`、链接。
    /// `.inlineOnlyPreservingWhitespace` 是关键——默认那档会把块级结构也吃掉，
    /// 我们已经自己切过块了，再让它插手会把空格和换行揉没。
    /// 认「整行只有一个 jarvis:// 链接」，交出链接和它的标签。
    ///
    /// 故意不用正则：这一行的形状很死（`[`…`](jarvis://`…`)`），按下标切比正则
    /// 好读，也不用担心标题里带方括号那类边界。
    ///
    /// **它只管「这一行是不是单独一个链接」，不管那是哪一种。** 是哪一种由
    /// JarvisLink 说了算——见那边的说明。
    static func soleLink(_ line: String) -> (link: JarvisLink, label: String)? {
        let mark = "](jarvis://"
        guard line.hasPrefix("["), line.hasSuffix(")"),
              let m = line.range(of: mark) else { return nil }
        let label = String(line[line.index(after: line.startIndex)..<m.lowerBound])
        let rest = String(line[m.upperBound..<line.index(before: line.endIndex)])
        guard !label.isEmpty, let link = JarvisLink(path: rest) else { return nil }
        return (link, label)
    }

    static func inline(_ s: String) -> AttributedString {
        (try? AttributedString(markdown: s, options: .init(
            allowsExtendedAttributes: true,
            interpretedSyntax: .inlineOnlyPreservingWhitespace,
            failurePolicy: .returnPartiallyParsedIfPossible)))
            ?? AttributedString(s)
    }
}

// MARK: - 画

struct MarkdownText: View {
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // 按下标。Block 的 id 是拿内容拼的，一条消息里出现两个一模一样的
            // 段落（列表里重复的行很常见）就会撞，List 差分算错行数直接崩。
            ForEach(Array(Markdown.blocks(text).enumerated()), id: \.offset) { _, block in
                view(for: block)
            }
        }
    }

    @ViewBuilder
    private func view(for block: Block) -> some View {
        switch block {
        case .heading(let level, let t):
            Text(Markdown.inline(t))
                .font(level <= 1 ? .title3.bold() : level == 2 ? .headline : .subheadline.bold())
                .padding(.top, 2)

        case .paragraph(let t):
            Text(Markdown.inline(t))

        case .chart(let kind, let label):
            ChartCard(kind: kind, label: label)

        case .list(let items):
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(items.enumerated()), id: \.offset) { _, it in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(it.marker)
                            .font(.callout.monospacedDigit())
                            .foregroundStyle(.secondary)
                        Text(Markdown.inline(it.text))
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    // 嵌套一层缩一点，再深就不缩了——手机宽度经不起。
                    .padding(.leading, CGFloat(min(it.indent, 4)) * 4)
                }
            }

        case .code(let body):
            // 代码不折行，横向滚——折了就读不出结构。
            ScrollView(.horizontal, showsIndicators: false) {
                Text(body)
                    .font(.system(.footnote, design: .monospaced))
                    .textSelection(.enabled)
                    .padding(10)
            }
            .background(Color.primary.opacity(0.06),
                        in: RoundedRectangle(cornerRadius: 8))

        case .quote(let lines):
            HStack(alignment: .top, spacing: 8) {
                Rectangle().frame(width: 3).foregroundStyle(.tertiary)
                Text(Markdown.inline(lines.joined(separator: "\n")))
                    .foregroundStyle(.secondary)
            }
            .fixedSize(horizontal: false, vertical: true)

        case .table(let rows):
            ScrollView(.horizontal, showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                        HStack(alignment: .top, spacing: 0) {
                            ForEach(Array(row.enumerated()), id: \.offset) { _, cell in
                                Text(Markdown.inline(cell))
                                    .font(.footnote)
                                    .fontWeight(i == 0 ? .semibold : .regular)
                                    .frame(minWidth: 60, alignment: .leading)
                                    .padding(.horizontal, 8).padding(.vertical, 5)
                            }
                        }
                        .background(i == 0 ? Color.primary.opacity(0.06) : .clear)
                        if i == 0 { Divider() }
                    }
                }
            }
            .background(Color.primary.opacity(0.03),
                        in: RoundedRectangle(cornerRadius: 8))

        case .rule:
            Divider()
        }
    }
}
