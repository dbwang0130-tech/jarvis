import SwiftUI

// 秘书的回复是 markdown。SwiftUI 的 Text 只认行内语法（粗体、代码、链接），
// 标题、列表、代码块、表格这些块级结构一概不管，原样显示成一堆井号和星号。
// 所以这里按行切成块，每种块各画各的。
//
// 跟桌面端 internal/chat/markdown.go 是同一套取舍：够用就行，不追求完整的
// CommonMark——秘书实际会吐的就那么几种。

// MARK: - 块

enum Block: Identifiable {
    case heading(Int, String)
    case paragraph(String)
    case list([Item])
    case code(String)
    case quote([String])
    case table([[String]])
    case rule

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
