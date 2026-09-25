package cli

import (
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

var (
	termMu        sync.RWMutex
	termW, termH  = 80, 24
	sizeWatchOnce sync.Once

	resizeSubsMu sync.Mutex
	resizeSubs   []chan struct{}
)

func initTermSize(fd int) {
	if w, h, err := term.GetSize(fd); err == nil && w > 0 && h > 0 {
		termMu.Lock()
		changed := w != termW || h != termH
		termW, termH = w, h
		termMu.Unlock()
		resizeSubsMu.Lock()
		subs := append([]chan struct{}(nil), resizeSubs...)
		resizeSubsMu.Unlock()
		if changed {
			for _, ch := range subs {
				select {
				case ch <- struct{}{}:
				default:
				}
			}
		}
	}
}

func cachedTermSize() (int, int) {
	termMu.RLock()
	defer termMu.RUnlock()
	return termW, termH
}

// subscribeResize returns a non-blocking channel signaled after a
// terminal size change. cancel unregisters it.
func subscribeResize() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	resizeSubsMu.Lock()
	resizeSubs = append(resizeSubs, ch)
	resizeSubsMu.Unlock()
	return ch, func() {
		resizeSubsMu.Lock()
		for i, s := range resizeSubs {
			if s == ch {
				resizeSubs = append(resizeSubs[:i], resizeSubs[i+1:]...)
				break
			}
		}
		resizeSubsMu.Unlock()
	}
}

func startSizeWatcher(fd int) {
	sizeWatchOnce.Do(func() {
		initTermSize(fd)
		if fd < 0 {
			return
		}
		ch := make(chan os.Signal, 8)
		signal.Notify(ch, syscall.SIGWINCH)
		go func() {
			for range ch {
				initTermSize(fd)
			}
		}()
	})
}

// resizeMsg is relayed into bubbletea programs so their models can
// re-read the cached terminal size when the program output is not a
// *os.File (and therefore gets no tea.WindowSizeMsg).
type resizeMsg struct{}

// relayResize forwards terminal size changes to a running program via
// Send. The returned func stops the relay; Send after program exit is
// a no-op.
func relayResize(p *tea.Program) func() {
	ch, cancel := subscribeResize()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ch:
				p.Send(resizeMsg{})
			case <-done:
				return
			}
		}
	}()
	return func() {
		close(done)
		cancel()
	}
}

// queryCursorRow asks the terminal for its cursor row over the DSR
// escape sequence, switching the input to raw mode for the bounded
// read. The reply never contains a newline, so canonical mode would
// swallow it. A late, malformed or missing reply reports ok=false.
func queryCursorRow(in io.Reader, out io.Writer) (int, bool) {
	f, ok := in.(*os.File)
	if !ok {
		return 0, false
	}
	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return 0, false
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false
	}
	defer term.Restore(fd, old)
	io.WriteString(out, "\x1b[6n")
	var deadliner interface {
		SetReadDeadline(time.Time) error
	} = f
	deadline := time.Now().Add(200 * time.Millisecond)
	buf := make([]byte, 0, 16)
	b := make([]byte, 1)
	for len(buf) < 16 {
		if deadliner != nil {
			_ = deadliner.SetReadDeadline(deadline)
		}
		n, err := f.Read(b)
		if deadliner != nil {
			_ = deadliner.SetReadDeadline(time.Time{})
		}
		if err != nil || n == 0 {
			return 0, false
		}
		buf = append(buf, b[0])
		if b[0] == 'R' {
			parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(string(buf), "\x1b["), "R"), ";")
			if len(parts) == 2 {
				if n, err := strconv.Atoi(parts[0]); err == nil && n > 0 {
					return n, true
				}
			}
			return 0, false
		}
		if time.Now().After(deadline) {
			return 0, false
		}
	}
	return 0, false
}

// wideCond counts East Asian ambiguous runes as two cells for the
// conservative row accounting: overestimating a line only docks the
// frame one row early, underestimating leaks painted frames into
// scrollback. Structural rows such as the frame rules use ASCII only,
// because terminal emulators disagree on ambiguous rune widths — some
// even report one width over DSR while rendering another.
var wideCond = runewidth.Condition{EastAsianWidth: true}

// displayWidth returns the cell width of a line counting East Asian
// ambiguous runes as two cells, the conservative interpretation that
// CJK terminals and fallback-font rendering apply. Row accounting
// must never undercount a line: an overestimate only docks the frame
// one row early, an underestimate leaks painted frames into
// scrollback.
func displayWidth(s string) int {
	w := wideCond.StringWidth(ansi.Strip(s))
	w += 8 * strings.Count(s, "\t")
	return w
}

// truncateDisplayWidth cuts a styled line to the given cell width so
// ambiguous runes cannot push the row past the budget.
func truncateDisplayWidth(s string, w int) string {
	cond := &wideCond
	var b strings.Builder
	cur := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		rw := cond.RuneWidth(r)
		if cur+rw > w {
			return b.String() + cReset
		}
		cur += rw
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

// expandTabsDisplay replaces tab characters with the spaces a terminal
// would render, advancing to the next multiple-of-eight column counted
// with the conservative cell width. Every committed line flows through
// here, so the panel's row accounting and the terminal see the exact
// same bytes; a raw tab otherwise costs the terminal up to eight cells
// while the accounting charges none, and every wrapped line leaks the
// docked frame into scrollback.
func expandTabsDisplay(s string) string {
	if !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	col := 0
	i := 0
	for i < len(s) {
		switch c := s[i]; c {
		case '\t':
			pad := 8 - col%8
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
			i++
		case '\n':
			b.WriteByte('\n')
			col = 0
			i++
		case 0x1b:
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && !(s[j] >= '@' && s[j] <= '~') {
					j++
				}
				if j < len(s) {
					j++
				}
				b.WriteString(s[i:j])
				i = j
				continue
			}
			b.WriteByte(c)
			i++
		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			w := wideCond.RuneWidth(r)
			if w < 1 {
				w = 1
			}
			b.WriteString(s[i : i+size])
			col += w
			i += size
		}
	}
	return b.String()
}

// profileWriter serializes writes so the spinner goroutine and event
// rendering never interleave a single write, while keeping Fd
// available for raw-mode and size calls. It passes bytes through
// untouched: the colorprofile down sampler it used to wrap rewrites
// the stream and silently drops non-SGR escape sequences such as
// cursor-up and erase-below, which are the panel's lifeblood. Color
// emission is already gated at every call site by App.color, which
// reflects the terminal's capability.
type profileWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (p *profileWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w.Write(b)
}

func (p *profileWriter) Fd() uintptr {
	if f, ok := p.w.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func newProfileWriter(out io.Writer) io.Writer {
	return &profileWriter{mu: &sync.Mutex{}, w: out}
}

func fdOf(w io.Writer) int {
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		return int(f.Fd())
	}
	return -1
}
