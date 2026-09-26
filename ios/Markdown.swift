// 把 markdown 画出来。看懂它那一半在 MarkdownParse.swift。

import SwiftUI

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
