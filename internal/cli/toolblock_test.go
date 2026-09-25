package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
)

func TestRunningLabel(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"ls", `{}`, "Listing directory"},
		{"glob", `{"path":"src"}`, "Listing src"},
		{"grep", `{"pattern":"x"}`, "Searching files"},
		{"web_search", `{"query":"q"}`, "Searching the web"},
		{"web_fetch", `{"url":"u"}`, "Fetching page"},
		{"bash", `{"command":"go test"}`, "bash"},
	}
	for _, c := range cases {
		got := runningLabel(c.name, c.args)
		if !strings.Contains(got, c.want) {
			t.Errorf("runningLabel(%s)=%q want contains %q", c.name, got, c.want)
		}
	}
}

func TestBrowseSummary(t *testing.T) {
	if got := browseSummary("glob", "a.go\nb.go\nc.go"); got != "found 3 files" {
		t.Fatalf("glob summary=%q", got)
	}
	if got := browseSummary("grep", "one\ntwo"); got != "found 2 matches" {
		t.Fatalf("grep summary=%q", got)
	}
	if got := browseSummary("ls", "a/\nb/"); got != "listed 2 directories" {
		t.Fatalf("ls summary=%q", got)
	}
}

func TestWriteSummary(t *testing.T) {
	if got := writeSummary("write", "x", 3, 0); got != "Wrote 3 lines to x" {
		t.Fatalf("write=%q", got)
	}
	if got := writeSummary("edit", "x", 2, 1); got != "Added 2 lines, removed 1 lines" {
		t.Fatalf("edit=%q", got)
	}
}

func TestWebToolKindAndArgs(t *testing.T) {
	cases := []struct {
		name, args, wantHeader string
	}{
		{"web_search", `{"query":"什么是 JEV"}`, `Web Search("什么是 JEV")`},
		{"WebSearch", `{"input":"q1"}`, `Web Search("q1")`},
		{"mcp__web__web_search", `{"arguments":{"keyword":"k"}}`, `Web Search("k")`},
		{"web_fetch", `{"url":"https://x/y"}`, `Fetch(https://x/y)`},
		{"mcp__web__webfetch", `{"input":{"link":"https://z"}}`, `Fetch(https://z)`},
		{"web_search", `{"max_results":8,"search_request_list":[{"query":"JEV 是什么"},{"query":"JEV virus"}]}`, `Web Search("JEV 是什么")`},
		{"web_fetch", `{"fetch_request_list":[{"url":"https://x/y"},{"url":"https://x/z"}]}`, `Fetch(https://x/y)`},
	}
	a := &App{color: false}
	for _, c := range cases {
		got := stripTestAnsi(a.toolHeader(c.name, c.args, 100))
		if !strings.Contains(got, c.wantHeader) {
			t.Errorf("%s header=%q want contains %q", c.name, got, c.wantHeader)
		}
	}
}

func TestWebArgEmptyFallback(t *testing.T) {
	if got := webArgString(`{"weird":"x","q":"real"}`, "query", "q"); got != "real" {
		t.Fatalf("key fallback=%q", got)
	}
	if got := webArgString(`{"foo":"bar"}`, "query", "q"); got != "bar" {
		t.Fatalf("any-string fallback=%q", got)
	}
}

func TestTakeToolCallMatchesByIDThenName(t *testing.T) {
	a := &App{}
	t0 := time.Now()
	a.rememberToolCall(&loop.ToolCall{ID: "b1", Name: "bash", Arguments: `{"command":"ls"}`}, t0)
	a.rememberToolCall(&loop.ToolCall{Name: "web_search", Arguments: `{"search_request_list":[{"query":"jev 是什么"}]}`}, t0)
	a.rememberToolCall(&loop.ToolCall{Name: "web_fetch", Arguments: `{"fetch_request_list":[{"url":"https://typesafe.ai/"}]}`}, t0)
	a.rememberToolCall(&loop.ToolCall{Name: "web_search", Arguments: `{"search_request_list":[{"query":"typesafe"}]}`}, t0)

	if got := a.takeToolCall("b1", "bash"); got.args != `{"command":"ls"}` {
		t.Fatalf("id match failed: %+v", got)
	}
	if got := a.takeToolCall("call_02_real", "web_search"); got.args != `{"search_request_list":[{"query":"jev 是什么"}]}` {
		t.Fatalf("name fallback must take the oldest pending web_search: %+v", got)
	}
	if got := a.takeToolCall("call_00_real", "web_fetch"); got.args == "" {
		t.Fatalf("name fallback failed for web_fetch")
	}
	if got := a.takeToolCall("call_01_real", "web_search"); got.args != `{"search_request_list":[{"query":"typesafe"}]}` {
		t.Fatalf("second web_search should be consumed last: %+v", got)
	}
	if got := a.takeToolCall("call_99", "bash"); got.name != "" || got.args != "" {
		t.Fatalf("exhausted queue must return empty: %+v", got)
	}
}

