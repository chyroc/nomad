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
	if !strings.Contains(out, "3 lines") || !strings.Contains(out, "Ctrl+O") {
		t.Fatalf("multi-line success should collapse: %q", out)
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
	if !strings.Contains(out, "single short") {
		t.Fatalf("tiny result should be inline: %q", out)
	}
	if strings.Contains(out, "Ctrl+O") {
		t.Fatalf("tiny result should not show fold hint: %q", out)
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
