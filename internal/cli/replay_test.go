package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
	out := buf.String()
	for _, want := range []string{"❯ do the thing", "Thought", "bash", "⎿  ok", "done", "✻ Worked for"} {
		if !strings.Contains(out, want) {
			t.Errorf("replay output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "partial") {
		t.Error("streaming chunk should be skipped during replay")
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
	out := buf.String()
	if !strings.Contains(out, "⎿") || !strings.Contains(out, "boom") {
		t.Fatalf("error replay wrong:\n%s", out)
	}
	if !strings.Contains(out, "more lines") {
		t.Fatalf("long result should be folded:\n%s", out)
	}
}
