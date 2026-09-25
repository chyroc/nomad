package cli

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPanelRowsTruncateAndCap(t *testing.T) {
	lines := make([]string, 40)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	rows := panelRows(lines, "", 80, 24, 5)
	wantCap := 24 - 5 - 1
	if len(rows) != wantCap {
		t.Fatalf("rows=%d want cap %d", len(rows), wantCap)
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
	rows := panelRows([]string{"a"}, "⠋ bash(ls)", 80, 24, 5)
	if len(rows) != 2 || !strings.HasPrefix(rows[1], "⠋ bash") {
		t.Fatalf("spinner row missing: %v", rows)
	}
	if got := panelRows(nil, "", 80, 24, 5); len(got) != 0 {
		t.Fatalf("empty panel should have no rows, got %v", got)
	}
}

func TestPanelLifecycleErasesOnFinish(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	p := &turnPanel{out: &lockedWriter{w: &buf, mu: &mu}, color: false,
		chrome: []string{"TOP", "❯ ", "BOTTOM", "S1", "S2"}}
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
	for _, want := range []string{"Working", "bash ✓", "final answer", "Worked for", "BOTTOM", "S1"} {
		if !strings.Contains(s, want) {
			t.Fatalf("panel output missing %q:\n%q", want, s)
		}
	}
	if n := strings.Count(s, "\x1b[J"); n < 2 {
		t.Fatalf("panel should erase on printAbove and finish, got %d clears:\n%q", n, s)
	}
	afterFinish := strings.SplitAfterN(s, "Worked for 5s · 1 tools\n", 2)
	if len(afterFinish) == 2 && strings.Contains(afterFinish[1], "S1") {
		t.Fatalf("docked chrome must be erased on finish:\n%q", afterFinish[1])
	}
}

func TestPanelRedrawKeepsChromeBelowDynamicRows(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	p := &turnPanel{out: &lockedWriter{w: &buf, mu: &mu}, color: false,
		chrome: []string{"TOP", "❯ ", "BOTTOM", "S1", "S2"}}
	p.begin("Working…")
	p.push("● read ✓")
	p.push("● bash ✓")

	mu.Lock()
	s := buf.String()
	mu.Unlock()
	lastUp := strings.LastIndex(s, "\x1b[2K● read")
	frame := s
	if i := strings.LastIndex(s[:lastUp], "\x1b["); i >= 0 {
		frame = s[i:]
	}
	order := []string{"● read ✓", "● bash ✓", "TOP", "❯ ", "BOTTOM", "S1", "S2"}
	prev := -1
	for _, want := range order {
		idx := strings.Index(frame, want)
		if idx < 0 {
			t.Fatalf("final frame missing %q:\n%q", want, frame)
		}
		if idx <= prev {
			t.Fatalf("%q out of order in final frame:\n%q", want, frame)
		}
		prev = idx
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

func TestTurnEndLineVerbByToolCount(t *testing.T) {
	end := time.Date(2026, 9, 25, 9, 56, 0, 0, time.Local)
	think := turnEndLine(end, 4*time.Second, 0)
	if !strings.Contains(think, "Cogitated for 4s") || strings.Contains(think, "tools") {
		t.Fatalf("think-only turn line=%q", think)
	}
	worked := turnEndLine(end, 9*time.Second, 2)
	if !strings.Contains(worked, "Worked for 9s · 2 tools") {
		t.Fatalf("tool turn line=%q", worked)
	}
}
