package cli

import (
	"strings"
	"testing"
	"time"
)

func TestSpinnerStaysInDockAcrossManyBlocks(t *testing.T) {
	master, slave, err := openRecordingPTY(t)
	if err != nil {
		t.Skip(err)
	}
	rec := startRecorder(master)

	p := newTurnPanel(slave, true)
	p.setChrome(func(w int) []string {
		f := frameStub(w)
		return append([]string{f.top, "❯ "}, f.rows...)
	})
	p.begin("")
	p.setPhase(phaseWorking)
	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 6; i++ {
		var b strings.Builder
		b.WriteString("● bash\n  ⎿  ok\n")
		for j := 0; j < 14; j++ {
			b.WriteString("    output-line\n")
		}
		p.printAbove(b.String())
		time.Sleep(120 * time.Millisecond)
	}
	p.finish("")
	time.Sleep(200 * time.Millisecond)

	grid := render(rec.string())
	rows := gridLines(grid)
	ruleRows := 0
	spinnerRows := 0
	for _, r := range rows {
		tm := strings.TrimSpace(r)
		if strings.HasPrefix(tm, strings.Repeat("T", 20)) {
			ruleRows++
		}
		if strings.Contains(tm, "Working…") || strings.Contains(tm, "Inferring…") {
			spinnerRows++
		}
	}
	if ruleRows > 1 {
		t.Fatalf("dock rule appears %d times in history (should only be final):\n%s", ruleRows, grid)
	}
	if spinnerRows > 1 {
		t.Fatalf("spinner appears %d times in history:\n%s", spinnerRows, grid)
	}
}
