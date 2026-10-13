import Foundation

/// Error returned by a JSON-RPC call.
public struct RPCError: Error, LocalizedError, Sendable {
    public var code: Int
    public var message: String
    public var errorDescription: String? { message }
}

/// Talks newline-delimited JSON-RPC 2.0 with a `nomad app-server` child.
public final class AppServerClient: @unchecked Sendable {
    public typealias NotificationHandler = @MainActor (_ method: String, _ params: Data) -> Void
    public typealias RequestHandler = @MainActor (_ method: String, _ params: Data, _ reply: @escaping @Sendable (Any) -> Void) -> Void

    private let executable: URL
    private let environment: [String: String]
    private let process = Process()
    private let stdinPipe = Pipe()
    private let stdoutPipe = Pipe()
    private let stderrPipe = Pipe()
    private let lock = NSLock()
    private var buffer = Data()
    private var nextID = 0
    private var pending: [String: (Result<Data, Error>) -> Void] = [:]
    private var stderrTail = ""

    public var onNotification: NotificationHandler?
    public var onRequest: RequestHandler?
    public var onExit: (@MainActor (String) -> Void)?

    public init(executable: URL, environment: [String: String] = [:]) {
        self.executable = executable
        self.environment = environment
    }

    /// Locates the nomad binary: NOMAD_BIN, the app bundle, then common install paths.
    public static func locateBinary() -> URL? {
        let fm = FileManager.default
        var candidates: [String] = []
        if let env = ProcessInfo.processInfo.environment["NOMAD_BIN"] { candidates.append(env) }
        if let res = Bundle.main.resourceURL?.appendingPathComponent("nomad").path { candidates.append(res) }
        let home = fm.homeDirectoryForCurrentUser.path
        candidates += [
            "\(home)/go/bin/nomad", "/opt/homebrew/bin/nomad", "/usr/local/bin/nomad",
            "\(home)/.local/bin/nomad",
        ]
        for c in candidates where fm.isExecutableFile(atPath: c) {
            return URL(fileURLWithPath: c)
        }
        return nil
    }

    public func start() throws {
        process.executableURL = executable
        process.arguments = ["app-server"]
        process.environment = ProcessInfo.processInfo.environment.merging(environment) { _, new in new }
        process.currentDirectoryURL = FileManager.default.homeDirectoryForCurrentUser
        process.standardInput = stdinPipe
        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe
        stdoutPipe.fileHandleForReading.readabilityHandler = { [weak self] h in
            let data = h.availableData
            if data.isEmpty { return }
            self?.consume(data)
        }
        stderrPipe.fileHandleForReading.readabilityHandler = { [weak self] h in
            let data = h.availableData
            guard let self, !data.isEmpty, let s = String(data: data, encoding: .utf8) else { return }
            self.lock.lock()
            self.stderrTail = String((self.stderrTail + s).suffix(2000))
            self.lock.unlock()
        }
        process.terminationHandler = { [weak self] _ in
            guard let self else { return }
            self.lock.lock()
            let waiters = self.pending
            self.pending = [:]
            let tail = self.stderrTail
            self.lock.unlock()
            for (_, w) in waiters { w(.failure(RPCError(code: -1, message: "nomad exited"))) }
            let handler = self.onExit
            DispatchQueue.main.async {
                MainActor.assumeIsolated { handler?(tail) }
            }
        }
        try process.run()
    }

    public func stop() {
        stdoutPipe.fileHandleForReading.readabilityHandler = nil
        try? stdinPipe.fileHandleForWriting.close()
        if process.isRunning { process.terminate() }
    }

    /// Sends a request and returns the raw JSON of its result.
    @discardableResult
    public func call(_ method: String, _ params: [String: Any] = [:]) async throws -> Data {
        try await withCheckedThrowingContinuation { cont in
            lock.lock()
            nextID += 1
            let id = "c\(nextID)"
            pending[id] = { cont.resume(with: $0) }
            lock.unlock()
            write(["jsonrpc": "2.0", "id": id, "method": method, "params": params])
        }
    }

    public func call<T: Decodable>(_ method: String, _ params: [String: Any] = [:], as type: T.Type) async throws -> T {
        let data = try await call(method, params)
        return try JSONTime.decoder().decode(T.self, from: data)
    }

    private func write(_ obj: [String: Any]) {
        guard var data = try? JSONSerialization.data(withJSONObject: obj) else { return }
        data.append(0x0A)
        lock.lock()
        defer { lock.unlock() }
        try? stdinPipe.fileHandleForWriting.write(contentsOf: data)
    }

    private func consume(_ chunk: Data) {
        lock.lock()
        buffer.append(chunk)
        var lines: [Data] = []
        while let nl = buffer.firstIndex(of: 0x0A) {
            lines.append(buffer.subdata(in: buffer.startIndex..<nl))
            buffer.removeSubrange(buffer.startIndex...nl)
        }
        lock.unlock()
        for line in lines where !line.isEmpty {
            dispatch(line)
        }
    }

    private func dispatch(_ line: Data) {
        guard let obj = try? JSONSerialization.jsonObject(with: line) as? [String: Any] else { return }
        let method = obj["method"] as? String
        let params = (obj["params"]).flatMap { try? JSONSerialization.data(withJSONObject: $0, options: [.fragmentsAllowed]) } ?? Data("{}".utf8)
        if let method {
            if let id = obj["id"] {
                let handler = onRequest
                let reply: @Sendable (Any) -> Void = { [weak self] result in
                    self?.write(["jsonrpc": "2.0", "id": id, "result": result])
                }
                DispatchQueue.main.async {
                    MainActor.assumeIsolated {
                        if let handler { handler(method, params, reply) } else { reply([String: Any]()) }
                    }
                }
            } else {
                let handler = onNotification
                DispatchQueue.main.async {
                    MainActor.assumeIsolated { handler?(method, params) }
                }
            }
            return
        }
        guard let id = obj["id"] as? String else { return }
        lock.lock()
        let waiter = pending.removeValue(forKey: id)
        lock.unlock()
        if let err = obj["error"] as? [String: Any] {
            waiter?(.failure(RPCError(code: err["code"] as? Int ?? -1, message: err["message"] as? String ?? "error")))
        } else {
            let result = obj["result"].flatMap { try? JSONSerialization.data(withJSONObject: $0, options: [.fragmentsAllowed]) } ?? Data("{}".utf8)
            waiter?(.success(result))
        }
    }
}
