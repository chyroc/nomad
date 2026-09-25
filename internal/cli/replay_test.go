package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chyroc/nomad/internal/loop"
)

func TestReplayRichRendering(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	now := time.Now()
	evs := []loop.Event{
		{Kind: loop.EvUserMessage, Time: now, Content: "do the thing"},
		{Kind: loop.EvAssistantThinking, Time: now, Content: "hmm"},
		{Kind: loop.EvToolCall, Time: now, ToolCall: &loop.ToolCall{Name: "bash", Arguments: `{"command":"go test"}`}},
		{Kind: loop.EvToolResult, Time: now.Add(time.Second), ToolName: "bash", Result: "ok\n"},
		{Kind: loop.EvAssistantChunk, Time: now, Content: "partial"},
		{Kind: loop.EvAssistantMessage, Time: now, Content: "done"},
		{Kind: loop.EvTurnEnd, Time: now.Add(2 * time.Second), Usage: &loop.Usage{InputTokens: 3, OutputTokens: 4}},
	}
	a.replay(evs)
	out := ansi.Strip(buf.String())
	for _, want := range []string{"❯ do the thing", "● bash(go test)", "⎿  ok", "Thought for", "(ctrl+o to expand)", "done", "✻ Worked for", "1 tools"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"partial", "✓"} {
		if strings.Contains(out, banned) {
			t.Errorf("replay must not print transient step %q:\n%s", banned, out)
		}
	}
}

func TestReplayRegistersToolFold(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	long := strings.Repeat("line\n", 20)
	a.replay([]loop.Event{
		{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{Name: "bash", Arguments: `{"command":"ls"}`}},
		{Kind: loop.EvToolResult, ToolName: "bash", Result: long},
	})
	if out := buf.String(); strings.Contains(out, "line\nline") {
		t.Fatalf("replay must not print tool bodies:\n%s", out)
	}
	a.foldMu.Lock()
	folds := len(a.folds)
	a.foldMu.Unlock()
	if folds != 1 {
		t.Fatalf("replay should register the tool fold, got %d", folds)
	}
	lines := a.latestFoldLines()
	if len(lines) < 20 || !strings.Contains(strings.Join(lines, "\n"), "line") {
		t.Fatalf("fold expansion missing body: %d lines", len(lines))
	}
}

func TestReplayErrorAndFold(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	long := strings.Repeat("line\n", 20)
	a.replay([]loop.Event{
		{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{Name: "bash", Arguments: `{"command":"ls"}`}},
		{Kind: loop.EvToolResult, ToolName: "bash", Result: long, IsError: true},
		{Kind: loop.EvError, Content: "boom"},
	})
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "boom") {
		t.Fatalf("error replay wrong:\n%s", out)
	}
	if !strings.Contains(out, "⎿  Error: line") {
		t.Fatalf("erroring bash call should render an error block:\n%s", out)
	}
	if !strings.Contains(out, "+17 lines (ctrl+o to expand)") {
		t.Fatalf("20-line error body should show three rows and fold the rest:\n%s", out)
	}
}

func TestInteractiveAnswerAlwaysShowsThoughtLine(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.turnStart = time.Now().Add(-3 * time.Second)
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantMessage, Time: time.Now(), Content: "hi there"})
	out := buf.String()
	if !strings.Contains(out, "Thought for ") || !strings.Contains(out, "(ctrl+o to expand)") {
		t.Fatalf("missing Thought line before answer:\n%s", out)
	}
	if !strings.Contains(out, "hi there") {
		t.Fatalf("answer missing:\n%s", out)
	}
	idxThought := strings.Index(out, "Thought")
	idxAnswer := strings.Index(out, "hi there")
	if idxThought < 0 || idxAnswer < 0 || idxThought > idxAnswer {
		t.Fatalf("Thought line must precede the answer:\n%s", out)
	}

	buf.Reset()
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantMessage, Time: time.Now(), Content: "again"})
	if n := strings.Count(buf.String(), "Thought"); n != 0 {
		t.Fatalf("Thought line should appear once per turn, got:\n%s", buf.String())
	}
}

func TestInteractiveThinkingFoldPrintsPermanentThought(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false, bar: newStatusBar(), model: "doubao-x"}
	a.turnStart = time.Now().Add(-2 * time.Second)
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantThinking, Time: time.Now().Add(-time.Second), Content: "hmm"})
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantChunk, Time: time.Now(), Content: "answer"})
	out := buf.String()
	if !strings.Contains(out, "Thought for") {
		t.Fatalf("thinking turn should keep the Thought summary permanently:\n%s", out)
	}
}

func TestBrowseToolMergesIntoDeferredThoughtLine(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false, bar: newStatusBar(), model: "doubao-x"}
	a.paths.Workspace = t.TempDir()
	a.thoughtLineShown = false
	a.turnStart = time.Now().Add(-6 * time.Second)

	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantThinking, Time: time.Now().Add(-6 * time.Second), Content: "where is it"})
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvToolCall, Time: time.Now().Add(-5 * time.Second),
		ToolCall: &loop.ToolCall{ID: "g1", Name: "ls", Arguments: `{}`}})
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvToolResult, Time: time.Now(),
		ToolCall: &loop.ToolCall{ID: "g1", Name: "ls"}, Result: "tmp/\nvar/\n"})

	if strings.Contains(buf.String(), "(ctrl+o to expand)") {
		t.Fatalf("Thought line must not print before the browsing action finishes:\n%s", buf.String())
	}

	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantChunk, Time: time.Now(), Content: "done"})
	out := buf.String()
	if !strings.Contains(out, "listed 2 directories (ctrl+o to expand)") ||
		!strings.Contains(out, "Thought for") {
		t.Fatalf("Thought line should carry the browse suffix:\n%s", out)
	}
	if n := strings.Count(out, "(ctrl+o to expand)"); n != 1 {
		t.Fatalf("exactly one Thought line expected, got %d:\n%s", n, out)
	}
}
