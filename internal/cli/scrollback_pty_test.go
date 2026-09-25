package cli

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chyroc/nomad/internal/loop"
	"github.com/creack/pty"
)

// scrollEmu is a terminal emulator that models the behaviors the
// shared render grid omits: scrolling and both autowrap semantics.
// Writing a newline on the last row pushes the top row into history,
// exactly like a real terminal, so frame rows committed into
// scrollback become visible to tests. wrapImmediate models terminals
// that move to the next row as soon as the last cell is filled,
// rather than deferring the wrap until the next rune.
type scrollEmu struct {
	cols, rows    int
	wrapImmediate bool
	history       []string
	screen        [][]rune
	r, c          int
}

func newScrollEmu(rows, cols int) *scrollEmu {
	e := &scrollEmu{cols: cols, rows: rows, screen: make([][]rune, rows), r: 1, c: 1}
	for i := range e.screen {
		e.screen[i] = make([]rune, cols)
	}
	return e
}

func (e *scrollEmu) scroll() {
	e.history = append(e.history, string(e.screen[0]))
	for r := 1; r < e.rows; r++ {
		e.screen[r-1] = e.screen[r]
	}
	e.screen[e.rows-1] = make([]rune, e.cols)
}

func (e *scrollEmu) put(ch rune) {
	w := wideCond.RuneWidth(ch)
	if w < 1 {
		w = 1
	}
	if e.c+w-1 > e.cols {
		e.c = 1
		e.r++
		if e.r > e.rows {
			e.scroll()
			e.r = e.rows
		}
	}
	if ch == '	' {
		w = e.cols + 1 - e.c
		if pad := 8 - (e.c-1)%8; pad < w {
			w = pad
		}
		for k := 0; k < w && e.c-1+k < e.cols; k++ {
			e.screen[e.r-1][e.c-1+k] = ' '
		}
		e.c += w
		return
	}
	e.screen[e.r-1][e.c-1] = ch
	for k := 1; k < w && e.c-1+k < e.cols; k++ {
		e.screen[e.r-1][e.c-1+k] = 0
	}
	e.c += w
	if e.wrapImmediate && e.c > e.cols {
		e.c = 1
		e.r++
		if e.r > e.rows {
			e.scroll()
			e.r = e.rows
		}
	}
}

func (e *scrollEmu) feed(raw string) {
	for i := 0; i < len(raw); i++ {
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			j := i + 2
			for j < len(raw) && !(raw[j] >= '@' && raw[j] <= '~') {
				j++
			}
			if j >= len(raw) {
				return
			}
			e.csi(raw[i+2:j], raw[j])
			i = j
			continue
		}
		switch raw[i] {
		case '\r':
			e.c = 1
		case '\n':
			if e.r == e.rows {
				e.scroll()
			} else {
				e.r++
			}
		default:
			r, size := utf8.DecodeRuneInString(raw[i:])
			if r == utf8.RuneError && size <= 1 {
				continue
			}
			e.put(r)
			i += size - 1
		}
	}
}

func (e *scrollEmu) csi(body string, cmd byte) {
	n := func(def int) int {
		if body == "" {
			return def
		}
		v, err := strconv.Atoi(body)
		if err != nil || v <= 0 {
			return def
		}
		return v
	}
	mode := func(def int) int {
		if body == "" {
			return def
		}
		v, err := strconv.Atoi(body)
		if err != nil {
			return def
		}
		return v
	}
	switch cmd {
	case 'A':
		e.r -= n(1)
		if e.r < 1 {
			e.r = 1
		}
	case 'B':
		e.r += n(1)
		if e.r > e.rows {
			e.r = e.rows
		}
	case 'C':
		e.c += n(1)
		if e.c > e.cols {
			e.c = e.cols
		}
	case 'D':
		e.c -= n(1)
		if e.c < 1 {
			e.c = 1
		}
	case 'J':
		switch mode(0) {
		case 0:
			for c := e.c - 1; c < e.cols; c++ {
				e.screen[e.r-1][c] = 0
			}
			for r := e.r; r < e.rows; r++ {
				for c := range e.screen[r] {
					e.screen[r][c] = 0
				}
			}
		case 2:
			for r := range e.screen {
				for c := range e.screen[r] {
					e.screen[r][c] = 0
				}
			}
		}
	case 'K':
		switch mode(0) {
		case 0:
			for c := e.c - 1; c < e.cols; c++ {
				e.screen[e.r-1][c] = 0
			}
		case 2:
			for c := range e.screen[e.r-1] {
				e.screen[e.r-1][c] = 0
			}
		}
	}
}

