import AppKit
import Foundation
import SwiftUI

/// Live state of one thread's transcript.
public struct ConversationState: Equatable {
    public var events: [LoopEvent] = [] {
        didSet { turns = TurnBuilder.build(events) }
    }
    public private(set) var turns: [Turn] = []
    public var running = false
    public var pendingUser: String?
    public var loaded = false
    public var workspace: String?

    public init(events: [LoopEvent] = [], running: Bool = false) {
        self.events = events
        self.turns = TurnBuilder.build(events)
        self.running = running
    }
}

/// A tool call waiting for the user's decision.
public struct Approval: Identifiable {
    public var id: String
    public var threadID: String
    public var tool: String
    public var arguments: String
    var reply: (String) -> Void
}

/// Cross-device login progress.
public struct LoginState: Equatable {
    public var inProgress = false
    public var url: String?
    public var code = ""
    public var error: String?
}

/// Where the main pane points.
public enum Route: Hashable {
    case home
    case thread(String)
}

/// The observable app state shared by every view.
@MainActor
public final class AppModel: ObservableObject {
    public enum Connection: Equatable {
        case connecting
        case ready
        case failed(String)
    }

    @Published public var connection: Connection = .connecting
    @Published public var loggedIn = false
    @Published public var login = LoginState()
    @Published public var projects: [Project] = [] { didSet { persistProjects() } }
    @Published public var selectedProject: String? { didSet { defaults.set(selectedProject, forKey: "selectedProject") } }
    @Published public var threads: [ThreadSummary] = []
    @Published public var route: Route = .home
    @Published public var conversations: [String: ConversationState] = [:]
    @Published public var drafts: [String: String] = [:]
    @Published public var models: [ModelOption] = []
    @Published public var model = ""
    @Published public var effort = "high"
    @Published public var efforts: [String] = ["low", "medium", "high", "xhigh", "max"]
    @Published public var permission: PermissionMode = .ask { didSet { defaults.set(permission.rawValue, forKey: "permissionMode") } }
    @Published public var approvals: [Approval] = []
    @Published public var branches: [String: String] = [:]
    @Published public var collapsedProjects: Set<String> = []
    @Published public var expandedThreadLists: Set<String> = []
    @Published public var sidebarVisible = true
    @Published public var backStack: [Route] = []
    @Published public var forwardStack: [Route] = []

    private var client: AppServerClient?
    private let defaults: UserDefaults
    private let preview: Bool

    public init(defaults: UserDefaults = .standard, preview: Bool = false) {
        self.defaults = defaults
        self.preview = preview
        if let paths = defaults.stringArray(forKey: "projects") {
            projects = paths.map { Project(path: $0) }
        }
        selectedProject = defaults.string(forKey: "selectedProject") ?? projects.first?.path
        if let raw = defaults.string(forKey: "permissionMode"), let mode = PermissionMode(rawValue: raw) {
            permission = mode
        }
    }

    private func persistProjects() {
        guard !preview else { return }
        defaults.set(projects.map(\.path), forKey: "projects")
    }


    public func start() {
        guard !preview else { return }
        connection = .connecting
        guard let bin = AppServerClient.locateBinary() else {
            connection = .failed("nomad binary not found. Install it (npm i -g @chyroc/nomad) or set NOMAD_BIN.")
            return
        }
        let c = AppServerClient(executable: bin)
        c.onNotification = { [weak self] method, params in self?.handleNotification(method, params) }
        c.onRequest = { [weak self] method, params, reply in self?.handleRequest(method, params, reply) }
        c.onExit = { [weak self] tail in
            guard let self, self.client === c else { return }
            self.connection = .failed(tail.isEmpty ? "nomad app-server exited" : tail)
        }
        do {
            try c.start()
        } catch {
            connection = .failed(error.localizedDescription)
            return
        }
        client = c
        Task { await initialize() }
    }

    public func restart() {
        client?.stop()
        client = nil
        start()
    }

    private struct InitResult: Decodable {
        var loggedIn: Bool
        var model: String
        var effort: String
        var efforts: [String]?
    }

