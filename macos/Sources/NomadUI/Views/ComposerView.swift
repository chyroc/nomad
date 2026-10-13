import AppKit
import SwiftUI

/// The rounded input card: text, attachments and the control row.
struct ComposerView: View {
    @EnvironmentObject var model: AppModel
    var placeholder: String
    var running: Bool
    @State private var height: CGFloat = 20
    @State private var images: [URL] = []
    @State private var focus = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !images.isEmpty {
                attachments
                    .padding(.horizontal, 16)
                    .padding(.top, 12)
            }
            ZStack(alignment: .topLeading) {
                if model.draft.isEmpty {
                    Text(placeholder)
                        .font(.system(size: 13.5))
                        .foregroundStyle(Color(hex: 0xB8B8BA))
                        .allowsHitTesting(false)
                }
                ComposerTextView(
                    text: Binding(get: { model.draft }, set: { model.draft = $0 }),
                    height: $height,
                    focusToken: focus,
                    onSubmit: submit
                )
                .frame(height: min(max(height, 20), 220))
            }
            .padding(.horizontal, 16)
            .padding(.top, 15)
            .padding(.bottom, 10)
            .frame(minHeight: 62, alignment: .top)
            controls
                .padding(.horizontal, 10)
                .padding(.bottom, 10)
        }
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Color.white))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).stroke(Theme.composerBorder, lineWidth: 1))
        .shadow(color: .black.opacity(0.05), radius: 10, x: 0, y: 3)
        .contentShape(Rectangle())
        .onTapGesture { focus += 1 }
        .onAppear { focus += 1 }
        .onChange(of: model.route) { _, _ in focus += 1 }
    }

    private var canSend: Bool {
        !model.draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    private func submit() {
        guard !running, canSend else { return }
        model.send(model.draft, images: images)
        images = []
    }

    private var attachments: some View {
        HStack(spacing: 8) {
            ForEach(images, id: \.self) { url in
                HStack(spacing: 6) {
                    if let img = NSImage(contentsOf: url) {
                        Image(nsImage: img).resizable().scaledToFill().frame(width: 22, height: 22).clipShape(RoundedRectangle(cornerRadius: 4))
                    }
                    Text(url.lastPathComponent).font(.system(size: 12)).lineLimit(1).foregroundStyle(Theme.textSecondary)
                    Button { images.removeAll { $0 == url } } label: {
                        Image(systemName: "xmark").font(.system(size: 9, weight: .bold)).foregroundStyle(Theme.textTertiary)
                    }
                    .buttonStyle(.plain)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 5)
                .background(RoundedRectangle(cornerRadius: 8).fill(Theme.chipBar))
            }
        }
    }

    private var controls: some View {
        HStack(spacing: 6) {
            IconButton(symbol: "plus", help: tr("Attach image"), size: 15) { pickImages() }
            PermissionMenu()
            Spacer(minLength: 8)
            ModelMenu()
            SendButton(running: running, enabled: canSend) {
                if running { model.interrupt() } else { submit() }
            }
        }
    }

    private func pickImages() {
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.image]
        panel.allowsMultipleSelection = true
        if panel.runModal() == .OK {
            images += panel.urls.filter { !images.contains($0) }
        }
    }
}

struct SendButton: View {
    var running: Bool
    var enabled: Bool
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Circle().fill(running || enabled ? Theme.sendEnabled : Theme.sendDisabled)
                if running {
                    RoundedRectangle(cornerRadius: 2).fill(Color.white).frame(width: 9, height: 9)
                } else {
                    Image(systemName: "arrow.up")
                        .font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(.white)
                }
            }
            .frame(width: 28, height: 28)
        }
        .buttonStyle(.plain)
        .help(running ? tr("Stop") : tr("Send"))
        .padding(.leading, 6)
    }
}

struct PermissionMenu: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        Menu {
            ForEach(PermissionMode.allCases) { mode in
                Button {
                    model.permission = mode
                } label: {
                    if mode == model.permission {
                        Label(mode.title, systemImage: "checkmark")
                    } else {
                        Text(mode.title)
                    }
                }
            }
        } label: {
            let color = model.permission == .full ? Theme.accentOrange : Theme.textSecondary
            HStack(spacing: 4) {
                Image(systemName: model.permission.symbol)
                    .font(.system(size: 12))
                Text(model.permission.title)
                    .font(.system(size: 13))
            }
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .frame(height: 26)
            .contentShape(Rectangle())
        }
        .plainMenu()
        .help(model.permission.detail)
    }
}

