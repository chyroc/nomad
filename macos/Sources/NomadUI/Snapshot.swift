import AppKit
import SwiftUI

/// Renders the app with sample data into PNG files without showing a
/// window, activating the app or touching the user's defaults.
public enum Snapshot {
    public static let size = CGSize(width: 1512, height: 949)

    @MainActor
    public static func run(outputDirectory: String) {
        let app = NSApplication.shared
        app.setActivationPolicy(.prohibited)
        let dir = URL(fileURLWithPath: outputDirectory, isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let scenarios: [(String, AppModel)] = [
            ("home", SampleData.home()),
            ("home-noproject", SampleData.homeWithoutProject()),
            ("thread", SampleData.thread()),
            ("thread-live", SampleData.liveThread()),
            ("login", SampleData.login()),
        ]
        for (name, model) in scenarios {
            let url = dir.appendingPathComponent("\(name).png")
            render(RootView().environmentObject(model), to: url)
            print(url.path)
        }
    }

    /// Writes the 1024px app icon.
    @MainActor
    public static func renderIcon(to path: String) {
        NSApplication.shared.setActivationPolicy(.prohibited)
        let icon = ZStack {
            RoundedRectangle(cornerRadius: 100, style: .continuous)
                .fill(LinearGradient(colors: [Color(hex: 0xFFFFFF), Color(hex: 0xEEEFF1)], startPoint: .top, endPoint: .bottom))
                .frame(width: 412, height: 412)
                .shadow(color: .black.opacity(0.18), radius: 10, x: 0, y: 6)
            NomadLogo(tint: Color(hex: 0x1A1C1F), lineWidth: 2.4)
                .frame(width: 250, height: 250)
        }
        .frame(width: 512, height: 512)
        render(icon, size: CGSize(width: 512, height: 512), to: URL(fileURLWithPath: path))
    }

    @MainActor
    static func render<V: View>(_ view: V, size: CGSize = Snapshot.size, to url: URL) {
        let hosting = NSHostingView(rootView: view.frame(width: size.width, height: size.height))
        hosting.frame = CGRect(origin: .zero, size: size)
        let window = NSWindow(contentRect: hosting.frame, styleMask: [.borderless], backing: .buffered, defer: false)
        window.contentView = hosting
        hosting.layoutSubtreeIfNeeded()
        RunLoop.main.run(until: Date().addingTimeInterval(0.8))
        hosting.layoutSubtreeIfNeeded()
        guard let rep = hosting.bitmapImageRepForCachingDisplay(in: hosting.bounds) else { return }
        hosting.cacheDisplay(in: hosting.bounds, to: rep)
        if let data = rep.representation(using: .png, properties: [:]) {
            try? data.write(to: url)
        }
        window.close()
    }
}

/// Deterministic sample state used by snapshots and tests.
@MainActor
public enum SampleData {
    static func base() -> AppModel {
        let defaults = UserDefaults(suiteName: "nomad.snapshot.\(UUID().uuidString)")!
        let m = AppModel(defaults: defaults, preview: true)
        m.connection = .ready
        m.loggedIn = true
        m.projects = [
            Project(path: "/Users/demo/code/nomad"),
            Project(path: "/Users/demo/code/beadlet"),
        ]
        m.selectedProject = "/Users/demo/code/nomad"
        m.branches = ["/Users/demo/code/nomad": "master", "/Users/demo/code/beadlet": "main"]
        m.models = [
            ModelOption(id: "doubao-seed-2-0-pro", name: "Doubao Seed 2.0 Pro", efforts: ["low", "medium", "high", "max"]),
            ModelOption(id: "deepseek-v3-2", name: "DeepSeek V3.2", efforts: nil),
        ]
        m.model = "doubao-seed-2-0-pro"
        m.effort = "max"
        m.permission = .full
        let now = Date()
        let titles: [(String, String)] = [
            ("t1", "Add a macOS app for nomad"),
            ("t2", "修复 SSE 断线重连"),
            ("t3", "Explain the permission gate"),
            ("t4", "升级 ark-runtime-go 到 0.9"),
            ("t5", "Write tests for store meta"),
            ("t6", "整理 README 的安装说明"),
        ]
        m.threads = titles.enumerated().map { i, t in
            ThreadSummary(id: t.0, title: t.1, workspace: "/Users/demo/code/nomad",
                          updatedAt: now.addingTimeInterval(Double(-i) * 3600), running: i == 1)
        } + [
            ThreadSummary(id: "b1", title: "Design the paywall screen", workspace: "/Users/demo/code/beadlet", updatedAt: now, running: false),
            ThreadSummary(id: "b2", title: "Localize onboarding copy", workspace: "/Users/demo/code/beadlet", updatedAt: now, running: false),
            ThreadSummary(id: "c1", title: "解释一下 SSE 和 WebSocket 的区别", workspace: "/Users/demo/.nomad/chats", updatedAt: now, running: false),
            ThreadSummary(id: "c2", title: "Draft a release note", workspace: "/Users/demo/.nomad/chats", updatedAt: now, running: false),
        ]
        m.chatWorkspace = "/Users/demo/.nomad/chats"
        return m
    }

