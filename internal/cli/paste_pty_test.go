package cli

import (
	"testing"
	"time"

	"github.com/creack/pty"
)

// pasteInRawEditor opens a pty, puts the editor in raw mode on the
// slave (as in real use) and returns a writer on the master side.
func pasteInRawEditor(t *testing.T, writes ...[]byte) string {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	ed := newLineEditor(slave, slave, nil, "")
	done := make(chan string, 1)
	go func() {
		line, _ := ed.ReadLine("> ", readLineOptions{})
		done <- line
	}()
	time.Sleep(200 * time.Millisecond)
	for _, w := range writes {
		if _, err := master.Write(w); err != nil {
			t.Fatal(err)
		}
		time.Sleep(80 * time.Millisecond)
	}
	select {
	case got := <-done:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLine never returned after paste + Enter")
		return ""
	}
}

// Real terminals deliver start marker + content + end marker in one
// write; the old drain swallowed the end marker as text and Enter was
// then ignored forever ("paste then Enter does nothing").
func TestPasteContentAndEndInSinglePacket(t *testing.T) {
	got := pasteInRawEditor(t,
		[]byte("\x1b[200~hello world\x1b[201~"),
		[]byte{'\r'},
	)
	if got != "hello world" {
		t.Fatalf("ReadLine=%q want hello world", got)
	}
}

// A trailing fragment of the end marker flushed with content must not
// be treated as text or lock the editor in paste mode.
func TestPasteEndMarkerSplitAcrossReads(t *testing.T) {
	got := pasteInRawEditor(t,
		[]byte("\x1b[200~line one\nline two\x1b[20"),
		[]byte("1~"),
		[]byte{'\r'},
	)
	if want := "line one\nline two"; got != want {
		t.Fatalf("ReadLine=%q want %q", got, want)
	}
}

// Multiple whole paste chunks accumulate before one Enter submits.
func TestPasteMultilineContent(t *testing.T) {
	got := pasteInRawEditor(t,
		[]byte("\x1b[200~first\nsecond\x1b[201~"),
		[]byte{'\r'},
	)
	if want := "first\nsecond"; got != want {
		t.Fatalf("ReadLine=%q want %q", got, want)
	}
}