    private func initialize() async {
        guard let client else { return }
        do {
            let res = try await client.call("initialize", as: InitResult.self)
            loggedIn = res.loggedIn
            model = res.model
            if !res.effort.isEmpty { effort = res.effort }
            if let e = res.efforts, !e.isEmpty { efforts = e.filter { $0 != "none" } }
            connection = .ready
            await refreshThreads()
            for p in projects { refreshBranch(p.path) }
            if loggedIn { await refreshModels() }
        } catch {
            connection = .failed(error.localizedDescription)
        }
    }

    private struct ThreadList: Decodable { var threads: [ThreadSummary] }

    public func refreshThreads() async {
        guard let client else { return }
        guard let res = try? await client.call("thread/list", as: ThreadList.self) else { return }
        let running = Set(threads.filter { $0.running == true }.map(\.id))
        threads = res.threads.map { t in
            var t = t
            if running.contains(t.id) || conversations[t.id]?.running == true { t.running = true }
            return t
        }
        for t in res.threads where !t.workspace.isEmpty && !projects.contains(where: { $0.path == t.workspace }) {
            projects.append(Project(path: t.workspace))
            refreshBranch(t.workspace)
        }
        if selectedProject == nil { selectedProject = projects.first?.path }
    }

    private struct ModelList: Decodable {
        var models: [ModelOption]
        var model: String
        var effort: String
    }

    public func refreshModels() async {
        guard let client else { return }
        guard let res = try? await client.call("models/list", as: ModelList.self) else { return }
        models = res.models
        if model.isEmpty { model = res.model.isEmpty ? (res.models.first?.id ?? "") : res.model }
    }

    private struct WorkspaceInfo: Decodable { var branch: String }

    public func refreshBranch(_ path: String) {
        guard let client else { return }
        Task {
            if let info = try? await client.call("workspace/info", ["path": path], as: WorkspaceInfo.self) {
                branches[path] = info.branch
            }
        }
    }


    public func navigate(_ to: Route) {
        guard to != route else { return }
        backStack.append(route)
        forwardStack.removeAll()
        route = to
        if case let .thread(id) = to { loadThread(id) }
    }

    public func goBack() {
        guard let prev = backStack.popLast() else { return }
        forwardStack.append(route)
        route = prev
    }

    public func goForward() {
        guard let next = forwardStack.popLast() else { return }
        backStack.append(route)
        route = next
    }

    public func newChat(in project: String? = nil) {
        if let project { selectedProject = project }
        navigate(.home)
    }

    public var currentThreadID: String? {
        if case let .thread(id) = route { return id }
        return nil
    }

    public func thread(_ id: String) -> ThreadSummary? {
        threads.first { $0.id == id }
    }

    public func title(for id: String) -> String {
        if let t = thread(id), !t.title.isEmpty { return t.title }
        if let text = conversations[id]?.pendingUser ?? conversations[id]?.turns.first?.userText {
            return String(text.prefix(60))
        }
        return tr("New chat")
    }

    public func threads(in project: String) -> [ThreadSummary] {
        threads.filter { $0.workspace == project }
    }

    private struct ReadResult: Decodable {
        var events: [LoopEvent]
        var workspace: String?
        var running: Bool?
    }

    public func loadThread(_ id: String) {
        guard let client, conversations[id]?.loaded != true, !id.hasPrefix("pending:") else { return }
        Task {
            guard let res = try? await client.call("thread/read", ["threadId": id], as: ReadResult.self) else { return }
            var conv = conversations[id] ?? ConversationState()
            if conv.events.isEmpty { conv.events = res.events }
            conv.loaded = true
            conv.workspace = res.workspace
            if res.running == true { conv.running = true }
            conversations[id] = conv
        }
    }


    public func addProject(_ url: URL) {
        let path = url.standardizedFileURL.path
        if !projects.contains(where: { $0.path == path }) {
            projects.insert(Project(path: path), at: 0)
        }
        collapsedProjects.remove(path)
        selectedProject = path
        refreshBranch(path)
        navigate(.home)
    }

    public func removeProject(_ path: String) {
        projects.removeAll { $0.path == path }
        if selectedProject == path { selectedProject = projects.first?.path }
    }

    public func pickProjectFolder() {
        let panel = NSOpenPanel()
        panel.canChooseDirectories = true
        panel.canChooseFiles = false
        panel.allowsMultipleSelection = false
        panel.prompt = tr("Open folder…").replacingOccurrences(of: "…", with: "")
        if panel.runModal() == .OK, let url = panel.url {
            addProject(url)
        }
    }