func TestServerWebToolBlockKeepsArgsAcrossEmptyCallID(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	t0 := time.Now()
	a.rememberToolCall(&loop.ToolCall{Name: "web_search",
		Arguments: `{"max_results":8,"search_request_list":[{"query":"JEV 是什么"},{"query":"JEV virus"}]}`}, t0)
	a.rememberToolCall(&loop.ToolCall{Name: "web_fetch",
		Arguments: `{"fetch_request_list":[{"url":"https://openrouter.ai/blog/insights/what-is-jev/"}]}`}, t0)
	a.finishTool(loop.Event{Kind: loop.EvToolResult, Time: t0.Add(time.Second),
		ToolName: "web_search", ToolCall: &loop.ToolCall{ID: "call_02_eq3lbyg9akp43yhxo4pp4rwm", Name: "web_search"},
		Result: `{"results":[]}`})
	a.finishTool(loop.Event{Kind: loop.EvToolResult, Time: t0.Add(2 * time.Second),
		ToolName: "web_fetch", ToolCall: &loop.ToolCall{ID: "call_00_q1ixe96z0shr4iacfwkdvk8n", Name: "web_fetch"},
		Result: "page body"})
	out := stripTestAnsi(buf.String())
	for _, want := range []string{`Web Search("JEV 是什么")`, "Did 1 search in 1s", `Fetch(https://openrouter.ai/blog/insights/what-is-jev/)`, "Fetched in 2s"} {
		if !strings.Contains(out, want) {
			t.Errorf("block missing %q:\n%s", want, out)
		}
	}
}

func TestBashBlockDedupesExitCode(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	a.rememberToolCall(&loop.ToolCall{ID: "b1", Name: "bash", Arguments: `{}`}, time.Now())
	a.finishTool(loop.Event{Kind: loop.EvToolResult,
		ToolName: "bash", ToolCall: &loop.ToolCall{ID: "b1", Name: "bash"}, Result: "exit_code: 0\n"})
	out := stripTestAnsi(buf.String())
	if n := strings.Count(out, "exit_code: 0"); n > 1 {
		t.Fatalf("exit_code rendered %d times:\n%s", n, out)
	}

	buf.Reset()
	a.rememberToolCall(&loop.ToolCall{ID: "b2", Name: "bash", Arguments: "{}"}, time.Now())
	a.finishTool(loop.Event{Kind: loop.EvToolResult,
		ToolName: "bash", ToolCall: &loop.ToolCall{ID: "b2", Name: "bash"},
		Result: "exit_code: 0\ntotal 88\ndrwxr-xr-x 2 x x 4096 Sep 25 .\n"})
	out = stripTestAnsi(buf.String())
	if strings.Contains(out, "exit_code") {
		t.Fatalf("zero exit code metadata should not render:\n%s", out)
	}
	if !strings.Contains(out, "total 88") {
		t.Fatalf("body should preview after the metadata line:\n%s", out)
	}
}

func TestTextRows(t *testing.T) {
	cases := []struct {
		line  string
		width int
		want  int
	}{
		{"", 80, 1},
		{"abc", 80, 1},
		{strings.Repeat("a", 79), 80, 1},
		{strings.Repeat("a", 80), 80, 2},
		{strings.Repeat("a", 160), 80, 3},
		{strings.Repeat("a", 161), 80, 3},
	}
	for _, c := range cases {
		if got := textRows(c.line, c.width); got != c.want {
			t.Errorf("textRows(%q, %d)=%d want %d", c.line[:min(len(c.line), 12)], c.width, got, c.want)
		}
	}
}