func (e *scrollEmu) lines() []string {
	out := append([]string(nil), e.history...)
	for _, row := range e.screen {
		out = append(out, strings.TrimRight(string(row), "\x00"))
	}
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}

func (e *scrollEmu) count(sub string) int {
	n := 0
	for _, l := range e.lines() {
		n += strings.Count(l, sub)
	}
	return n
}

func waitForRecorder(rec *recordedBuffer, sub string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.string(), sub) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestNoFrameResidueWhenScreenScrolls drives a full turn on a screen
// shorter than the transcript: a streamed answer long enough to force
// scrolling, then a chain of server-side web tool blocks. The docked
// spinner frame must never be committed into scrollback and no
// transcript line may be lost or duplicated.
func TestNoFrameResidueWhenScreenScrolls(t *testing.T) {
	master, slave, err := openRecordingPTY(t)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 20, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	rec := startRecorder(master)
	termMu.Lock()
	termW, termH = 100, 20
	termMu.Unlock()
	t.Cleanup(func() {
		termMu.Lock()
		termW, termH = 80, 24
		termMu.Unlock()
	})

	a := &App{out: slave, color: false, bar: newStatusBar(), model: "m"}
	a.panel = newTurnPanel(slave, false)
	a.panel.setChrome(func(w int) []string {
		f := frameStub(w)
		return append([]string{f.top, "❯ "}, f.rows...)
	})
	a.startActivity("")

	t0 := time.Now()
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantThinking, Time: t0, Content: "weighing options"})
	for i := 1; i <= 30; i++ {
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantChunk, Time: t0,
			Content: fmt.Sprintf("answer-%02d", i) + "\n"})
	}
	for i := 0; i < 6; i++ {
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantThinking, Time: t0, Content: "refining"})
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvToolCall, Time: t0, ToolCall: &loop.ToolCall{
			Name:      "web_search",
			Arguments: `{"search_request_list":[{"query":"q` + strconv.Itoa(i) + `"}]}`,
		}})
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvToolResult, Time: t0.Add(time.Second),
			ToolName: "web_search", ToolCall: &loop.ToolCall{ID: "call_" + strconv.Itoa(i), Name: "web_search"},
			Result: `{"results":[]}`})
	}
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvTurnEnd, Time: t0.Add(20 * time.Second)})
	a.endTurnPanel(a.turnEndSummary)
	if !waitForRecorder(rec, "Worked for", 2*time.Second) {
		t.Fatalf("turn summary never reached the pty:\n%s", rec.string())
	}

	emu := newScrollEmu(20, 100)
	emu.feed(rec.string())

	for _, banned := range []string{"Inferring", "Working…", strings.Repeat("T", 20), strings.Repeat("B", 20), "STATUS1", "STATUS2", "❯"} {
		if n := emu.count(banned); n > 0 {
			t.Fatalf("frame leaked into the transcript %d time(s) with %q:\nHISTORY+SCREEN:\n%s\nRAW:\n%s",
				n, banned, strings.Join(emu.lines(), "\n"), rec.string())
		}
	}
	for _, want := range []string{"Thought for", "Worked for", "Did 1 search"} {
		if emu.count(want) == 0 {
			t.Fatalf("transcript missing %q:\n%s", want, strings.Join(emu.lines(), "\n"))
		}
	}
	if n := emu.count("Did 1 search"); n != 6 {
		t.Fatalf("want 6 search blocks, got %d:\n%s", n, strings.Join(emu.lines(), "\n"))
	}
	for i := 0; i < 6; i++ {
		if n := emu.count(fmt.Sprintf(`Web Search("q%d")`, i)); n != 1 {
			t.Fatalf(`Web Search("q%d") appears %d times, want 1:\n%s`, i, n, strings.Join(emu.lines(), "\n"))
		}
	}
	for i := 1; i <= 30; i++ {
		if n := emu.count(fmt.Sprintf("answer-%02d", i)); n != 1 {
			t.Fatalf("answer-%02d appears %d times, want exactly 1:\n%s", i, n, strings.Join(emu.lines(), "\n"))
		}
	}
}

