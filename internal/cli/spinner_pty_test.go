package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestSpinnerShowsLivePhaseAndSeconds(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skip(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	var rec recordedBuffer
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := master.Read(b)
			if n > 0 {
				rec.write(append([]byte(nil), b[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()

	p := newTurnPanel(slave, true)
	p.setChrome(func(w int) []string { return []string{"TOP", "❯ ", "BOTTOM", "S1", "S2"} })
	p.begin("")
	p.setPhase(phaseThinking)
	time.Sleep(1300 * time.Millisecond)
	p.setOutputTokens(255)
	p.setPhase(phaseWorking)
	time.Sleep(300 * time.Millisecond)
	p.finish("")
	slave.Write([]byte("\n"))
	time.Sleep(100 * time.Millisecond)

	raw := rec.string()
	if !strings.Contains(raw, "Inferring") {
		t.Fatalf("missing Inferring spinner:\n%s", strings.ReplaceAll(raw, "\x1b", "ESC")[:min(len(raw), 2000)])
	}
	if !strings.Contains(raw, "thinking)") {
		t.Fatalf("missing (Ns · thinking) suffix")
	}
	if !strings.Contains(raw, "↓ 255 tokens") {
		t.Fatalf("missing output token suffix")
	}
	if !strings.Contains(raw, "Working…") {
		t.Fatalf("missing Working phase")
	}
}
