package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func frameStub(width int) inputFrame {
	return inputFrame{
		top:  strings.Repeat("T", width),
		rows: []string{strings.Repeat("B", width), "STATUS1", "STATUS2"},
	}
}

func TestTurnDocksChromeAboveEditor(t *testing.T) {
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

	ed := newLineEditor(slave, slave, nil, "")
	ed.setFrame(frameStub)
	got := make(chan string, 1)
	go func() {
		line, _ := ed.ReadLine("❯ ", readLineOptions{showTopRule: true, blankContinues: true})
		got <- line
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := master.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	panel := newTurnPanel(slave, false)
	panel.setChrome(func(width int) []string {
		f := frameStub(width)
		return append([]string{f.top, "❯ "}, f.rows...)
	})

	if _, err := master.WriteString("\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "hello" {
			t.Fatalf("ReadLine=%q want hello", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLine never returned after Enter")
	}

	panel.begin("Working…")
	time.Sleep(300 * time.Millisecond)

	grid := render(rec.string())
	rows := gridLines(grid)
	echoRow, spinnerRow := -1, -1
	for i, r := range rows {
		switch {
		case strings.Contains(r, "hello") && echoRow < 0:
			echoRow = i
		case strings.Contains(r, "Working") && spinnerRow < 0:
			spinnerRow = i
		}
	}
	topRuleRow := -1
	if spinnerRow >= 0 {
		for i := spinnerRow + 1; i < len(rows); i++ {
			if strings.HasPrefix(strings.TrimSpace(rows[i]), "TTTT") {
				topRuleRow = i
				break
			}
		}
	}
	if echoRow < 0 || spinnerRow < 0 || topRuleRow < 0 {
		t.Fatalf("missing rows echo=%d spinner=%d rule=%d:\n%s", echoRow, spinnerRow, topRuleRow, grid)
	}
	if !(echoRow < spinnerRow && spinnerRow < topRuleRow) {
		t.Fatalf("expected echo above spinner above pinned chrome (echo=%d spinner=%d rule=%d):\n%s",
			echoRow, spinnerRow, topRuleRow, grid)
	}
	if strings.TrimSpace(rows[topRuleRow+1]) == "" ||
		!strings.HasPrefix(strings.TrimSpace(rows[topRuleRow+2]), "BBBB") ||
		strings.TrimSpace(rows[topRuleRow+3]) != "STATUS1" ||
		strings.TrimSpace(rows[topRuleRow+4]) != "STATUS2" {
		t.Fatalf("pinned chrome malformed:\n%s", grid)
	}

	panel.finish("Worked for 1s")
	time.Sleep(200 * time.Millisecond)
	grid2 := render(rec.string())
	rows2 := gridLines(grid2)
	summaryFound := false
	for _, r := range rows2 {
		if strings.Contains(r, "Worked for 1s") {
			summaryFound = true
		}
	}
	if !summaryFound {
		t.Fatalf("summary line missing after finish:\n%s", grid2)
	}
	lastStatus := -1
	for i, r := range rows2 {
		if strings.TrimSpace(r) == "STATUS2" {
			lastStatus = i
		}
	}
	summaryRow := -1
	for i, r := range rows2 {
		if strings.Contains(r, "Worked for 1s") {
			summaryRow = i
		}
	}
	if lastStatus >= summaryRow {
		t.Fatalf("docked chrome must be erased above the summary line:\n%s", grid2)
	}
}