    public var draftKey: String {
        switch route {
        case .home: return "home"
        case let .thread(id): return id
        }
    }

    public var draft: String {
        get { drafts[draftKey] ?? "" }
        set { drafts[draftKey] = newValue }
    }

    public func send(_ raw: String, images: [URL] = []) {
        let text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty, let client else { return }
        drafts[draftKey] = ""
        var params: [String: Any] = [
            "text": text,
            "permissionMode": permission.rawValue,
            "model": model,
            "effort": effort,
        ]
        if !images.isEmpty { params["images"] = images.map(\.path) }
        switch route {
        case .home:
            guard let workspace = selectedProject else {
                pickProjectFolder()
                drafts["home"] = raw
                return
            }
            params["workspace"] = workspace
            let key = "pending:" + UUID().uuidString
            var conv = ConversationState()
            conv.pendingUser = text
            conv.running = true
            conv.loaded = true
            conv.workspace = workspace
            conversations[key] = conv
            navigate(.thread(key))
            Task { await startTurn(client, params, pendingKey: key) }
        case let .thread(id):
            guard !id.hasPrefix("pending:") else { return }
            params["threadId"] = id
            conversations[id, default: ConversationState()].pendingUser = text
            conversations[id]?.running = true
            setThreadRunning(id, true)
            Task { await startTurn(client, params, pendingKey: nil) }
        }
    }

    private struct TurnStarted: Decodable { var threadId: String }

    private func startTurn(_ client: AppServerClient, _ params: [String: Any], pendingKey: String?) async {
        do {
            let res = try await client.call("turn/start", params, as: TurnStarted.self)
            guard let key = pendingKey else { return }
            let pending = conversations.removeValue(forKey: key)
            var real = conversations[res.threadId] ?? ConversationState()
            real.loaded = true
            real.workspace = pending?.workspace
            if real.events.isEmpty { real.pendingUser = pending?.pendingUser }
            real.running = true
            conversations[res.threadId] = real
            if route == .thread(key) { route = .thread(res.threadId) }
            backStack = backStack.map { $0 == .thread(key) ? .thread(res.threadId) : $0 }
        } catch {
            let key = pendingKey ?? (params["threadId"] as? String ?? "")
            conversations[key]?.running = false
            let text = conversations[key]?.pendingUser
            conversations[key]?.pendingUser = nil
            var ev = conversations[key]?.events ?? []
            if let text { ev.append(LoopEvent(kind: "user_message", time: Date(), content: text)) }
            ev.append(LoopEvent(kind: "error", time: Date(), content: error.localizedDescription))
            conversations[key]?.events = ev
            if pendingKey == nil { setThreadRunning(key, false) }
        }
    }

    public func interrupt() {
        guard let id = currentThreadID, let client else { return }
        Task { _ = try? await client.call("turn/interrupt", ["threadId": id]) }
    }

    public func resolve(_ approval: Approval, _ decision: String) {
        approvals.removeAll { $0.id == approval.id }
        approval.reply(decision)
    }

    public func setModel(_ id: String) {
        model = id
        if let m = models.first(where: { $0.id == id }), let supported = m.efforts, !supported.isEmpty, !supported.contains(effort) {
            effort = supported.last ?? effort
        }
        persistDefaults()
    }

    public func setEffort(_ e: String) {
        effort = e
        persistDefaults()
    }

    private func persistDefaults() {
        guard let client else { return }
        let params: [String: Any] = ["model": model, "effort": effort]
        Task { _ = try? await client.call("defaults/set", params) }
    }

    public func effortChoices() -> [String] {
        if let m = models.first(where: { $0.id == model }), let e = m.efforts, !e.isEmpty {
            return efforts.filter { e.contains($0) }
        }
        return efforts
    }

    public var modelLabel: String {
        if let m = models.first(where: { $0.id == model }) { return m.name }
        return model.isEmpty ? tr("Model") : model
    }

    private func setThreadRunning(_ id: String, _ running: Bool) {
        if let i = threads.firstIndex(where: { $0.id == id }) {
            threads[i].running = running
            if !running { threads[i].updatedAt = Date() }
        }
    }


