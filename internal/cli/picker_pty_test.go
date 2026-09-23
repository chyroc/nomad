package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

func reproItems() []pickItem {
	names := []string{
		"doubao-seed-2-0-lite", "doubao-seed-2-1-pro", "doubao-seed-2-1-pro",
		"deepseek-v4-pro", "glm-5-2", "doubao-seed-2-1-turbo",
		"doubao-seed-evolving", "deepseek-v4-flash-ga", "deepseek-v4-pro-ga",
		"glm-5-3-flash", "doubao-seed-2-1-lite", "deepseek-v4-1-flash",
	}
	items := make([]pickItem, 0, len(names))
	for i, n := range names {
		id := fmt.Sprintf("runtime-%d", i)
		items = append(items, pickItem{id: id, label: n, tag: "fmv-catalog-" + id})
	}
	return items
}

type emu struct {
	mu      sync.Mutex
	rec     bytes.Buffer
	pending []byte
	curRow  int
}

func (e *emu) pump(master *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := master.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			e.mu.Lock()
			e.rec.Write(data)
			e.pending = append(e.pending, data...)
			var reply []byte
			e.pending, reply = e.scan(e.pending)
			e.mu.Unlock()
			if reply != nil {
				master.Write(reply)
			}
		}
		if err != nil {
			return
		}
	}
}

func (e *emu) scan(data []byte) (rest, reply []byte) {
	for {
		idx := bytes.Index(data, []byte{0x1b, '[', '6', 'n'})
		if idx < 0 {
			return data, reply
		}
		reply = append(reply, []byte(fmt.Sprintf("\x1b[%d;1R", e.curRow))...)
		data = append(data[:idx], data[idx+4:]...)
	}
}

func (e *emu) snapshot() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.rec.String()
	e.rec.Reset()
	return s
}

const screenRows, screenCols = 26, 120

type grid struct {
	c   [screenRows][screenCols]rune
	r   int
	col int
}

func newGrid() *grid {
	g := &grid{r: 1, col: 1}
	for i := range g.c {
		for j := range g.c[i] {
			g.c[i][j] = ' '
		}
	}
	return g
}

func (g *grid) put(ch rune) {
	if g.r < 1 || g.r > screenRows || g.col < 1 || g.col > screenCols {
		return
	}
	g.c[g.r-1][g.col-1] = ch
	g.col++
}

func (g *grid) clearFromCursor(mode int) {
	switch mode {
	case 1:
		for r := 1; r <= g.r; r++ {
			end := screenCols
			if r == g.r {
				end = g.col
			}
			for c := 1; c <= end; c++ {
				g.c[r-1][c-1] = ' '
			}
		}
	case 2:
		for r := range g.c {
			for c := range g.c[r] {
				g.c[r][c] = ' '
			}
		}
	default:
		for r := g.r; r <= screenRows; r++ {
			start := 1
			if r == g.r {
				start = g.col
			}
			for c := start; c <= screenCols; c++ {
				g.c[r-1][c-1] = ' '
			}
		}
	}
}

func render(raw string) string {
	g := newGrid()
	for i := 0; i < len(raw); i++ {
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			j := i + 2
			for j < len(raw) {
				if raw[j] >= '@' && raw[j] <= '~' {
					break
				}
				j++
			}
			if j >= len(raw) {
				break
			}
			body, cmd := raw[i+2:j], raw[j]
			i = j
			read2 := func() (int, int) {
				ps := strings.Split(strings.TrimSuffix(body, string(cmd)), ";")
				a, b := 1, 1
				if len(ps) >= 1 && ps[0] != "" {
					fmt.Sscanf(ps[0], "%d", &a)
				}
				if len(ps) >= 2 && ps[1] != "" {
					fmt.Sscanf(ps[1], "%d", &b)
				}
				return a, b
			}
			switch cmd {
			case 'H', 'f':
				r, c := read2()
				g.r, g.col = r, c
			case 'A':
				n, _ := read2()
				g.r -= n
			case 'B':
				n, _ := read2()
				g.r += n
			case 'C':
				n, _ := read2()
				g.col += n
			case 'D':
				n, _ := read2()
				g.col -= n
			case 'J':
				mode := 0
				if len(body) > 1 {
					fmt.Sscanf(body[:len(body)-1], "%d", &mode)
				}
				g.clearFromCursor(mode)
			}
			continue
		}
		switch raw[i] {
		case '\r':
			g.col = 1
		case '\n':
			g.r++
		default:
			g.put(rune(raw[i]))
		}
	}
	var sb strings.Builder
	for r := 1; r <= screenRows; r++ {
		line := strings.TrimRight(string(g.c[r-1][:]), " ")
		fmt.Fprintf(&sb, "%2d|%s\n", r, line)
	}
	return sb.String()
}

