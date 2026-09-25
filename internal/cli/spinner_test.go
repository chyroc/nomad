package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestPanelForSpinner() *turnPanel {
	return &turnPanel{out: ioDiscard{}, color: true, active: true, verb: "Inferring"}
}

func TestSpinnerRowThinkingElapsed(t *testing.T) {
	p := newTestPanelForSpinner()
	p.phase = phaseThinking
	p.turnStart = time.Now().Add(-6 * time.Second)
	p.frame = 0
	row := p.spinnerRow()
	plain := stripTestAnsi(row)
	if !strings.HasPrefix(plain, activityFrames[0]+" Inferring…") {
		t.Fatalf("thinking spinner wrong: %q", plain)
	}
	if !strings.Contains(plain, "(6s · thinking)") {
		t.Fatalf("thinking suffix wrong: %q", plain)
	}
}

func TestSpinnerRowWorkingWithTokens(t *testing.T) {
	p := newTestPanelForSpinner()
	p.phase = phaseWorking
	p.turnStart = time.Now().Add(-13 * time.Second)
	p.setOutputTokens(255)
	row := p.spinnerRow()
	plain := stripTestAnsi(row)
	if !strings.Contains(plain, "Working… (13s · ↓ 255 tokens)") {
		t.Fatalf("working/token spinner wrong: %q", plain)
	}
}

func TestSpinnerRowStaticLabelWins(t *testing.T) {
	p := newTestPanelForSpinner()
	p.phase = phaseThinking
	p.spinnerLabel = "⠿ Listing directory…"
	if got := stripTestAnsi(p.spinnerRow()); !strings.Contains(got, "Listing directory") {
		t.Fatalf("pinned label should win: %q", got)
	}
}

func TestSpinnerRowIdleEmpty(t *testing.T) {
	p := newTestPanelForSpinner()
	if p.spinnerRow() != "" {
		t.Fatal("idle phase should render no spinner")
	}
}

func stripTestAnsi(s string) string {
	for {
		i := strings.IndexByte(s, 0x1b)
		if i < 0 {
			return s
		}
		j := i + 1
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			return s[:i]
		}
		s = s[:i] + s[j+1:]
	}
}

func TestTickerAdvancesDynamicSpinner(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	p := &turnPanel{out: &lockedWriter{w: &buf, mu: &mu}, color: true, active: true, verb: "Working"}
	p.phase = phaseWorking
	p.turnStart = time.Now().Add(-time.Second)
	p.startTicker()
	defer p.stopTicker()
	time.Sleep(300 * time.Millisecond)
	p.mu.Lock()
	frame := p.frame
	p.mu.Unlock()
	if frame < 2 {
		t.Fatalf("ticker should advance the dynamic spinner frame, got %d", frame)
	}
}

func TestSpinnerEstimatesTokensFromChars(t *testing.T) {
	p := newTestPanelForSpinner()
	p.phase = phaseWorking
	p.turnStart = time.Now()
	p.addStreamedChars(800)
	row := stripTestAnsi(p.spinnerRow())
	if !strings.Contains(row, "↓ 200 tokens") {
		t.Fatalf("800 chars should estimate 200 tokens: %q", row)
	}
	p.setOutputTokens(1799)
	if got := stripTestAnsi(p.spinnerRow()); !strings.Contains(got, "↓ 1799 tokens") {
		t.Fatalf("real usage must replace estimate: %q", got)
	}
	p.addStreamedChars(99999)
	if got := stripTestAnsi(p.spinnerRow()); !strings.Contains(got, "↓ 1799 tokens") {
		t.Fatalf("streaming chars must not override real usage: %q", got)
	}
}

func TestSpinnerElapsedSurvivesPhaseSwitch(t *testing.T) {
	p := newTestPanelForSpinner()
	p.turnStart = time.Now().Add(-9 * time.Second)
	p.setPhase(phaseThinking)
	first := stripTestAnsi(p.spinnerRow())
	p.setPhase(phaseWorking)
	second := stripTestAnsi(p.spinnerRow())
	if !strings.Contains(first, "(9s") || !strings.Contains(second, "(9s") {
		t.Fatalf("elapsed must keep counting across phases:\nfirst=%s\nsecond=%s", first, second)
	}
}
