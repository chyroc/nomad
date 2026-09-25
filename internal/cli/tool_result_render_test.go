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

func newBlockApp(t *testing.T) *App {
	t.Helper()
	var buf bytes.Buffer
	a := &App{out: &buf, color: false, bar: newStatusBar(), model: "doubao-x",
		paths: config.Paths{Workspace: t.TempDir()}}
	return a
}

func TestFinishToolBashBlockShowsPreview(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{Name: "bash", Arguments: `{}`}, time.Now())
	a.finishTool(toolResultEvent("bash", "one\ntwo\nthree", false))
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	for _, want := range []string{"● bash", "⎿  one", "    two", "    three"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bash block missing %q:\n%s", want, out)
		}
	}
}

func TestFinishToolEmptyBashSaysDone(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{Name: "bash", Arguments: `{}`}, time.Now())
	a.finishTool(toolResultEvent("bash", "   \n ", false))
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, "⎿  Done") {
		t.Fatalf("empty bash result should say Done:\n%s", out)
	}
}

func TestFinishToolDurationInSummary(t *testing.T) {
	a := newBlockApp(t)
	start := time.Now().Add(-3 * time.Second)
	a.rememberToolCall(&loop.ToolCall{ID: "t1", Name: "bash", Arguments: `{"command":"go test"}`}, start)
	ev := toolResultEvent("bash", "ok", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "bash"}
	ev.Time = start.Add(3 * time.Second)
	a.finishTool(ev)
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, "● bash(go test)") {
		t.Fatalf("header should carry invocation:\n%s", out)
	}
	if !strings.Contains(out, "⎿  ok") {
		t.Fatalf("summary should be the first output line:\n%s", out)
	}
}

func TestFinishToolWriteSummaryAndNumberedPreview(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{ID: "t1", Name: "write", Arguments: `{"file_path":"x.txt","content":"a\nb\nc"}`}, time.Now())
	ev := toolResultEvent("write", "created", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "write"}
	a.finishTool(ev)
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, "● Write(x.txt)") {
		t.Fatalf("write header missing:\n%s", out)
	}
	if !strings.Contains(out, "Wrote 3 lines to x.txt") {
		t.Fatalf("write summary missing:\n%s", out)
	}
	if !strings.Contains(out, "    1 a") || !strings.Contains(out, "    3 c") {
		t.Fatalf("write preview should show numbered added lines:\n%s", out)
	}
}

func TestFinishToolEditSummaryAddedRemoved(t *testing.T) {
	a := newBlockApp(t)
	writeFile(a.paths.Workspace+"/x.txt", "alpha\nbeta\n")
	a.rememberToolCall(&loop.ToolCall{ID: "t1", Name: "edit",
		Arguments: `{"file_path":"x.txt","old_string":"beta","new_string":"BETA\ngamma"}`}, time.Now())
	ev := toolResultEvent("edit", "ok", false)
	ev.ToolCall = &loop.ToolCall{ID: "t1", Name: "edit"}
	a.finishTool(ev)
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, "Added 2 lines, removed 1 lines") {
		t.Fatalf("edit summary missing:\n%s", out)
	}
	if !strings.Contains(out, "+BETA") {
		t.Fatalf("edit diff preview missing added line:\n%s", out)
	}
}

func TestFinishToolWebSearchBlock(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{ID: "s1", Name: "web_search", Arguments: `{"query":"什么是 JEV"}`}, time.Now())
	ev := toolResultEvent("web_search", "results…", false)
	ev.ToolCall = &loop.ToolCall{ID: "s1", Name: "web_search"}
	a.finishTool(ev)
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, `● Web Search("什么是 JEV")`) {
		t.Fatalf("web search header wrong:\n%s", out)
	}
	if !strings.Contains(out, "Did 1 search in") {
		t.Fatalf("web search summary wrong:\n%s", out)
	}
}

func TestFinishToolWebFetchError(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{ID: "f1", Name: "web_fetch",
		Arguments: `{"url":"https://www.cdc.gov.au/x"}`}, time.Now())
	msg := "Unable to verify if domain www.cdc.gov.au is safe to fetch."
	ev := toolResultEvent("web_fetch", msg, true)
	ev.ToolCall = &loop.ToolCall{ID: "f1", Name: "web_fetch"}
	a.finishTool(ev)
	out := ansi.Strip(a.out.(*bytes.Buffer).String())
	if !strings.Contains(out, "● Fetch(https://www.cdc.gov.au/x)") {
		t.Fatalf("fetch header wrong:\n%s", out)
	}
	if !strings.Contains(out, "⎿  Error: Unable to verify if domain") {
		t.Fatalf("fetch error summary wrong:\n%s", out)
	}
}

func TestFinishToolBrowseFoldsIntoNote(t *testing.T) {
	a := newBlockApp(t)
	a.rememberToolCall(&loop.ToolCall{ID: "g1", Name: "glob", Arguments: `{"pattern":"*.go"}`}, time.Now())
	ev := toolResultEvent("glob", "a.go\nb.go", false)
	ev.ToolCall = &loop.ToolCall{ID: "g1", Name: "glob"}
	a.finishTool(ev)
	out := a.out.(*bytes.Buffer).String()
	if strings.Contains(out, "● glob") {
		t.Fatalf("browse tool must not print its own block:\n%s", out)
	}
	note := strings.Join(a.browseNotes, ",")
	if !strings.Contains(note, "found 2 files") {
		t.Fatalf("browse note wrong: %q", note)
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
