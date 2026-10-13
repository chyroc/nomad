import Foundation
import XCTest
@testable import NomadUI

final class TurnBuilderTests: XCTestCase {
    func testCollapsesWorkAndKeepsFinalMessage() {
        let t0 = Date(timeIntervalSince1970: 1000)
        let events = [
            LoopEvent(kind: "user_message", time: t0, content: "hi"),
            LoopEvent(kind: "assistant_thinking", time: t0, content: "a"),
            LoopEvent(kind: "assistant_thinking", time: t0, content: "b"),
            LoopEvent(kind: "tool_call", time: t0, toolCall: .init(id: "1", name: "bash", arguments: #"{"command":"ls"}"#)),
            LoopEvent(kind: "assistant_message", time: t0, content: "checking"),
            LoopEvent(kind: "tool_result", time: t0, toolCall: .init(id: "1"), toolName: "bash", result: "x"),
            LoopEvent(kind: "assistant_message", time: t0, content: "done"),
            LoopEvent(kind: "turn_end", time: t0.addingTimeInterval(65)),
            LoopEvent(kind: "turn_end", time: t0.addingTimeInterval(66)),
        ]
        let turns = TurnBuilder.build(events)
        XCTAssertEqual(turns.count, 1)
        let turn = turns[0]
        XCTAssertEqual(turn.userText, "hi")
        XCTAssertEqual(turn.finalMessage, "done")
        XCTAssertEqual(turn.workItems.count, 3)
        guard case let .thinking(_, text) = turn.workItems[0] else { return XCTFail("expected thinking") }
        XCTAssertEqual(text, "ab")
        guard case let .tool(step) = turn.workItems[1] else { return XCTFail("expected tool") }
        XCTAssertEqual(step.result, "x")
        XCTAssertEqual(TurnBuilder.formatDuration(turn.duration ?? 0), "1m 6s")
    }

    func testLiveTurnHasNoFinalMessage() {
        let events = [
            LoopEvent(kind: "user_message", content: "go"),
            LoopEvent(kind: "assistant_message", content: "partial"),
        ]
        let turn = TurnBuilder.build(events)[0]
        XCTAssertNil(turn.finalMessage)
        XCTAssertEqual(turn.workItems.count, 1)
    }

    func testInterruptAndErrorMarkTurn() {
        let turns = TurnBuilder.build([
            LoopEvent(kind: "user_message", content: "a"),
            LoopEvent(kind: "local_interrupted"),
            LoopEvent(kind: "user_message", content: "b"),
            LoopEvent(kind: "error", content: "boom"),
        ])
        XCTAssertTrue(turns[0].interrupted)
        XCTAssertEqual(turns[1].error, "boom")
    }

    func testToolLabels() {
        let edit = ToolStep(id: "1", name: "edit", arguments: #"{"path":"a/b.go","old_string":"x","new_string":"x\ny"}"#, result: "ok")
        XCTAssertEqual(ToolLabel.describe(edit).verb, "Edited %@")
        XCTAssertEqual(ToolLabel.describe(edit).subject, "b.go")
        XCTAssertEqual(ToolLabel.diffStat(edit)?.add, 2)
        XCTAssertEqual(ToolLabel.diffStat(edit)?.del, 1)
        let bash = ToolStep(id: "2", name: "bash", arguments: #"{"command":"go test ./...\necho"}"#)
        XCTAssertEqual(ToolLabel.describe(bash).verb, "Running %@")
        XCTAssertEqual(ToolLabel.describe(bash).subject, "go test ./...")
    }
}

final class MarkdownTests: XCTestCase {
    func testBlocks() {
        let blocks = MarkdownParser.parse("""
        # Title
        Intro line
        continued
        - one
          wrapped
        - [x] two
        1. first

        ```go
        fmt.Println("x")
        ```
        > quoted
        | a | b |
        |---|---|
        | 1 | 2 |
        ---
        """)
        XCTAssertEqual(blocks, [
            .heading(level: 1, text: "Title"),
            .paragraph("Intro line\ncontinued"),
            .listItem(ordered: nil, depth: 0, text: "one\nwrapped"),
            .listItem(ordered: nil, depth: 0, text: "☑ two"),
            .listItem(ordered: 1, depth: 0, text: "first"),
            .code(language: "go", text: "fmt.Println(\"x\")"),
            .quote("quoted"),
            .table([["a", "b"], ["1", "2"]]),
            .rule,
        ])
    }

    func testCJKBreakable() {
        XCTAssertEqual(CJK.breakable("ab工具"), "ab工\u{200B}具\u{200B}")
    }
}

final class JSONTimeTests: XCTestCase {
    func testParsesGoNanoTimestamps() throws {
        let d = JSONTime.parse("2026-10-13T12:00:00.123456789+08:00")
        XCTAssertNotNil(d)
        XCTAssertNotNil(JSONTime.parse("2026-10-13T04:00:00Z"))
        let ev = try JSONTime.decoder().decode(LoopEvent.self, from: Data(#"{"kind":"user_message","time":"2026-10-13T12:00:00.5+08:00","content":"x"}"#.utf8))
        XCTAssertEqual(ev.content, "x")
        XCTAssertNotNil(ev.time)
    }
}

@MainActor
final class AppModelTests: XCTestCase {
    func testNotificationsDriveConversation() throws {
        let model = SampleData.home()
        let event = #"{"threadId":"n1","event":{"kind":"user_message","time":"2026-10-13T12:00:00Z","content":"hello"}}"#
        model.handleNotification("thread/started", Data(#"{"id":"n1","title":"hello","workspace":"/tmp/new","updatedAt":"2026-10-13T12:00:00Z","running":true}"#.utf8))
        model.handleNotification("event", Data(event.utf8))
        XCTAssertEqual(model.threads.first?.id, "n1")
        XCTAssertTrue(model.projects.contains { $0.path == "/tmp/new" })
        XCTAssertEqual(model.conversations["n1"]?.turns.first?.userText, "hello")
        XCTAssertEqual(model.conversations["n1"]?.running, true)
        model.handleNotification("turn/completed", Data(#"{"threadId":"n1","interrupted":true}"#.utf8))
        XCTAssertEqual(model.conversations["n1"]?.running, false)
        XCTAssertEqual(model.conversations["n1"]?.turns.last?.interrupted, true)
    }

    func testApprovalRequestRepliesWithDecision() {
        let model = SampleData.home()
        let replied = expectation(description: "reply")
        model.handleRequest("approval/request", Data(#"{"threadId":"t1","tool":"bash","arguments":{"command":"ls"}}"#.utf8)) { result in
            XCTAssertEqual((result as? [String: String])?["decision"], "session")
            replied.fulfill()
        }
        XCTAssertEqual(model.approvals.count, 1)
        XCTAssertEqual(model.approvals[0].arguments, #"{"command":"ls"}"#)
        model.resolve(model.approvals[0], "session")
        XCTAssertTrue(model.approvals.isEmpty)
        wait(for: [replied], timeout: 1)
    }
}

final class AppServerClientTests: XCTestCase {
    private func nomadBinary() throws -> URL {
        if let env = ProcessInfo.processInfo.environment["NOMAD_BIN"] {
            return URL(fileURLWithPath: env)
        }
        let repo = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let bin = repo.appendingPathComponent("bin/nomad")
        guard FileManager.default.isExecutableFile(atPath: bin.path) else {
            throw XCTSkip("build bin/nomad first (make build)")
        }
        return bin
    }

    func testTalksToRealAppServer() async throws {
        let bin = try nomadBinary()
        let home = FileManager.default.temporaryDirectory.appendingPathComponent("nomad-ui-test-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: home) }
        let client = AppServerClient(executable: bin, environment: ["HOME": home.path])
        try client.start()
        defer { client.stop() }

        struct Init: Decodable { var loggedIn: Bool; var efforts: [String] }
        let res = try await client.call("initialize", as: Init.self)
        XCTAssertFalse(res.loggedIn)
        XCTAssertTrue(res.efforts.contains("high"))

        struct List: Decodable { var threads: [ThreadSummary] }
        let list = try await client.call("thread/list", as: List.self)
        XCTAssertTrue(list.threads.isEmpty)

        do {
            _ = try await client.call("no/such")
            XCTFail("expected an error")
        } catch let err as RPCError {
            XCTAssertEqual(err.code, -32601)
        }

        do {
            _ = try await client.call("turn/start", ["workspace": home.path, "text": "hi"])
            XCTFail("expected not logged in")
        } catch let err as RPCError {
            XCTAssertTrue(err.message.contains("not logged in"), err.message)
        }
    }
}
