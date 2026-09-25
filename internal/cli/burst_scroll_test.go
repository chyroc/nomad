package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
	"github.com/creack/pty"
)

func driveBurstTurn(t *testing.T, rows, cols int, wrapImmediate bool) *scrollEmu {
	master, slave, err := openRecordingPTY(t)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
		t.Fatal(err)
	}
	rec := startRecorder(master)
	termMu.Lock()
	termW, termH = cols, rows
	termMu.Unlock()
	t.Cleanup(func() {
		termMu.Lock()
		termW, termH = 80, 24
		termMu.Unlock()
	})
	out := newProfileWriter(slave)
	a := &App{out: out, color: true, bar: newStatusBar(), model: "m"}
	a.panel = newTurnPanel(out, true)
	a.panel.setChrome(a.dockChromeRows)
	a.startActivity("")

	t0 := time.Now().Add(-60 * time.Second)
	th := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	ev := func(e loop.Event) { a.renderInteractiveEvent(e) }
	call := func(ms int, name, args string) {
		ev(loop.Event{Kind: loop.EvToolCall, Time: th(ms), ToolCall: &loop.ToolCall{Name: name, Arguments: args}})
	}
	result := func(ms int, id, name, res string) {
		ev(loop.Event{Kind: loop.EvToolResult, Time: th(ms),
			ToolName: name, ToolCall: &loop.ToolCall{ID: id, Name: name}, Result: res})
	}
	think := func(ms int) {
		ev(loop.Event{Kind: loop.EvAssistantThinking, Time: th(ms), Content: "refining the plan"})
	}
	usage := func(ms int, out int) {
		ev(loop.Event{Kind: loop.EvUsage, Time: th(ms), Usage: &loop.Usage{InputTokens: 100, OutputTokens: out}})
	}
	longBody := "col1\tcol2\t" + strings.Repeat("body line ", 9) + "\n"

	think(323)
	usage(324, 343)
	call(438, "grep", `{"pattern":"jev"}`)
	call(439, "bash", `{"command":"ls -la; git log --oneline -3"}`)
	call(439, "web_search", `{"search_request_list":[{"query":"jev 是什么"}]}`)
	result(488, "call_00_8r0m", "grep", "internal/cli/toolblock_test.go\n"+longBody)
	result(569, "call_01_xuea", "bash", "exit_code: 0\ntotal 88\n"+strings.Repeat(longBody, 8))
	result(569, "call_02_e2zl", "web_search", `{"results":[]}`)

	for burst := 0; burst < 5; burst++ {
		base := 8000 + burst*10000
		think(base)
		usage(base+1, 1029+burst*800)
		if burst == 0 {
			call(base+110, "grep", `{"path":"internal/cli"}`)
			call(base+110, "web_fetch", `{"fetch_request_list":[{"url":"https://typesafe.ai"},{"url":"https://arxiv.org/x"}]}`)
			call(base+110, "web_search", `{"search_request_list":[{"query":"TypeSafe AI Jev pricing"}]}`)
			result(base+175, "call_00_k5ih", "grep", "internal/cli/toolblock_test.go:84\tmatch\n"+longBody)
			result(base+175, "call_01_qo00", "web_fetch", strings.Repeat(longBody, 4))
			result(base+175, "call_02_7z0g", "web_search", `{"results":[]}`)
		} else {
			result(base, "call_00_v3ix", "web_fetch", strings.Repeat(longBody, 3))
			result(base+1, "call_01_als5", "web_fetch", strings.Repeat(longBody, 3))
			call(base+1, "web_fetch", `{"fetch_request_list":[{"url":"https://typesafe-jev.com/sitemap.xml"}]}`)
			call(base+1, "web_fetch", `{"fetch_request_list":[{"url":"https://developer.aliyun.com/article/1766084"}]}`)
		}
		time.Sleep(150 * time.Millisecond)
	}
	ev(loop.Event{Kind: loop.EvAssistantChunk, Time: th(60000), Content: "done"})
	ev(loop.Event{Kind: loop.EvTurnEnd, Time: th(61000), Usage: &loop.Usage{InputTokens: 100, OutputTokens: 4000}})
	a.endTurnPanel(a.turnEndSummary)
	if !waitForRecorder(rec, "Worked for", 3*time.Second) {
		t.Fatalf("summary never arrived:\n%s", rec.string())
	}
	emu := newScrollEmu(rows, cols)
	emu.wrapImmediate = wrapImmediate
	emu.feed(rec.string())
	return emu
}

func TestBurstEventStreamKeepsFrameInsideScreen(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		for _, geom := range [][2]int{{40, 169}, {30, 169}, {24, 169}, {50, 100}} {
			emu := driveBurstTurn(t, geom[0], geom[1], wrap)
			leaks := emu.count("Inferring") + emu.count("Working…") + emu.count("Searching files")
			if leaks > 0 {
				for i, l := range emu.lines() {
					if strings.Contains(l, "Inferring") || strings.Contains(l, "Working…") || strings.Contains(l, "Searching") {
						t.Errorf("geom %dx%d immediateWrap=%v leak line %d: %s", geom[0], geom[1], wrap, i, l)
					}
				}
			}
		}
	}
}
