package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestFrameStaysDockedThroughEndTurnGrace(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 30, Cols: 120}); err != nil {
		t.Fatal(err)
	}
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

	chrome := func(width int) []string {
		f := frameStub(width)
		return append([]string{f.top, "❯ "}, f.rows...)
	}

	ed := newLineEditor(slave, slave, nil, "")
	ed.setFrame(frameStub)
	got := make(chan string, 1)
	go func() {
		line, _ := ed.ReadLine("❯ ", readLineOptions{showTopRule: true, blankContinues: true})
		got <- line
	}()
	time.Sleep(250 * time.Millisecond)
	master.WriteString("你好")
	time.Sleep(150 * time.Millisecond)
	master.WriteString("\r")
	if line := <-got; line != "你好" {
		t.Fatalf("input=%q", line)
	}

	panel := newTurnPanel(slave, true)
	panel.setChrome(chrome)
	panel.begin("Working…")
	time.Sleep(150 * time.Millisecond)
	panel.push("● read ✓")
	time.Sleep(200 * time.Millisecond)
	panel.printAbove("● answer line\n")
	time.Sleep(150 * time.Millisecond)

	panel.collapseDynamic()
	time.Sleep(400 * time.Millisecond)
	assertFrameVisible(t, render(rec.string()))

	panel.finish("Cogitated for 1s · done 10:11 AM")

	ed2 := newLineEditor(slave, slave, nil, "")
	ed2.setFrame(frameStub)
	go ed2.ReadLine("❯ ", readLineOptions{showTopRule: true, blankContinues: true})
	time.Sleep(400 * time.Millisecond)

	grid := render(rec.string())
	assertFrameVisible(t, grid)
	rows := gridLines(grid)
	foundSummary := false
	for _, r := range rows {
		if strings.Contains(r, "Cogitated for 1s") {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("summary line missing:\n%s", grid)
	}
}

func assertFrameVisible(t *testing.T, grid string) {
	t.Helper()
	hasRule, hasStatus1, hasStatus2 := false, false, false
	for _, r := range gridLines(grid) {
		trimmed := strings.TrimSpace(r)
		if strings.HasPrefix(trimmed, strings.Repeat("T", 20)) {
			hasRule = true
		}
		if trimmed == "STATUS1" {
			hasStatus1 = true
		}
		if trimmed == "STATUS2" {
			hasStatus2 = true
		}
	}
	if !hasRule || !hasStatus1 || !hasStatus2 {
		t.Fatalf("input frame missing:\n%s", grid)
	}
}
