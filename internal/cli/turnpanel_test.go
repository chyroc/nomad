package cli

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestPanelRowsTruncateAndCap(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	rows := panelRows(lines, "", 80, 24)
	if len(rows) != panelMaxHeight(24) {
		t.Fatalf("rows=%d want cap %d", len(rows), panelMaxHeight(24))
	}
	for _, r := range rows {
		if len([]rune(r)) > 78 {
			t.Fatalf("row not truncated: %d runes", len([]rune(r)))
		}
	}
	if rows[len(rows)-1] != lines[len(lines)-1][:78] {
		t.Fatalf("oldest rows should be dropped, newest kept")
	}
}

func TestPanelRowsSpinnerLast(t *testing.T) {
	rows := panelRows([]string{"a"}, "⠋ bash(ls)", 80, 24)
	if len(rows) != 2 || !strings.HasPrefix(rows[1], "⠋ bash") {
		t.Fatalf("spinner row missing: %v", rows)
	}
	if got := panelRows(nil, "", 80, 24); len(got) != 0 {
		t.Fatalf("empty panel should have no rows, got %v", got)
	}
}

func TestPanelLifecycleErasesOnFinish(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	p := &turnPanel{out: &lockedWriter{w: &buf, mu: &mu}, color: false}
	p.begin("Working…")
	p.push("● bash ✓ · ok")
	p.setSpinner("⠿ edit(f.go)")
	p.printAbove("● final answer\n")
	p.finish("✻ Worked for 5s · 1 tools")

	var out bytes.Buffer
	mu.Lock()
	out.Write(buf.Bytes())
	mu.Unlock()
	s := out.String()
	for _, want := range []string{"Working", "bash ✓", "final answer", "Worked for"} {
		if !strings.Contains(s, want) {
			t.Fatalf("panel output missing %q:\n%q", want, s)
		}
	}
	if n := strings.Count(s, "\x1b[J"); n < 2 {
		t.Fatalf("panel should erase on printAbove and finish, got %d clears:\n%q", n, s)
	}
}

func TestPanelPushWhileInactivePrintsDirectly(t *testing.T) {
	var buf bytes.Buffer
	p := newTurnPanel(&buf, false)
	p.push("plain line")
	if !strings.Contains(buf.String(), "plain line\n") {
		t.Fatalf("inactive push should print directly: %q", buf.String())
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("inactive push should not emit control sequences: %q", buf.String())
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}
