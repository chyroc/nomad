package cli

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/creack/pty"
)

func openRecordingPTY(t *testing.T) (*os.File, *os.File, error) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() {
		master.Close()
		slave.Close()
	})
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 40, Cols: 140}); err != nil {
		return nil, nil, err
	}
	return master, slave, nil
}

func startRecorder(master *os.File) *recordedBuffer {
	var rec recordedBuffer
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				rec.write(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()
	return &rec
}

func TestToolBlocksSettleAboveDock(t *testing.T) {
	master, slave, err := openRecordingPTY(t)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	rec := startRecorder(master)

	workspace := t.TempDir()
	a := &App{out: slave, color: false, bar: newStatusBar(), model: "doubao-x",
		paths: config.Paths{Home: workspace, DataDir: workspace, Workspace: workspace}}
	a.panel = newTurnPanel(slave, false)
	a.panel.setChrome(func(w int) []string {
		f := frameStub(w)
		return append([]string{f.top, "❯ "}, f.rows...)
	})
	a.startActivity("Working…")

	a.rememberToolCall(&loop.ToolCall{ID: "s1", Name: "web_search", Arguments: `{"query":"JEV"}`}, time.Now())
	a.finishTool(loop.Event{Kind: loop.EvToolResult, ToolName: "web_search",
		ToolCall: &loop.ToolCall{ID: "s1", Name: "web_search"}, Result: "hits", Time: time.Now().Add(2 * time.Second)})

	a.rememberToolCall(&loop.ToolCall{ID: "b1", Name: "bash", Arguments: `{"command":"go test"}`}, time.Now())
	a.finishTool(loop.Event{Kind: loop.EvToolResult, ToolName: "bash",
		ToolCall: &loop.ToolCall{ID: "b1", Name: "bash"}, Result: "n=1\nn=2\nn=3", Time: time.Now().Add(3 * time.Second)})

	a.panel.finish("Worked for 5s · 2 tools")
	time.Sleep(150 * time.Millisecond)

	grid := render(rec.string())
	flat := strings.Join(gridLines(grid), "\n")
	for _, want := range []string{
		`Web Search("JEV")`,
		"Did 1 search in 2s",
		"bash(go test)",
		"n=3",
		"Worked for 5s",
		"2 tools",
	} {
		if !strings.Contains(flat, want) {
			t.Fatalf("settled transcript missing %q:\n%s", want, grid)
		}
	}
	si := strings.Index(flat, `Web Search("JEV")`)
	bi := strings.Index(flat, "bash(go test)")
	ti := strings.Index(flat, "Worked for 5s")
	if !(si >= 0 && bi > si && ti > bi) {
		t.Fatalf("blocks out of order search=%d bash=%d summary=%d:\n%s", si, bi, ti, grid)
	}
}
