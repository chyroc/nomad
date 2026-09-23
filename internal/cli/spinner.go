package cli

import (
	"io"
	"sync"
	"time"
)

// spinner is a terminal activity indicator ("⠋ thinking…"). It renders
// only while active and erases its line on Stop, so streamed content can
// take over the same line. No-op when the output is not a TTY.
type spinner struct {
	out   io.Writer
	label string

	mu       sync.Mutex
	active   bool
	rendered bool
	stop     chan struct{}
	done     chan struct{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newSpinner(out io.Writer, label string) *spinner {
	return &spinner{out: out, label: label}
}

// Start begins rendering. It is safe to call on an inactive spinner.
func (s *spinner) Start(color bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
		return
	}
	s.active = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	s.rendered = false

	if !color {
		close(s.done)
		return
	}
	s.rendered = true
	go func() {
		defer close(s.done)
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				frame := spinnerFrames[i%len(spinnerFrames)]
				i++
				s.mu.Lock()
				active := s.active
				s.mu.Unlock()
				if !active {
					return
				}
				io.WriteString(s.out, "\r\x1b[2K"+frame+" "+s.label)
			}
		}
	}()
}

// Stop erases the spinner line and waits for the renderer to exit.
func (s *spinner) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	stop, done := s.stop, s.done
	rendered := s.rendered
	s.mu.Unlock()

	if rendered {
		io.WriteString(s.out, "\r\x1b[2K")
	}
	close(stop)
	<-done
}
