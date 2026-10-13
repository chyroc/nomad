import Foundation

/// One loop event as emitted by `nomad app-server`.
public struct LoopEvent: Codable, Equatable, Sendable {
    public struct ToolCall: Codable, Equatable, Sendable {
        public var id: String
        public var name: String?
        public var arguments: String?
    }

    public struct Usage: Codable, Equatable, Sendable {
        public var input_tokens: Int
        public var output_tokens: Int
    }

    public var kind: String
    public var time: Date?
    public var content: String?
    public var tool_call: ToolCall?
    public var tool_name: String?
    public var result: String?
    public var is_error: Bool?
    public var usage: Usage?

    public init(kind: String, time: Date? = nil, content: String? = nil, toolCall: ToolCall? = nil,
                toolName: String? = nil, result: String? = nil, isError: Bool? = nil, usage: Usage? = nil) {
        self.kind = kind
        self.time = time
        self.content = content
        self.tool_call = toolCall
        self.tool_name = toolName
        self.result = result
        self.is_error = isError
        self.usage = usage
    }
}

/// A sidebar thread entry.
public struct ThreadSummary: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var title: String
    public var workspace: String
    public var updatedAt: Date?
    public var running: Bool?
}

/// A selectable model.
public struct ModelOption: Codable, Identifiable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var efforts: [String]?
}

/// A local project (workspace folder).
public struct Project: Codable, Identifiable, Equatable, Hashable, Sendable {
    public var path: String
    public var id: String { path }
    public var name: String { (path as NSString).lastPathComponent }
}

/// Tool permission modes understood by the runner gate.
public enum PermissionMode: String, CaseIterable, Identifiable, Sendable {
    case ask = "default"
    case acceptEdits
    case plan
    case full = "bypassPermissions"

    public var id: String { rawValue }

    var title: String {
        switch self {
        case .ask: return tr("Ask for approval")
        case .acceptEdits: return tr("Auto-accept edits")
        case .plan: return tr("Plan mode")
        case .full: return tr("Full access")
        }
    }

    var detail: String {
        switch self {
        case .ask: return tr("Ask before running commands and editing files")
        case .acceptEdits: return tr("Edit files without asking, ask before commands")
        case .plan: return tr("Read-only planning, no changes")
        case .full: return tr("Run anything without asking")
        }
    }

    var symbol: String {
        switch self {
        case .ask: return "hand.raised"
        case .acceptEdits: return "pencil.and.outline"
        case .plan: return "list.bullet.clipboard"
        case .full: return "exclamationmark.shield"
        }
    }
}

enum EffortLabel {
    static func title(_ effort: String) -> String {
        let zh: [String: String] = [
            "none": "关闭", "minimal": "最低", "low": "低", "medium": "中",
            "high": "高", "xhigh": "超高", "max": "极高",
        ]
        let en: [String: String] = [
            "none": "Off", "minimal": "Minimal", "low": "Low", "medium": "Medium",
            "high": "High", "xhigh": "Extra high", "max": "Max",
        ]
        return (L10n.isChinese ? zh[effort] : en[effort]) ?? effort
    }
}

enum JSONTime {
    static func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let c = try dec.singleValueContainer()
            let s = try c.decode(String.self)
            if let date = parse(s) { return date }
            throw DecodingError.dataCorruptedError(in: c, debugDescription: "bad date \(s)")
        }
        return d
    }

    static func parse(_ raw: String) -> Date? {
        var s = raw
        if let dot = s.firstIndex(of: ".") {
            var end = s.index(after: dot)
            while end < s.endIndex, s[end].isNumber { end = s.index(after: end) }
            let frac = s[s.index(after: dot)..<end]
            let kept = String(frac.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0)
            s.replaceSubrange(dot..<end, with: "." + kept)
        }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}