    public func startLogin() {
        guard let client else { return }
        login = LoginState(inProgress: true)
        Task {
            do {
                try await client.call("auth/login")
            } catch {
                login.inProgress = false
                login.error = error.localizedDescription
            }
        }
    }

    public func submitLoginCode() {
        guard let client else { return }
        let code = login.code.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !code.isEmpty else { return }
        Task {
            do {
                try await client.call("auth/submitCode", ["code": code])
            } catch {
                login.error = error.localizedDescription
            }
        }
    }

    public func logout() {
        guard let client else { return }
        Task {
            _ = try? await client.call("auth/logout")
            loggedIn = false
            models = []
        }
    }


    private struct EventParams: Decodable {
        var threadId: String
        var event: LoopEvent
    }

    private struct CompletedParams: Decodable {
        var threadId: String
        var error: String?
        var interrupted: Bool?
    }

    private struct AuthURL: Decodable { var url: String }

    private struct AuthCompleted: Decodable {
        var ok: Bool
        var error: String?
        var model: String?
        var effort: String?
    }

    func handleNotification(_ method: String, _ params: Data) {
        let dec = JSONTime.decoder()
        switch method {
        case "event":
            guard let p = try? dec.decode(EventParams.self, from: params) else { return }
            var conv = conversations[p.threadId] ?? ConversationState()
            conv.events.append(p.event)
            if p.event.kind == "user_message" {
                conv.pendingUser = nil
                conv.running = true
            }
            conv.loaded = true
            conversations[p.threadId] = conv
        case "thread/started":
            guard let t = try? dec.decode(ThreadSummary.self, from: params) else { return }
            if !threads.contains(where: { $0.id == t.id }) {
                threads.insert(t, at: 0)
            }
            if !t.workspace.isEmpty && !projects.contains(where: { $0.path == t.workspace }) {
                projects.insert(Project(path: t.workspace), at: 0)
            }
            conversations[t.id, default: ConversationState()].running = true
            conversations[t.id]?.loaded = true
        case "turn/completed":
            guard let p = try? dec.decode(CompletedParams.self, from: params) else { return }
            var conv = conversations[p.threadId] ?? ConversationState()
            conv.running = false
            conv.pendingUser = nil
            if p.interrupted == true {
                conv.events.append(LoopEvent(kind: "local_interrupted", time: Date()))
            } else if let err = p.error {
                conv.events.append(LoopEvent(kind: "error", time: Date(), content: err))
            }
            conversations[p.threadId] = conv
            approvals.removeAll { $0.threadID == p.threadId }
            setThreadRunning(p.threadId, false)
            Task { await refreshThreads() }
        case "auth/url":
            guard let p = try? dec.decode(AuthURL.self, from: params) else { return }
            login.url = p.url
            if let url = URL(string: p.url) { NSWorkspace.shared.open(url) }
        case "auth/completed":
            guard let p = try? dec.decode(AuthCompleted.self, from: params) else { return }
            login.inProgress = false
            if p.ok {
                loggedIn = true
                login = LoginState()
                if let m = p.model, !m.isEmpty { model = m }
                if let e = p.effort, !e.isEmpty { effort = e }
                Task { await refreshModels() }
            } else {
                login.error = p.error ?? "login failed"
            }
        default:
            break
        }
    }

    private struct ApprovalParams: Decodable {
        var threadId: String
        var tool: String
    }

    func handleRequest(_ method: String, _ params: Data, _ reply: @escaping @Sendable (Any) -> Void) {
        guard method == "approval/request",
              let p = try? JSONTime.decoder().decode(ApprovalParams.self, from: params) else {
            reply(["decision": "deny"])
            return
        }
        var args = ""
        if let obj = try? JSONSerialization.jsonObject(with: params) as? [String: Any], let a = obj["arguments"] {
            if let s = a as? String {
                args = s
            } else if let d = try? JSONSerialization.data(withJSONObject: a, options: [.sortedKeys]) {
                args = String(data: d, encoding: .utf8) ?? ""
            }
        }
        approvals.append(Approval(id: UUID().uuidString, threadID: p.threadId, tool: p.tool, arguments: args) { decision in
            reply(["decision": decision])
        })
    }
}
