import AppKit
import SwiftUI

struct ThreadView: View {
    @EnvironmentObject var model: AppModel
    var threadID: String

    private var conversation: ConversationState {
        model.conversations[threadID] ?? ConversationState()
    }

    var body: some View {
        let conv = conversation
        VStack(spacing: 0) {
            header
            ScrollViewReader { proxy in
                ScrollView(.vertical) {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(Array(conv.turns.enumerated()), id: \.element.id) { i, turn in
                            TurnView(
                                turn: turn,
                                previous: i > 0 ? conv.turns[i - 1] : nil,
                                live: conv.running && i == conv.turns.count - 1 && conv.pendingUser == nil
                            )
                        }
                        if let pending = conv.pendingUser {
                            UserBubble(text: pending)
                                .padding(.top, conv.turns.isEmpty ? 8 : 28)
                            WorkingLabel(text: threadID.hasPrefix("pending:") ? tr("Starting session…") : tr("Thinking"))
                                .padding(.top, 18)
                        }
                        Color.clear.frame(height: 24).id("bottom")
                    }
                    .frame(maxWidth: Theme.contentWidth)
                    .padding(.horizontal, 24)
                    .frame(maxWidth: .infinity)
                }
                .scrollIndicators(.never)
                .onChange(of: conv.events.count) { _, _ in
                    withAnimation(.easeOut(duration: 0.15)) { proxy.scrollTo("bottom", anchor: .bottom) }
                }
                .onChange(of: conv.pendingUser) { _, _ in proxy.scrollTo("bottom", anchor: .bottom) }
                .onAppear { proxy.scrollTo("bottom", anchor: .bottom) }
                .onChange(of: threadID) { _, _ in proxy.scrollTo("bottom", anchor: .bottom) }
            }
            VStack(spacing: 10) {
                ForEach(model.approvals.filter { $0.threadID == threadID }) { approval in
                    ApprovalCard(approval: approval)
                }
                ComposerView(placeholder: tr("Ask for follow-up changes"), running: conv.running)
            }
            .frame(maxWidth: Theme.contentWidth)
            .padding(.horizontal, 24)
            .padding(.bottom, 15)
            .frame(maxWidth: .infinity)
        }
    }

    private var isChat: Bool {
        if let t = model.thread(threadID) { return model.isChat(t) }
        let ws = conversation.workspace
        return ws == nil || ws == model.chatWorkspace
    }

    private var header: some View {
        HStack(spacing: 8) {
            Image(systemName: isChat ? "bubble.left" : "folder")
                .font(.system(size: 13))
                .foregroundStyle(Theme.icon)
            Text(model.title(for: threadID))
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Theme.textPrimary)
                .lineLimit(1)
            Menu {
                Button(tr("Copy")) {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(threadID, forType: .string)
                }
                if let ws = conversation.workspace ?? model.thread(threadID)?.workspace {
                    Button(tr("Reveal in Finder")) {
                        NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: ws)])
                    }
                }
            } label: {
                Image(systemName: "ellipsis")
                    .font(.system(size: 13))
                    .foregroundStyle(Theme.icon)
                    .frame(width: 22, height: 22)
            }
            .plainMenu()
            Spacer()
        }
        .padding(.leading, model.sidebarVisible ? 22 : 200)
        .padding(.trailing, 16)
        .frame(height: Theme.titlebarHeight)
    }
}

struct TurnView: View {
    var turn: Turn
    var previous: Turn?
    var live: Bool
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let label = dateLabel {
                Text(label)
                    .font(.system(size: 12.5))
                    .foregroundStyle(Theme.textSection)
                    .frame(maxWidth: .infinity)
                    .padding(.top, previous == nil ? 12 : 40)
            }
            if let text = turn.userText {
                UserBubble(text: text)
                    .padding(.top, dateLabel == nil ? (previous == nil ? 8 : 32) : 18)
            }
            work
                .padding(.top, 18)
            if let err = turn.error {
                ErrorLine(text: err)
                    .padding(.top, 12)
            } else if turn.interrupted {
                Text(tr("Interrupted"))
                    .font(Theme.metaFont)
                    .foregroundStyle(Theme.textTertiary)
                    .padding(.top, 12)
            }
        }
    }

    private var dateLabel: String? {
        guard let start = turn.startedAt else { return nil }
        if let prev = previous?.endedAt ?? previous?.startedAt, start.timeIntervalSince(prev) < 15 * 60,
           Calendar.current.isDate(prev, inSameDayAs: start) {
            return nil
        }
        return DateLabel.format(start)
    }

    @ViewBuilder
    private var work: some View {
        let collapsed = !live && turn.finalMessage != nil
        VStack(alignment: .leading, spacing: 0) {
            if collapsed {
                if !turn.workItems.isEmpty || turn.duration != nil {
                    WorkedForHeader(duration: turn.duration, expanded: $expanded, hasItems: !turn.workItems.isEmpty)
                    if expanded {
                        WorkItemsView(items: turn.workItems, live: false)
                            .padding(.top, 14)
                            .padding(.bottom, 4)
                    }
                    Rectangle().fill(Theme.divider).frame(height: 1).padding(.top, 8)
                }
                if let final = turn.finalMessage {
                    MarkdownView(text: final)
                        .padding(.top, turn.workItems.isEmpty && turn.duration == nil ? 0 : 19)
                    MessageActions(text: final)
                        .padding(.top, 12)
                }
            } else {
                WorkItemsView(items: turn.items, live: live)
                if live {
                    WorkingLabel(text: liveLabel)
                        .padding(.top, turn.items.isEmpty ? 0 : 14)
                }
            }
        }
    }

    private var liveLabel: String {
        if case .thinking? = turn.items.last { return tr("Thinking") }
        return tr("Working")
    }
}

