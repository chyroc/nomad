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
	for _, want := range []string{"❯ do the thing", "done", "✻ Worked for", "1 tools"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"partial", "bash(", "✓", "Thought"} {
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
	if strings.Contains(out, "line\nline") {
		t.Fatalf("tool body must stay out of replay:\n%s", out)
	}
}