struct ModelMenu: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        Menu {
            Section(tr("Model")) {
                ForEach(model.models) { m in
                    Button {
                        model.setModel(m.id)
                    } label: {
                        if m.id == model.model { Label(m.name, systemImage: "checkmark") } else { Text(m.name) }
                    }
                }
            }
            Section(tr("Reasoning")) {
                ForEach(model.effortChoices(), id: \.self) { e in
                    Button {
                        model.setEffort(e)
                    } label: {
                        if e == model.effort {
                            Label(EffortLabel.title(e), systemImage: "checkmark")
                        } else {
                            Text(EffortLabel.title(e))
                        }
                    }
                }
            }
        } label: {
            HStack(spacing: 6) {
                Text(model.modelLabel)
                Text(EffortLabel.title(model.effort))
            }
            .font(.system(size: 13))
            .foregroundStyle(Theme.textTertiary)
            .padding(.horizontal, 6)
            .frame(height: 26)
            .contentShape(Rectangle())
        }
        .plainMenu()
    }
}

/// An NSTextView that sends on Return and inserts a newline on Shift+Return,
/// leaving IME composition untouched.
struct ComposerTextView: NSViewRepresentable {
    @Binding var text: String
    @Binding var height: CGFloat
    var focusToken: Int
    var onSubmit: () -> Void

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeNSView(context: Context) -> NSScrollView {
        let tv = SubmitTextView()
        tv.delegate = context.coordinator
        tv.onSubmit = { context.coordinator.parent.onSubmit() }
        tv.isRichText = false
        tv.importsGraphics = false
        tv.allowsUndo = true
        tv.font = .systemFont(ofSize: 13.5)
        tv.textColor = NSColor(Theme.textPrimary)
        tv.insertionPointColor = NSColor(Theme.textPrimary)
        tv.drawsBackground = false
        tv.isAutomaticQuoteSubstitutionEnabled = false
        tv.isAutomaticDashSubstitutionEnabled = false
        tv.isAutomaticTextReplacementEnabled = false
        tv.textContainerInset = .zero
        tv.textContainer?.lineFragmentPadding = 0
        tv.textContainer?.widthTracksTextView = true
        tv.isVerticallyResizable = true
        tv.isHorizontallyResizable = false
        tv.autoresizingMask = [.width]
        let para = NSMutableParagraphStyle()
        para.lineSpacing = 4
        tv.defaultParagraphStyle = para
        tv.typingAttributes[.paragraphStyle] = para
        tv.string = text
        let sv = NSScrollView()
        sv.documentView = tv
        sv.drawsBackground = false
        sv.hasVerticalScroller = true
        sv.autohidesScrollers = true
        sv.borderType = .noBorder
        context.coordinator.textView = tv
        DispatchQueue.main.async { context.coordinator.recalc() }
        return sv
    }

    func updateNSView(_ nsView: NSScrollView, context: Context) {
        context.coordinator.parent = self
        guard let tv = context.coordinator.textView else { return }
        if tv.string != text {
            tv.string = text
            context.coordinator.recalc()
        }
        if context.coordinator.lastFocus != focusToken {
            context.coordinator.lastFocus = focusToken
            DispatchQueue.main.async { tv.window?.makeFirstResponder(tv) }
        }
    }

    final class Coordinator: NSObject, NSTextViewDelegate {
        var parent: ComposerTextView
        weak var textView: NSTextView?
        var lastFocus = -1

        init(_ parent: ComposerTextView) { self.parent = parent }

        func textDidChange(_ notification: Notification) {
            guard let tv = textView else { return }
            parent.text = tv.string
            recalc()
        }

        func recalc() {
            guard let tv = textView, let lm = tv.layoutManager, let tc = tv.textContainer else { return }
            lm.ensureLayout(for: tc)
            let h = ceil(lm.usedRect(for: tc).height)
            let value = max(h, 20)
            if abs(value - parent.height) > 0.5 {
                DispatchQueue.main.async { self.parent.height = value }
            }
        }
    }
}

final class SubmitTextView: NSTextView {
    var onSubmit: (() -> Void)?

    override func keyDown(with event: NSEvent) {
        let isReturn = event.keyCode == 36 || event.keyCode == 76
        if isReturn && !hasMarkedText() && !event.modifierFlags.contains(.shift) && !event.modifierFlags.contains(.option) {
            onSubmit?()
            return
        }
        super.keyDown(with: event)
    }
}