enum DateLabel {
    static func format(_ date: Date) -> String {
        let f = DateFormatter()
        if L10n.isChinese {
            f.locale = Locale(identifier: "zh_CN")
            f.dateFormat = Calendar.current.isDateInToday(date) ? "H:mm" : "M月d日EEE H:mm"
        } else {
            f.locale = Locale(identifier: "en_US")
            f.dateFormat = Calendar.current.isDateInToday(date) ? "h:mm a" : "EEE, MMM d h:mm a"
        }
        return f.string(from: date)
    }
}

struct UserBubble: View {
    var text: String

    var body: some View {
        HStack {
            Spacer(minLength: 120)
            Text(text)
                .font(.system(size: Theme.messageSize + 1))
                .foregroundStyle(Theme.textPrimary)
                .lineSpacing(5)
                .textSelection(.enabled)
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
                .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Theme.userBubble))
        }
    }
}

struct WorkedForHeader: View {
    var duration: TimeInterval?
    @Binding var expanded: Bool
    var hasItems: Bool
    @State private var hover = false

    var body: some View {
        Button {
            if hasItems { withAnimation(.easeInOut(duration: 0.15)) { expanded.toggle() } }
        } label: {
            HStack(spacing: 6) {
                Text(tr("Worked for %@", TurnBuilder.formatDuration(duration ?? 0)))
                    .font(Theme.metaFont)
                if hasItems {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 9.5, weight: .semibold))
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                }
            }
            .foregroundStyle(hover ? Theme.textSecondary : Theme.textTertiary)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}

struct WorkItemsView: View {
    var items: [TurnItem]
    var live: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach(items) { item in
                switch item {
                case let .thinking(_, text):
                    ThinkingRow(text: text)
                case let .tool(step):
                    ToolRow(step: step)
                case let .message(_, text):
                    MarkdownView(text: text)
                        .padding(.vertical, 4)
                }
            }
        }
    }
}

struct ThinkingRow: View {
    var text: String
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button { open.toggle() } label: {
                HStack(spacing: 6) {
                    Text(tr("Thought")).font(Theme.metaFont)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 9.5, weight: .semibold))
                        .rotationEffect(.degrees(open ? 90 : 0))
                }
                .foregroundStyle(Theme.textTertiary)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            if open {
                Text(text.trimmingCharacters(in: .whitespacesAndNewlines))
                    .font(.system(size: 12.5))
                    .foregroundStyle(Theme.textTertiary)
                    .lineSpacing(4)
                    .textSelection(.enabled)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) { Rectangle().fill(Theme.divider).frame(width: 2) }
            }
        }
    }
}

struct ToolRow: View {
    var step: ToolStep
    @State private var open = false
    @State private var hover = false

    var body: some View {
        let d = ToolLabel.describe(step)
        VStack(alignment: .leading, spacing: 8) {
            Button { if step.done { open.toggle() } } label: {
                HStack(spacing: 6) {
                    label(d)
                    if let stat = ToolLabel.diffStat(step), step.done {
                        Text("+\(stat.add)").foregroundStyle(Theme.diffAdd)
                        if stat.del > 0 { Text("-\(stat.del)").foregroundStyle(Theme.diffDel) }
                    }
                    if step.isError {
                        Image(systemName: "exclamationmark.circle").foregroundStyle(Theme.accentRed)
                    }
                    if !step.done {
                        ProgressView().controlSize(.mini).scaleEffect(0.75)
                    } else if hover || open {
                        Image(systemName: "chevron.right")
                            .font(.system(size: 9.5, weight: .semibold))
                            .rotationEffect(.degrees(open ? 90 : 0))
                            .foregroundStyle(Theme.textTertiary)
                    }
                }
                .font(Theme.metaFont)
                .lineLimit(1)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .onHover { hover = $0 }
            if open, let result = step.result {
                ToolOutput(step: step, result: result)
            }
        }
    }

    @ViewBuilder
    private func label(_ d: (verb: String, subject: String, code: Bool)) -> some View {
        let parts = tr(d.verb).components(separatedBy: "%@")
        HStack(spacing: 4) {
            Text(parts.first?.trimmingCharacters(in: .whitespaces) ?? "")
                .foregroundStyle(Theme.textTertiary)
            if d.code {
                Text(d.subject)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(Theme.textSecondary)
                    .truncationMode(.tail)
            } else {
                Text(d.subject)
                    .foregroundStyle(Theme.textSecondary)
                    .truncationMode(.middle)
            }
        }
    }
}

struct ToolOutput: View {
    var step: ToolStep
    var result: String

