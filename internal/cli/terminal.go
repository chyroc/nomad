package cli

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/colorprofile"
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

// profileWriter downsamples SGR output to the terminal's color
// capability (NO_COLOR, TERM=dumb, non-TTY) while keeping Fd available
// for raw-mode and size calls. Writes are serialized so the spinner
// goroutine and event rendering never interleave a single write.
type profileWriter struct {
	mu *sync.Mutex
	*colorprofile.Writer
}

func (p *profileWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Writer.Write(b)
}

func (p *profileWriter) Fd() uintptr {
	if f, ok := p.Writer.Forward.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func newProfileWriter(out io.Writer) io.Writer {
	return &profileWriter{mu: &sync.Mutex{}, Writer: colorprofile.NewWriter(out, os.Environ())}
}

func fdOf(w io.Writer) int {
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		return int(f.Fd())
	}
	return -1
}