    public static func home() -> AppModel {
        base()
    }

    public static func homeWithoutProject() -> AppModel {
        let m = base()
        m.selectedProject = nil
        return m
    }

    public static func login() -> AppModel {
        let m = base()
        m.loggedIn = false
        return m
    }

    static func t(_ base: Date, _ offset: TimeInterval) -> Date { base.addingTimeInterval(offset) }

    public static func thread() -> AppModel {
        let m = base()
        let start = Calendar.current.date(byAdding: .day, value: -2, to: Date())!
        var events: [LoopEvent] = [
            LoopEvent(kind: "user_message", time: start, content: "给 nomad 加一个 macOS 客户端，界面参考 Codex。"),
            LoopEvent(kind: "assistant_thinking", time: t(start, 3), content: "Look at the runner and store first."),
            LoopEvent(kind: "tool_call", time: t(start, 5), toolCall: .init(id: "a", name: "bash", arguments: #"{"command":"ls internal"}"#)),
            LoopEvent(kind: "tool_result", time: t(start, 6), toolCall: .init(id: "a"), toolName: "bash", result: "ark\ncli\nconfig\nloop\nstore"),
            LoopEvent(kind: "tool_call", time: t(start, 8), toolCall: .init(id: "b", name: "read", arguments: #"{"path":"internal/loop/loop.go"}"#)),
            LoopEvent(kind: "tool_result", time: t(start, 9), toolCall: .init(id: "b"), toolName: "read", result: "package loop"),
            LoopEvent(kind: "tool_call", time: t(start, 20), toolCall: .init(id: "c", name: "edit", arguments: #"{"path":"internal/store/session.go","old_string":"a\nb","new_string":"a\nb\nc\nd"}"#)),
            LoopEvent(kind: "tool_result", time: t(start, 21), toolCall: .init(id: "c"), toolName: "edit", result: "ok"),
            LoopEvent(kind: "assistant_message", time: t(start, 2870), content: """
            本轮已推进：
            - 新增 `nomad app-server`：stdio 上的 JSON-RPC，供桌面端调用。
            - 会话记录增加 `meta.json` 侧车文件，按项目分组。
            - 完整检查、`go test -race ./...` 全部通过。

            macOS 客户端的界面骨架已经完成，下一步接入真实会话并做像素级比对。
            """),
            LoopEvent(kind: "turn_end", time: t(start, 2872)),
        ]
        let second = t(start, 3 * 3600)
        events += [
            LoopEvent(kind: "user_message", time: second, content: "继续，把审批卡片也做出来"),
            LoopEvent(kind: "tool_call", time: t(second, 4), toolCall: .init(id: "d", name: "bash", arguments: #"{"command":"swift build -c release"}"#)),
            LoopEvent(kind: "tool_result", time: t(second, 60), toolCall: .init(id: "d"), toolName: "bash", result: "Build complete!"),
            LoopEvent(kind: "assistant_message", time: t(second, 1295), content: """
            已完成审批卡片：工具调用需要确认时，卡片出现在输入框上方，支持 **允许一次**、**本会话允许** 和 **拒绝**。

            ```swift
            model.resolve(approval, "session")
            ```

            1. 点击「是」只放行这一次。
            2. 「本会话不再询问」会记住该工具。
            """),
            LoopEvent(kind: "turn_end", time: t(second, 1296)),
        ]
        m.conversations["t1"] = ConversationState(events: events)
        m.conversations["t1"]?.loaded = true
        m.route = .thread("t1")
        return m
    }

    public static func liveThread() -> AppModel {
        let m = thread()
        let now = Date()
        var conv = m.conversations["t1"] ?? ConversationState()
        conv.events += [
            LoopEvent(kind: "user_message", time: now, content: "把改动提交并推送"),
            LoopEvent(kind: "tool_call", time: now, toolCall: .init(id: "e", name: "bash", arguments: #"{"command":"git status --short"}"#)),
            LoopEvent(kind: "tool_result", time: now, toolCall: .init(id: "e"), toolName: "bash", result: " M internal/store/session.go"),
            LoopEvent(kind: "tool_call", time: now, toolCall: .init(id: "f", name: "bash", arguments: #"{"command":"git push origin master"}"#)),
        ]
        conv.running = true
        m.conversations["t1"] = conv
        m.approvals = [Approval(id: "ap1", threadID: "t1", tool: "bash", arguments: #"{"command":"git push origin master"}"#) { _ in }]
        return m
    }
}