func TestRuleAndStatusFitWideTerminals(t *testing.T) {
	a := &App{color: true, bar: newStatusBar(), model: "doubao-x"}
	f := a.renderInputFrame(90)
	if strings.ContainsRune(f.top, 0x2500) {
		t.Fatalf("frame rules must not use ambiguous-width runes:\n%q", f.top)
	}
	if w := displayWidth(f.top); w > 89 {
		t.Fatalf("rule is %d cells wide:\n%q", w, f.top)
	}
	line1, line2 := a.bar.render(a, 90)
	for _, l := range []string{line1, line2} {
		if w := displayWidth(l); w > 88 {
			t.Fatalf("status row is %d cells wide:\n%q", w, l)
		}
	}
}

func TestAmbiguousHeavyTurnKeepsFrameInsideScreen(t *testing.T) {
	master, slave, err := openRecordingPTY(t)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 16, Cols: 90}); err != nil {
		t.Fatal(err)
	}
	rec := startRecorder(master)
	termMu.Lock()
	termW, termH = 90, 16
	termMu.Unlock()
	t.Cleanup(func() {
		termMu.Lock()
		termW, termH = 80, 24
		termMu.Unlock()
	})
	a := &App{out: slave, color: true, bar: newStatusBar(), model: "m"}
	a.panel = newTurnPanel(slave, true)
	a.panel.setChrome(a.dockChromeRows)
	a.startActivity("")
	t0 := time.Now()
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantThinking, Time: t0, Content: "调研中"})
	for i := 1; i <= 18; i++ {
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvAssistantChunk, Time: t0,
			Content: "行·" + strings.Repeat("·", 44) + "\n"})
	}
	for i := 0; i < 4; i++ {
		a.renderInteractiveEvent(loop.Event{Kind: loop.EvToolResult, Time: t0.Add(time.Second),
			ToolName: "web_search", ToolCall: &loop.ToolCall{ID: "c" + strconv.Itoa(i), Name: "web_search"},
			Result: `{"results":[]}`})
	}
	a.renderInteractiveEvent(loop.Event{Kind: loop.EvTurnEnd, Time: t0.Add(20 * time.Second)})
	a.endTurnPanel(a.turnEndSummary)
	if !waitForRecorder(rec, "Worked for", 2*time.Second) {
		t.Fatalf("turn summary never reached the pty:\n%s", rec.string())
	}

	emu := newScrollEmu(16, 90)
	emu.feed(rec.string())
	for _, banned := range []string{"Inferring", "Working…", "STATUS"} {
		if n := emu.count(banned); n > 0 {
			t.Fatalf("%q leaked %d times on a CJK-wide 16-row terminal:\n%s\nRAW:\n%s",
				banned, n, strings.Join(emu.lines(), "\n"), rec.string())
		}
	}
	for i := 0; i < 4; i++ {
		if n := emu.count("Did 1 search"); n != 4 && i == 0 {
			t.Fatalf("want 4 search blocks, got %d:\n%s", n, strings.Join(emu.lines(), "\n"))
		}
	}
}

// TestProfileWriterPassesCursorSequencesThrough locks the byte
// contract between the panel and the terminal: the shared output
// writer must forward cursor movement, erase and mode sequences
// untouched. A color down sampler previously wrapped this writer and
// silently dropped every non-SGR sequence, so the panel's erases never
// reached the terminal and every frame it repainted leaked into
// scrollback.
func TestProfileWriterPassesCursorSequencesThrough(t *testing.T) {
	var buf bytes.Buffer
	pw := newProfileWriter(&buf)
	seqs := []string{
		"\x1b[6A\r\x1b[J",
		"● Web Search(\"q\")\r\n",
		"\r\n\r\n\r\n\r\n\r\n\r\n",
		"\x1b[6A",
		"\r\x1b[2K⠧ Inferring… (9s)\r\n\x1b[2K\r\n\x1b[2K────",
		"\x1b[?25l",
		"\x1b[?25h",
		"\x1b[6n",
	}
	var want strings.Builder
	for _, s := range seqs {
		if n, err := pw.Write([]byte(s)); err != nil || n != len(s) {
			t.Fatalf("write %q: n=%d err=%v", s, n, err)
		}
		want.WriteString(s)
	}
	if buf.String() != want.String() {
		t.Fatalf("writer must forward bytes untouched:\n got: %q\nwant: %q",
			strings.ReplaceAll(buf.String(), "\x1b", "E"),
			strings.ReplaceAll(want.String(), "\x1b", "E"))
	}
}
