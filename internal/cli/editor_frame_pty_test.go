package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

type recordedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recordedBuffer) write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.Write(p)
}

func (r *recordedBuffer) string() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func TestEditorPinnedFrameDoesNotScroll(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 26, Cols: 120}); err != nil {
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
	ed.setFrame(func(width int) inputFrame {
		return inputFrame{
			top:  strings.Repeat("T", width),
			rows: []string{strings.Repeat("B", width), "STATUS1", "STATUS2"},
		}
	})
	got := make(chan string, 1)
	go func() {
		line, _ := ed.ReadLine("❯ ", readLineOptions{showTopRule: true})
		got <- line
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := master.WriteString("hi"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	grid := render(rec.string())
	rows := gridLines(grid)
	if !strings.HasPrefix(rows[0], "TTTT") {
		t.Fatalf("row 1 should be the pinned top rule:\n%s", grid)
	}
	if !strings.Contains(rows[1], "hi") {
		t.Fatalf("row 2 should be the input row:\n%s", grid)
	}
	if !strings.HasPrefix(strings.TrimSpace(rows[2]), "BBBB") {
		t.Fatalf("row 3 should be the bottom rule:\n%s", grid)
	}
	if strings.TrimSpace(rows[3]) != "STATUS1" || strings.TrimSpace(rows[4]) != "STATUS2" {
		t.Fatalf("rows 4-5 should be the pinned status rows:\n%s", grid)
	}

	repaints := strings.Count(rec.string(), "STATUS2")
	if _, err := master.WriteString("xyz"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	grid2 := render(rec.string())
	rows2 := gridLines(grid2)
	if strings.TrimSpace(rows2[3]) != "STATUS1" || strings.TrimSpace(rows2[4]) != "STATUS2" {
		t.Fatalf("status rows must stay pinned on the same rows after typing:\n%s", grid2)
	}
	if strings.Contains(rows2[5], "STATUS") {
		t.Fatalf("repaint must not scroll the status rows:\n%s", grid2)
	}
	if n := strings.Count(rec.string(), "STATUS2"); n < repaints {
		t.Fatalf("status content disappeared after repaint")
	}

	if _, err := master.WriteString("\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "hixyz" {
			t.Fatalf("ReadLine=%q want hixyz", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLine never returned after Enter")
	}
}

func gridLines(grid string) []string {
	var out []string
	for _, l := range strings.Split(grid, "\n") {
		if idx := strings.Index(l, "|"); idx >= 0 {
			out = append(out, l[idx+1:])
		}
	}
	return out
}

func TestEditorInterruptErasesPinnedFrame(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 26, Cols: 120}); err != nil {
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
	ed.setFrame(func(width int) inputFrame {
		return inputFrame{
			top:  strings.Repeat("T", width),
			rows: []string{strings.Repeat("B", width), "STATUS1", "STATUS2"},
		}
	})
	resErr := make(chan error, 1)
	go func() {
		_, err := ed.ReadLine("❯ ", readLineOptions{showTopRule: true})
		resErr <- err
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := master.WriteString("\x03"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resErr:
		if err != ErrInterrupt {
			t.Fatalf("err=%v want ErrInterrupt", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadLine never returned after Ctrl+C")
	}
	time.Sleep(200 * time.Millisecond)

	grid := render(rec.string())
	for _, l := range gridLines(grid) {
		if strings.Contains(l, "STATUS1") || strings.Contains(l, "STATUS2") ||
			strings.Contains(strings.TrimSpace(l), "TTTT") || strings.Contains(strings.TrimSpace(l), "BBBB") {
			t.Fatalf("pinned frame must be erased on interrupt:\n%s", grid)
		}
	}
}
