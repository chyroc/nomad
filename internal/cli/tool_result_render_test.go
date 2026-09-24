package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/loop"
)

func toolResultEvent(name, result string, isErr bool) loop.Event {
	return loop.Event{Kind: loop.EvToolResult, ToolName: name, Result: result, IsError: isErr}
}

func TestFinishToolOneLineMultiLineSuccess(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.finishTool(toolResultEvent("bash", "one\ntwo\nthree", false))
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "● bash") || !strings.Contains(out, "✓") || !strings.Contains(out, "one") {
		t.Fatalf("multi-line success should render one compact line: %q", out)
	}
	if !strings.Contains(out, "ctrl+o to expand") {
		t.Fatalf("multi-line success should hint the fold: %q", out)
	}
	if strings.Contains(out, "\ntwo\n") {
		t.Fatalf("body lines must not be expanded: %q", out)
	}
	if strings.Contains(out, "⎿") {
		t.Fatalf("result must not indent below the call line: %q", out)
	}
}

func TestFinishToolOneLineTinySuccess(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.finishTool(toolResultEvent("bash", "single short", false))
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "● bash") || !strings.Contains(out, "✓ · single short") {
		t.Fatalf("tiny result should render one inline line: %q", out)
	}
	if strings.Contains(out, "ctrl+o") {
		t.Fatalf("tiny result should not show fold hint: %q", out)
	}
}

func TestFinishToolOneLineWithDuration(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	start := time.Now().Add(-3 * time.Second)
	a.rememberToolCall(&loop.ToolCall{ID: "t1", Name: "bash", Arguments: `{"command":"go test"}`}, start)
	ev := toolResultEvent("bash", "ok", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "bash"}
	ev.Time = time.Now()
	a.finishTool(ev)
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "bash(go test)") || !strings.Contains(out, "3s") {
		t.Fatalf("result line should carry invocation and duration: %q", out)
	}
}

func TestFinishToolWriteSummaryAndFoldDiff(t *testing.T) {
	var buf bytes.Buffer
	dir := t.TempDir()
	a := &App{out: &buf, color: false, paths: config.Paths{Workspace: dir}}
	a.rememberToolCall(&loop.ToolCall{ID: "t1", Name: "write", Arguments: `{"file_path":"x.txt","content":"a\nb\nc"}`}, time.Now())
	ev := toolResultEvent("write", "File created successfully at: x.txt", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "write"}
	a.finishTool(ev)
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "Wrote 3 lines to x.txt") {
		t.Fatalf("write summary missing: %q", out)
	}
}

func TestFinishToolErrorOneLine(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	long := strings.Repeat("e\n", 10) + "boom"
	a.finishTool(toolResultEvent("bash", long, true))
	out := ansi.Strip(buf.String())
	if !strings.Contains(out, "✗") {
		t.Fatalf("error line should carry the failure mark: %q", out)
	}
	if !strings.Contains(out, "ctrl+o to expand") {
		t.Fatalf("multi-line error body should register a fold: %q", out)
	}
	if strings.Contains(out, "\nboom") {
		t.Fatalf("error body must stay collapsed: %q", out)
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
