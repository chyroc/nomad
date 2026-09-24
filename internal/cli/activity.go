package cli

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// activityLine is a single self-erasing status line in the style of
// Claude Code: a braille spinner plus a label while running
// ("⠋ Bash(sleep 1)"), replaced in place by a final static line
// ("⏺ Bash(...) …" / "✓ …" / "✗ …") when finished. Only one is shown
// at a time per App; it never scrolls while active.
type activityLine struct {
	out   io.Writer
	color bool

	mu       sync.Mutex
	active   bool
	label    string
	stop     chan struct{}
	done     chan struct{}
	rendered bool
}

var activityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func newActivityLine(out io.Writer, color bool) *activityLine {
	return &activityLine{out: out, color: color}
}

func (a *activityLine) Start(label string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active {
		a.label = label
		return
	}
	a.active = true
	a.label = label
	a.stop = make(chan struct{})
	a.done = make(chan struct{})
	if !a.color {
		a.rendered = false
		return
	}
	a.rendered = true
	go func() {
		defer close(a.done)
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-a.stop:
				return
			case <-t.C:
				frame := activityFrames[i%len(activityFrames)]
				i++
				a.mu.Lock()
				if a.active {
					io.WriteString(a.out, "\r\x1b[2K"+cDim+frame+cReset+" "+a.label)
				}
				a.mu.Unlock()
			}
		}
	}()
}

// SetLabel updates the running text in place.
func (a *activityLine) SetLabel(label string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.label = label
}

// EraseLine clears the live spinner line without ending the activity,
// so a static line can be printed and the activity restarted after.
func (a *activityLine) EraseLine() {
	a.mu.Lock()
	if a.active && a.rendered {
		io.WriteString(a.out, "\r\x1b[2K")
	}
	a.mu.Unlock()
}

// Finish erases the live line and prints a final static line, then
// leaves a blank line if spacer is true.
func (a *activityLine) Finish(final string, spacer bool) {
	a.mu.Lock()
	if !a.active {
		a.mu.Unlock()
		if final != "" {
			io.WriteString(a.out, final+"\n")
		}
		return
	}
	a.active = false
	stop, done, rendered := a.stop, a.done, a.rendered
	a.mu.Unlock()

	if rendered {
		io.WriteString(a.out, "\r\x1b[2K")
	}
	close(stop)
	if rendered {
		<-done
	}
	if final != "" {
		io.WriteString(a.out, final+"\n")
	}
	_ = spacer
}

// Finishf is a convenience.
func (a *activityLine) Finishf(format string, args ...interface{}) {
	a.Finish(fmt.Sprintf(format, args...), false)
}