func TestPtyReproPicker(t *testing.T) {
	cases := []int{5, 22}
	for _, anchor := range cases {
		t.Run(fmt.Sprintf("anchor-%d", anchor), func(t *testing.T) {
			runPickerCase(t, anchor)
		})
	}
}

func runPickerCase(t *testing.T, anchor int) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	e := &emu{curRow: anchor}
	go e.pump(master)

	items := reproItems()
	pk := newPickerFull(slave, slave, items, 8,
		"Select model",
		"Switch the model and thinking effort. Enter saves as default, s applies to this session.",
		[]string{"low", "medium", "high", "xhigh", "max"}, 4)

	result := make(chan pickResult, 1)
	okch := make(chan bool, 1)
	go func() {
		r, ok := pk.RunFull()
		result <- r
		okch <- ok
	}()

	time.Sleep(400 * time.Millisecond)
	fmt.Printf("=== DRAW 1 (anchor row %d, sel index 8) ===\n%s\n", anchor, render(e.snapshot()))

	master.Write([]byte{0x1b, '[', 'B'})
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("=== AFTER DOWN ===\n%s\n", render(e.snapshot()))

	master.Write([]byte{'z', 'z'})
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("=== AFTER FILTER 'zz' (no matches) ===\n%s\n", render(e.snapshot()))
	master.Write([]byte{127, 127})
	time.Sleep(200 * time.Millisecond)
	e.snapshot()

	master.Write([]byte{0x1b})
	time.Sleep(300 * time.Millisecond)
	select {
	case r := <-result:
		fmt.Printf("=== ERASED ===\n%s\n", render(e.snapshot()))
		t.Logf("esc result=%+v ok=%v", r, <-okch)
	case <-time.After(2 * time.Second):
		t.Fatal("picker did not close on Esc")
	}
}

func TestPtyMultiSelect(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	e := &emu{curRow: 5}
	go e.pump(master)

	items := []pickItem{
		{id: "a", label: "alpha", desc: "first skill"},
		{id: "b", label: "beta", desc: "second skill"},
		{id: "c", label: "gamma", desc: "third skill"},
	}
	pk := newPickerFull(slave, slave, items, 0, "Sync skills", "desc.", nil, 0).
		withMultiSelect(nil)
	resCh := make(chan pickResult, 1)
	okCh := make(chan bool, 1)
	go func() {
		r, ok := pk.RunFull()
		resCh <- r
		okCh <- ok
	}()

	time.Sleep(300 * time.Millisecond)
	master.Write([]byte{' '}) // check alpha
	time.Sleep(150 * time.Millisecond)
	master.Write([]byte{0x1b, '[', 'B'}) // move to beta
	time.Sleep(150 * time.Millisecond)
	master.Write([]byte{' '}) // check beta
	time.Sleep(150 * time.Millisecond)
	master.Write([]byte{' '}) // uncheck beta
	time.Sleep(150 * time.Millisecond)
	fmt.Printf("=== MULTI DRAW ===\n%s\n", render(e.snapshot()))
	master.Write([]byte{'\r'})
	select {
	case r := <-resCh:
		confirmed := <-okCh
		if !confirmed || len(r.ids) != 1 || r.ids[0] != "a" {
			t.Fatalf("expected only [a], got %+v ok=%v", r, confirmed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multi picker did not confirm")
	}
}
