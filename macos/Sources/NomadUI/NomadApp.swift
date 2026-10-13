import AppKit
import SwiftUI

/// The Nomad desktop application.
public struct NomadApp: App {
    @StateObject private var model = AppModel()

    public init() {}

    public var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(model)
                .frame(minWidth: 900, minHeight: 600)
                .background(WindowConfigurator())
                .onAppear { model.start() }
        }
        .windowStyle(.hiddenTitleBar)
        .defaultSize(width: 1512, height: 949)
        .commands {
            CommandGroup(replacing: .newItem) {
                Button(tr("New chat")) { model.newChat() }
                    .keyboardShortcut("n", modifiers: .command)
                Button(tr("Open folder…")) { model.pickProjectFolder() }
                    .keyboardShortcut("o", modifiers: .command)
            }
            CommandGroup(after: .sidebar) {
                Button(tr("Toggle sidebar")) {
                    withAnimation(.easeInOut(duration: 0.2)) { model.sidebarVisible.toggle() }
                }
                .keyboardShortcut("b", modifiers: .command)
            }
        }
    }
}
