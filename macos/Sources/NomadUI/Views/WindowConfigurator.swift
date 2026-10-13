import AppKit
import SwiftUI

/// Makes the hosting window borderless-looking and places the traffic
/// lights at the same offsets as the reference client.
struct WindowConfigurator: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let view = TrackingView()
        return view
    }

    func updateNSView(_ nsView: NSView, context: Context) {}

    final class TrackingView: NSView {
        private var observers: [NSObjectProtocol] = []

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            guard let window else { return }
            window.titlebarAppearsTransparent = true
            window.titleVisibility = .hidden
            window.styleMask.insert(.fullSizeContentView)
            window.isMovableByWindowBackground = false
            window.backgroundColor = .white
            observers.forEach(NotificationCenter.default.removeObserver)
            observers = [
                NSWindow.didResizeNotification, NSWindow.didExitFullScreenNotification,
                NSWindow.didBecomeKeyNotification,
            ].map { name in
                NotificationCenter.default.addObserver(forName: name, object: window, queue: .main) { [weak window] _ in
                    if let window { Self.layoutTrafficLights(window) }
                }
            }
            DispatchQueue.main.async { Self.layoutTrafficLights(window) }
        }

        static func layoutTrafficLights(_ window: NSWindow) {
            let kinds: [NSWindow.ButtonType] = [.closeButton, .miniaturizeButton, .zoomButton]
            let buttons = kinds.compactMap { window.standardWindowButton($0) }
            guard let first = buttons.first, let container = first.superview?.superview else { return }
            let height = Theme.titlebarHeight
            var frame = container.frame
            frame.size.height = height
            frame.origin.y = window.frame.height - height
            container.frame = frame
            for (i, b) in buttons.enumerated() {
                let x = 16 + CGFloat(i) * 23
                b.setFrameOrigin(NSPoint(x: x, y: (height - b.frame.height) / 2))
            }
        }
    }
}
