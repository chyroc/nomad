import Foundation
import SwiftUI

/// A parsed Markdown block.
public enum MarkdownBlock: Equatable {
    case paragraph(String)
    case heading(level: Int, text: String)
    case listItem(ordered: Int?, depth: Int, text: String)
    case code(language: String, text: String)
    case quote(String)
    case rule
    case table([[String]])
}

/// A small block-level Markdown parser for agent replies.
public enum MarkdownParser {
    public static func parse(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var lines = source.replacingOccurrences(of: "\r\n", with: "\n").components(separatedBy: "\n")[...]

        func flush() {
            if !paragraph.isEmpty {
                blocks.append(.paragraph(paragraph.joined(separator: "\n")))
                paragraph.removeAll()
            }
        }

        while let line = lines.popFirst() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                flush()
                let fence = String(trimmed.prefix(3))
                let lang = String(trimmed.dropFirst(3)).trimmingCharacters(in: .whitespaces)
                var body: [String] = []
                while let next = lines.popFirst() {
                    if next.trimmingCharacters(in: .whitespaces).hasPrefix(fence) { break }
                    body.append(next)
                }
                blocks.append(.code(language: lang, text: body.joined(separator: "\n")))
                continue
            }
            if trimmed.isEmpty {
                flush()
                continue
            }
            if let h = heading(trimmed) {
                flush()
                blocks.append(h)
                continue
            }
            if trimmed == "---" || trimmed == "***" || trimmed == "___" {
                flush()
                blocks.append(.rule)
                continue
            }
            if trimmed.hasPrefix(">") {
                flush()
                var quote = [String(trimmed.dropFirst()).trimmingCharacters(in: .whitespaces)]
                while let next = lines.first, next.trimmingCharacters(in: .whitespaces).hasPrefix(">") {
                    lines.removeFirst()
                    quote.append(String(next.trimmingCharacters(in: .whitespaces).dropFirst()).trimmingCharacters(in: .whitespaces))
                }
                blocks.append(.quote(quote.joined(separator: "\n")))
                continue
            }
            if trimmed.hasPrefix("|"), let next = lines.first, isTableSeparator(next) {
                flush()
                lines.removeFirst()
                var rows = [cells(trimmed)]
                while let row = lines.first, row.trimmingCharacters(in: .whitespaces).hasPrefix("|") {
                    lines.removeFirst()
                    rows.append(cells(row.trimmingCharacters(in: .whitespaces)))
                }
                blocks.append(.table(rows))
                continue
            }
            if let item = listItem(line) {
                flush()
                blocks.append(item)
                continue
            }
            if case let .listItem(ordered, depth, text)? = blocks.last, paragraph.isEmpty,
               line.hasPrefix("  ") || line.hasPrefix("\t") {
                blocks[blocks.count - 1] = .listItem(ordered: ordered, depth: depth, text: text + "\n" + trimmed)
                continue
            }
            paragraph.append(line)
        }
        flush()
        return blocks
    }

    private static func heading(_ s: String) -> MarkdownBlock? {
        var level = 0
        for c in s { if c == "#" { level += 1 } else { break } }
        guard (1...6).contains(level), s.count > level, s[s.index(s.startIndex, offsetBy: level)] == " " else { return nil }
        return .heading(level: level, text: String(s.dropFirst(level + 1)))
    }

    private static func listItem(_ line: String) -> MarkdownBlock? {
        let indent = line.prefix { $0 == " " || $0 == "\t" }.reduce(0) { $0 + ($1 == "\t" ? 4 : 1) }
        let s = line.trimmingCharacters(in: .whitespaces)
        let depth = indent / 2
        for marker in ["- ", "* ", "+ "] where s.hasPrefix(marker) {
            var text = String(s.dropFirst(2))
            if text.hasPrefix("[ ] ") { text = "☐ " + text.dropFirst(4) }
            if text.hasPrefix("[x] ") || text.hasPrefix("[X] ") { text = "☑ " + text.dropFirst(4) }
            return .listItem(ordered: nil, depth: depth, text: text)
        }
        let digits = s.prefix { $0.isNumber }
        if !digits.isEmpty, digits.count <= 3 {
            let rest = s.dropFirst(digits.count)
            if rest.hasPrefix(". ") || rest.hasPrefix(") ") {
                return .listItem(ordered: Int(digits), depth: depth, text: String(rest.dropFirst(2)))
            }
        }
        return nil
    }

    private static func isTableSeparator(_ line: String) -> Bool {
        let s = line.trimmingCharacters(in: .whitespaces)
        guard s.hasPrefix("|") else { return false }
        return s.allSatisfy { "|-: ".contains($0) } && s.contains("-")
    }

    private static func cells(_ row: String) -> [String] {
        var s = row
        if s.hasPrefix("|") { s.removeFirst() }
        if s.hasSuffix("|") { s.removeLast() }
        return s.components(separatedBy: "|").map { $0.trimmingCharacters(in: .whitespaces) }
    }

    /// Styles inline Markdown (code, emphasis, links) as an attributed string.
    static func inline(_ text: String, size: CGFloat = 14, color: Color = Theme.textBody) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(interpretedSyntax: .inlineOnlyPreservingWhitespace)
        var out = (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
        out.font = .system(size: size)
        out.foregroundColor = color
        for run in out.runs {
            guard let intent = run.inlinePresentationIntent else {
                if run.link != nil { out[run.range].foregroundColor = Theme.accentBlue }
                continue
            }
            if intent.contains(.code) {
                out[run.range].font = .system(size: size - 1.5, design: .monospaced)
                out[run.range].backgroundColor = Theme.inlineCode
            } else if intent.contains(.stronglyEmphasized) {
                out[run.range].font = .system(size: size, weight: .semibold)
            } else if intent.contains(.emphasized) {
                out[run.range].font = .system(size: size).italic()
            }
            if run.link != nil { out[run.range].foregroundColor = Theme.accentBlue }
        }
        return out
    }
}

