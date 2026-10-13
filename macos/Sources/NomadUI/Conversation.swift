import Foundation

/// One tool invocation inside a turn.
public struct ToolStep: Identifiable, Equatable, Sendable {
    public var id: String
    public var name: String
    public var arguments: String
    public var result: String?
    public var isError: Bool = false
    public var done: Bool { result != nil }
}

/// A renderable entry of a turn's work log.
public enum TurnItem: Identifiable, Equatable, Sendable {
    case thinking(id: Int, text: String)
    case tool(ToolStep)
    case message(id: Int, text: String)

    public var id: String {
        switch self {
        case let .thinking(id, _): return "think-\(id)"
        case let .tool(step): return "tool-\(step.id)"
        case let .message(id, _): return "msg-\(id)"
        }
    }
}

/// One user prompt and everything the agent did in response.
public struct Turn: Identifiable, Equatable, Sendable {
    public var id: Int
    public var userText: String?
    public var startedAt: Date?
    public var endedAt: Date?
    public var items: [TurnItem] = []
    public var complete = false
    public var error: String?
    public var interrupted = false

    /// The closing assistant message shown below the collapsed work log.
    public var finalMessage: String? {
        guard complete || error != nil || interrupted, case let .message(_, text)? = items.last else { return nil }
        return text
    }

    /// Items rendered inside the "worked for" disclosure.
    public var workItems: [TurnItem] {
        finalMessage == nil ? items : Array(items.dropLast())
    }

    public var duration: TimeInterval? {
        guard let s = startedAt, let e = endedAt else { return nil }
        return max(0, e.timeIntervalSince(s))
    }
}

enum TurnBuilder {
    static func build(_ events: [LoopEvent]) -> [Turn] {
        var turns: [Turn] = []
        var seq = 0
        func current() -> Int {
            if turns.isEmpty {
                turns.append(Turn(id: 0, startedAt: nil))
            }
            return turns.count - 1
        }
        for ev in events {
            seq += 1
            switch ev.kind {
            case "user_message":
                turns.append(Turn(id: turns.count, userText: ev.content ?? "", startedAt: ev.time))
            case "assistant_message":
                let i = current()
                if let text = ev.content, !text.isEmpty {
                    turns[i].items.append(.message(id: seq, text: text))
                }
            case "assistant_thinking":
                let i = current()
                let text = ev.content ?? ""
                if case let .thinking(id, prev)? = turns[i].items.last {
                    turns[i].items[turns[i].items.count - 1] = .thinking(id: id, text: prev + text)
                } else {
                    turns[i].items.append(.thinking(id: seq, text: text))
                }
            case "tool_call":
                let i = current()
                let call = ev.tool_call
                turns[i].items.append(.tool(ToolStep(
                    id: call?.id ?? "seq\(seq)",
                    name: call?.name ?? ev.tool_name ?? "tool",
                    arguments: call?.arguments ?? ""
                )))
            case "tool_result":
                let i = current()
                let callID = ev.tool_call?.id
                if let idx = turns[i].items.lastIndex(where: {
                    if case let .tool(step) = $0 { return step.id == callID || (callID == nil && !step.done) }
                    return false
                }), case var .tool(step) = turns[i].items[idx] {
                    step.result = ev.result ?? ""
                    step.isError = ev.is_error ?? false
                    turns[i].items[idx] = .tool(step)
                } else {
                    turns[i].items.append(.tool(ToolStep(
                        id: callID ?? "seq\(seq)", name: ev.tool_name ?? "tool", arguments: "",
                        result: ev.result ?? "", isError: ev.is_error ?? false
                    )))
                }
            case "turn_end":
                let i = current()
                turns[i].complete = true
                turns[i].endedAt = ev.time ?? turns[i].endedAt
                if turns[i].startedAt == nil { turns[i].startedAt = ev.time }
            case "local_interrupted":
                let i = current()
                turns[i].interrupted = true
            case "error":
                let i = current()
                turns[i].error = ev.content ?? "error"
            default:
                break
            }
        }
        return turns
    }

    static func formatDuration(_ t: TimeInterval) -> String {
        let total = Int(t.rounded())
        let h = total / 3600, m = (total % 3600) / 60, s = total % 60
        if h > 0 { return "\(h)h \(m)m" }
        if m > 0 { return "\(m)m \(s)s" }
        return "\(s)s"
    }
}

/// Short human labels for tool steps.
enum ToolLabel {
    static func args(_ json: String) -> [String: Any] {
        guard let data = json.data(using: .utf8),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return obj
    }

    static func first(_ m: [String: Any], _ keys: String...) -> String {
        for k in keys {
            if let v = m[k] {
                let s = "\(v)".trimmingCharacters(in: .whitespacesAndNewlines)
                if !s.isEmpty { return s }
            }
        }
        return ""
    }

    static func fileName(_ path: String) -> String {
        path.isEmpty ? "" : (path as NSString).lastPathComponent
    }

    /// Returns the verb template key and the subject of a step.
    static func describe(_ step: ToolStep) -> (verb: String, subject: String, code: Bool) {
        let m = args(step.arguments)
        let done = step.done
        switch step.name {
        case "bash":
            let cmd = first(m, "command", "cmd").components(separatedBy: "\n").first ?? ""
            return (done ? "Ran %@" : "Running %@", cmd, true)
        case "read":
            return (done ? "Read %@" : "Reading %@", fileName(first(m, "file_path", "path")), false)
        case "write":
            return (done ? "Created %@" : "Creating %@", fileName(first(m, "file_path", "path")), false)
        case "edit":
            return (done ? "Edited %@" : "Editing %@", fileName(first(m, "file_path", "path")), false)
        case "glob", "grep":
            return (done ? "Searched %@" : "Searching %@", first(m, "pattern", "query", "path"), true)
        default:
            return (done ? "Called %@" : "Calling %@", step.name, false)
        }
    }

    /// Line counts added and removed by an edit or write step.
    static func diffStat(_ step: ToolStep) -> (add: Int, del: Int)? {
        let m = args(step.arguments)
        func lines(_ s: String) -> Int { s.isEmpty ? 0 : s.components(separatedBy: "\n").count }
        switch step.name {
        case "edit":
            let old = first(m, "old_string"), new = first(m, "new_string")
            if old.isEmpty && new.isEmpty { return nil }
            return (lines(new), lines(old))
        case "write":
            let content = m["content"] as? String ?? ""
            return content.isEmpty ? nil : (lines(content), 0)
        default:
            return nil
        }
    }
}
