import SwiftUI

/// The window content: sidebar, separator and the main pane.
public struct RootView: View {
    @EnvironmentObject var model: AppModel

    public init() {}

    public var body: some View {
        HStack(spacing: 0) {
            if model.sidebarVisible {
                SidebarView()
                    .frame(width: Theme.sidebarWidth)
                    .transition(.move(edge: .leading))
                Rectangle().fill(Theme.separator).frame(width: 1)
            }
            MainPane()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .background(Theme.mainBackground)
        .overlay(alignment: .topLeading) { TitlebarButtons() }
        .ignoresSafeArea()
        .environment(\.colorScheme, .light)
    }
}

struct TitlebarButtons: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        HStack(spacing: 8) {
            IconButton(symbol: "sidebar.left", help: tr("Toggle sidebar")) {
                withAnimation(.easeInOut(duration: 0.2)) { model.sidebarVisible.toggle() }
            }
            IconButton(symbol: "arrow.left", help: tr("Back"), enabled: !model.backStack.isEmpty) { model.goBack() }
            IconButton(symbol: "arrow.right", help: tr("Forward"), enabled: !model.forwardStack.isEmpty) { model.goForward() }
        }
        .padding(.leading, 90)
        .frame(height: Theme.titlebarHeight)
    }
}

/// A borderless 22pt toolbar glyph with a hover plate.
struct IconButton: View {
    var symbol: String
    var help: String = ""
    var size: CGFloat = 14
    var enabled = true
    var action: () -> Void
    @State private var hover = false

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: size, weight: .regular))
                .foregroundStyle(enabled ? Theme.icon : Theme.iconMuted.opacity(0.6))
                .frame(width: 24, height: 24)
                .background(RoundedRectangle(cornerRadius: 6).fill(hover && enabled ? Theme.hoverRow : .clear))
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .onHover { hover = $0 }
        .help(help)
    }
}

struct MainPane: View {
    @EnvironmentObject var model: AppModel

    var body: some View {
        switch model.connection {
        case .connecting:
            VStack(spacing: 12) {
                ProgressView().controlSize(.small)
                Text(tr("Starting nomad…")).font(Theme.metaFont).foregroundStyle(Theme.textTertiary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case let .failed(message):
            VStack(spacing: 14) {
                Image(systemName: "exclamationmark.triangle").font(.system(size: 28)).foregroundStyle(Theme.accentOrange)
                Text(tr("Could not start nomad")).font(.system(size: 17, weight: .semibold))
                Text(message)
                    .font(Theme.monoFont)
                    .foregroundStyle(Theme.textSecondary)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 520)
                    .textSelection(.enabled)
                Button(tr("Retry")) { model.restart() }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .ready:
            if !model.loggedIn {
                LoginView()
            } else {
                switch model.route {
                case .home:
                    HomeView()
                case let .thread(id):
                    ThreadView(threadID: id)
                }
            }
        }
    }
}