/// Renders Markdown with the reference client's message typography.
struct MarkdownView: View {
    var text: String
    var size: CGFloat = Theme.messageSize
    var color: Color = Theme.textBody

    var body: some View {
        let blocks = MarkdownParser.parse(text)
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { i, block in
                blockView(block)
                    .padding(.top, i == 0 ? 0 : Self.gap(blocks[i - 1], block))
            }
        }
        .textSelection(.enabled)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    static func gap(_ prev: MarkdownBlock, _ next: MarkdownBlock) -> CGFloat {
        switch (prev, next) {
        case (.listItem, .listItem): return 0
        case (.paragraph, .listItem): return 2
        case (.listItem, _): return 7
        default: return 14
        }
    }

    private var lineSpacing: CGFloat { size * 0.42 }

    @ViewBuilder
    private func blockView(_ block: MarkdownBlock) -> some View {
        switch block {
        case let .paragraph(s):
            Text(MarkdownParser.inline(s, size: size, color: color))
                .lineSpacing(lineSpacing)
                .fixedSize(horizontal: false, vertical: true)
        case let .heading(level, s):
            Text(MarkdownParser.inline(s, size: level <= 1 ? size + 5 : (level == 2 ? size + 3 : size + 1), color: Theme.textPrimary))
                .fontWeight(.semibold)
                .padding(.top, 4)
        case let .listItem(ordered, depth, s):
            HStack(alignment: .firstTextBaseline, spacing: 0) {
                Group {
                    if let n = ordered {
                        Text("\(n).").font(.system(size: size)).monospacedDigit()
                    } else {
                        Text("•").font(.system(size: size + 2, weight: .bold))
                    }
                }
                .foregroundStyle(color)
                .frame(width: 22, alignment: ordered == nil ? .center : .trailing)
                .padding(.trailing, 6)
                Text(MarkdownParser.inline(s, size: size, color: color))
                    .lineSpacing(lineSpacing)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding(.leading, CGFloat(depth) * 22)
            .padding(.vertical, lineSpacing / 2)
        case let .code(lang, s):
            CodeBlockView(language: lang, code: s)
        case let .quote(s):
            HStack(spacing: 10) {
                RoundedRectangle(cornerRadius: 1.5).fill(Theme.divider).frame(width: 3)
                Text(MarkdownParser.inline(s, size: size, color: Theme.textSecondary))
                    .lineSpacing(size * 0.5)
                    .fixedSize(horizontal: false, vertical: true)
            }
        case .rule:
            Rectangle().fill(Theme.divider).frame(height: 1)
        case let .table(rows):
            TableBlockView(rows: rows, size: size)
        }
    }
}

struct CodeBlockView: View {
    var language: String
    var code: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(language.isEmpty ? "text" : language)
                    .font(.system(size: 11.5))
                    .foregroundStyle(Theme.textTertiary)
                Spacer()
                Button {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(code, forType: .string)
                    copied = true
                    DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { copied = false }
                } label: {
                    Image(systemName: copied ? "checkmark" : "square.on.square")
                        .font(.system(size: 11))
                        .foregroundStyle(Theme.textTertiary)
                }
                .buttonStyle(.plain)
                .help(tr("Copy"))
            }
            .padding(.horizontal, 12)
            .padding(.top, 8)
            .padding(.bottom, 4)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(Theme.monoFont)
                    .foregroundStyle(Theme.textBody)
                    .lineSpacing(3)
                    .fixedSize(horizontal: true, vertical: true)
                    .padding(.horizontal, 12)
                    .padding(.bottom, 10)
            }
        }
        .background(RoundedRectangle(cornerRadius: 10).fill(Theme.codeBlock))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.divider, lineWidth: 1))
    }
}

struct TableBlockView: View {
    var rows: [[String]]
    var size: CGFloat

    var body: some View {
        let columns = rows.map(\.count).max() ?? 0
        ScrollView(.horizontal, showsIndicators: false) {
            Grid(alignment: .leading, horizontalSpacing: 0, verticalSpacing: 0) {
                ForEach(Array(rows.enumerated()), id: \.offset) { r, row in
                    GridRow {
                        ForEach(0..<columns, id: \.self) { c in
                            Text(MarkdownParser.inline(c < row.count ? row[c] : "", size: size - 1))
                                .fontWeight(r == 0 ? .semibold : .regular)
                                .padding(.horizontal, 10)
                                .padding(.vertical, 6)
                        }
                    }
                    if r < rows.count - 1 {
                        Rectangle().fill(Theme.divider).frame(height: 1).gridCellColumns(columns)
                    }
                }
            }
            .overlay(RoundedRectangle(cornerRadius: 8).stroke(Theme.divider, lineWidth: 1))
        }
    }
}