func TestFinishToolDefersResultBeforeCall(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	t0 := time.Now()

	a.finishTool(loop.Event{Kind: loop.EvToolResult, Time: t0,
		ToolName: "web_fetch", ToolCall: &loop.ToolCall{ID: "call_00_tiraz", Name: "web_fetch"},
		Result: "page"})
	if out := buf.String(); strings.Contains(out, "Fetch") {
		t.Fatalf("result must not render before its call arrives:\n%s", out)
	}

	a.rememberToolCall(&loop.ToolCall{Name: "web_fetch",
		Arguments: `{"fetch_request_list":[{"url":"https://typesafe.ai/"},{"url":"https://arxiv.org/pdf/2609.26550"}]}`}, t0)
	out := stripTestAnsi(buf.String())
	for _, want := range []string{`Fetch(https://typesafe.ai/)`, "Fetched in 0s"} {
		if !strings.Contains(out, want) {
			t.Errorf("deferred block missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `Fetch()`) {
		t.Errorf("empty argument parens must not render:\n%s", out)
	}
}

func TestFlushDeferredToolResultsAtTurnEnd(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false}
	t0 := time.Now()
	a.finishTool(loop.Event{Kind: loop.EvToolResult, Time: t0,
		ToolName: "web_search", ToolCall: &loop.ToolCall{ID: "call_01_dqmv", Name: "web_search"},
		Result: `{"results":[]}`})
	if strings.Contains(buf.String(), "Web Search") {
		t.Fatalf("premature render:\n%s", buf.String())
	}
	a.flushDeferredToolResults()
	out := stripTestAnsi(buf.String())
	if !strings.Contains(out, "● Web Search\n") {
		t.Fatalf("flushed block should carry a bare title:\n%s", out)
	}
	if strings.Contains(out, "Web Search(") {
		t.Fatalf("no argument means no parens:\n%s", out)
	}
}

func TestTextRowsCountsAmbiguousRunesWide(t *testing.T) {
	if got := textRows(strings.Repeat("·", 44), 90); got != 1 {
		t.Errorf("44 middle dots at width 90 stay one row, got %d", got)
	}
	if got := textRows(strings.Repeat("·", 45), 90); got != 2 {
		t.Errorf("45 middle dots fill width 90 exactly and must count two rows, got %d", got)
	}
	if got := displayWidth("↓ 2434"); got != 7 {
		t.Errorf("displayWidth(↓ 2434)=%d want 7", got)
	}
	if got := truncateDisplayWidth(cDim+strings.Repeat("·", 50)+cReset, 90); displayWidth(got) > 90 {
		t.Errorf("truncateDisplayWidth overshot: %d", displayWidth(got))
	}
}

func TestBrowseSpinnerLabelHasSingleGlyph(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, color: false, bar: newStatusBar(), model: "m"}
	a.panel = newTurnPanel(&buf, false)
	a.startActivity("")
	a.startToolSpinner("grep", `{"pattern":"jev"}`)
	raw := buf.String()
	if n := strings.Count(raw, "⠿"); n > 0 {
		t.Fatalf("pinned label must not carry its own glyph:\n%s", strings.ReplaceAll(raw, "\x1b", "E"))
	}
	if !strings.Contains(raw, "Searching files…") {
		t.Fatalf("pinned label missing:\n%s", strings.ReplaceAll(raw, "\x1b", "E"))
	}
}

func TestToolBlockFoldMatchesReference(t *testing.T) {
	cases := []struct {
		result string
		want   []string
		banned []string
	}{
		{result: "n=1\nn=2\nn=3", want: []string{"⎿  n=1", "n=2", "n=3"}, banned: []string{"lines (ctrl+o"}},
		{result: "n=1\nn=2\nn=3\nn=4", want: []string{"n=4"}, banned: []string{"lines (ctrl+o"}},
		{result: "n=1\nn=2\nn=3\nn=4\nn=5", want: []string{"… +2 lines (ctrl+o to expand)"}, banned: []string{"n=4"}},
		{result: "n=1\nn=2\nn=3\nn=4\nn=5\nn=6\nn=7\nn=8\nn=9\nn=10\nn=11\nn=12\nn=13\nn=14\nn=15\nn=16",
			want: []string{"… +13 lines (ctrl+o to expand)"}, banned: []string{"n=4"}},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		a := &App{out: &buf, color: false}
		a.rememberToolCall(&loop.ToolCall{ID: "b1", Name: "bash", Arguments: "{}"}, time.Now())
		a.finishTool(loop.Event{Kind: loop.EvToolResult,
			ToolName: "bash", ToolCall: &loop.ToolCall{ID: "b1", Name: "bash"}, Result: c.result})
		out := stripTestAnsi(buf.String())
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("result %d lines: missing %q:\n%s", strings.Count(c.result, "\n")+1, w, out)
			}
		}
		for _, b := range c.banned {
			if strings.Contains(out, b) {
				t.Errorf("result %d lines: %q must not render:\n%s", strings.Count(c.result, "\n")+1, b, out)
			}
		}
	}
}
