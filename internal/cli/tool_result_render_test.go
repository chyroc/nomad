package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chyroc/nomad/internal/loop"
)

func toolResultEvent(name, result string, isErr bool) loop.Event {
	return loop.Event{Kind: loop.EvToolResult, ToolName: name, Result: result, IsError: isErr}
}

func TestFinishToolCollapsesMultiLineSuccess(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.finishTool(toolResultEvent("bash", "one\ntwo\nthree", false))
	out := buf.String()
	if !strings.Contains(out, "⎿  one") || !strings.Contains(out, "ctrl+o to expand") {
		t.Fatalf("multi-line success should collapse to one summary line: %q", out)
	}
	if strings.Contains(out, "\n  two\n") {
		t.Fatalf("body lines must not be expanded: %q", out)
	}
}

func TestFinishToolInlinesTinySuccess(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.finishTool(toolResultEvent("bash", "single short", false))
	out := buf.String()
	if !strings.Contains(out, "⎿  single short") {
		t.Fatalf("tiny result should be inline: %q", out)
	}
	if strings.Contains(out, "ctrl+o") {
		t.Fatalf("tiny result should not show fold hint: %q", out)
	}
}

func TestFinishToolWriteSummary(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.rememberToolArgs("t1", `{"file_path":"x.txt","content":"a\nb\nc"}`)
	ev := toolResultEvent("write", "File created successfully at: x.txt", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "write"}
	a.finishTool(ev)
	if !strings.Contains(buf.String(), "Wrote 3 lines to x.txt") {
		t.Fatalf("write summary missing: %q", buf.String())
	}
}

func TestFinishToolAlwaysShowsErrorBody(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	long := strings.Repeat("e\n", 10) + "boom"
	a.finishTool(toolResultEvent("bash", long, true))
	out := buf.String()
	if !strings.Contains(out, "boom") {
		t.Fatalf("error body must stay visible: %q", out)
	}
}

func TestRenderAnswerAnchorOnce(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.renderAnswer("first chunk")
	a.renderAnswer("second chunk")
	if n := strings.Count(buf.String(), "●"); n != 1 {
		t.Fatalf("anchor printed %d times, want 1", n)
	}

	buf.Reset()
	a2 := &App{out: &buf, color: false}
	a2.answerAnchorPrinted = false
	a2.renderAnswer("next turn")
	if !strings.Contains(buf.String(), "●") {
		t.Fatal("fresh turn should print anchor")
	}
}