    var body: some View {
        let m = ToolLabel.args(step.arguments)
        let command = step.name == "bash" ? ToolLabel.first(m, "command", "cmd") : ""
        VStack(alignment: .leading, spacing: 6) {
            if !command.isEmpty {
                Text("$ " + command)
                    .font(Theme.monoFont)
                    .foregroundStyle(Theme.textSecondary)
                    .textSelection(.enabled)
            }
            ScrollView(.vertical) {
                Text(result.isEmpty ? " " : String(result.prefix(20000)))
                    .font(Theme.monoFont)
                    .foregroundStyle(step.isError ? Theme.accentRed : Theme.textBody)
                    .lineSpacing(2)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .frame(maxHeight: 240)
        }
        .padding(12)
        .background(RoundedRectangle(cornerRadius: 10).fill(Theme.codeBlock))
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Theme.divider, lineWidth: 1))
    }
}

struct MessageActions: View {
    var text: String
    @State private var copied = false

    var body: some View {
        HStack(spacing: 14) {
            Button {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(text, forType: .string)
                copied = true
                DispatchQueue.main.asyncAfter(deadline: .now() + 1.2) { copied = false }
            } label: {
                Image(systemName: copied ? "checkmark" : "square.on.square")
                    .font(.system(size: 12.5))
                    .foregroundStyle(Theme.textTertiary)
            }
            .buttonStyle(.plain)
            .help(copied ? tr("Copied") : tr("Copy"))
        }
    }
}

struct WorkingLabel: View {
    var text: String
    @State private var phase = false

    var body: some View {
        Text(text)
            .font(Theme.metaFont)
            .foregroundStyle(Theme.textTertiary)
            .opacity(phase ? 0.45 : 1)
            .animation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true), value: phase)
            .onAppear { phase = true }
    }
}

struct ErrorLine: View {
    var text: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: "exclamationmark.triangle")
            Text(text).textSelection(.enabled)
        }
        .font(Theme.metaFont)
        .foregroundStyle(Theme.accentRed)
    }
}

struct ApprovalCard: View {
    @EnvironmentObject var model: AppModel
    var approval: Approval

    var body: some View {
        let step = ToolStep(id: approval.id, name: approval.tool, arguments: approval.arguments)
        let m = ToolLabel.args(approval.arguments)
        let detail: String = {
            switch approval.tool {
            case "bash": return ToolLabel.first(m, "command", "cmd")
            case "write", "edit", "read": return ToolLabel.first(m, "file_path", "path")
            default: return approval.arguments
            }
        }()
        VStack(alignment: .leading, spacing: 10) {
            Text(tr("Allow Nomad to run %@?", approval.tool))
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Theme.textPrimary)
            if !detail.isEmpty {
                ScrollView(.vertical) {
                    Text(detail)
                        .font(Theme.monoFont)
                        .foregroundStyle(Theme.textBody)
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 120)
                .fixedSize(horizontal: false, vertical: true)
                .padding(10)
                .background(RoundedRectangle(cornerRadius: 8).fill(Theme.codeBlock))
            }
            if let stat = ToolLabel.diffStat(step) {
                HStack(spacing: 6) {
                    Text("+\(stat.add)").foregroundStyle(Theme.diffAdd)
                    Text("-\(stat.del)").foregroundStyle(Theme.diffDel)
                }
                .font(Theme.metaFont)
            }
            VStack(alignment: .leading, spacing: 2) {
                ApprovalOption(index: 1, title: tr("Yes")) { model.resolve(approval, "allow") }
                    .keyboardShortcut(.return, modifiers: [])
                ApprovalOption(index: 2, title: tr("Yes, and don't ask again for %@ this session", approval.tool)) {
                    model.resolve(approval, "session")
                }
                ApprovalOption(index: 3, title: tr("No, tell Nomad what to do differently")) { model.resolve(approval, "deny") }
                    .keyboardShortcut(.escape, modifiers: [])
            }
        }
        .padding(16)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Color.white))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).stroke(Theme.composerBorder, lineWidth: 1))
        .shadow(color: .black.opacity(0.05), radius: 10, x: 0, y: 3)
    }
}

struct ApprovalOption: View {
    var index: Int
    var title: String
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Text("\(index).")
                    .foregroundStyle(Theme.textTertiary)
                    .monospacedDigit()
                Text(title).foregroundStyle(Theme.textPrimary)
                Spacer()
            }
            .font(.system(size: 13.5))
            .padding(.horizontal, 10)
            .frame(height: 32)
            .background(RoundedRectangle(cornerRadius: 8).fill(hover ? Theme.hoverRow : .clear))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hover = $0 }
    }
}
