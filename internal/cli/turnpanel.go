package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var activityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// panelMaxHeight returns the row budget for the live region: the
// terminal height minus slack so the region never scrolls itself into
// the scrollback (where it could no longer be erased).
func panelMaxHeight(height int) int {
	max := height - 3
	if max < 1 {
		max = 1
	}
	return max
}

// panelRows truncates the panel's lines to the width and caps them to
// the height, dropping the oldest rows first; the spinner row is
// appended last when a label is set.
func panelRows(lines []string, spinner string, width, height int) []string {
	if width < 4 {
		width = 80
	}
	rows := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		rows = append(rows, truncateToWidth(l, width-2))
	}
	if spinner != "" {
		rows = append(rows, truncateToWidth(spinner, width-2))
	}
	if max := panelMaxHeight(height); len(rows) > max {
		rows = rows[len(rows)-max:]
	}
	return rows
}

// turnPanel is the live region a turn renders its progress into. Tool
// steps, thoughts and the spinner repaint in place while the turn
// runs; when the turn ends the whole region is erased so only the
// final answer and the summary line stay in the scrollback. Text that
// must survive (streamed answer chunks, errors) is printed above the
// region via printAbove. The region never enters the alternate screen.
type turnPanel struct {
	out   io.Writer
	color bool

	mu           sync.Mutex
	active       bool
	lines        []string
	spinnerLabel string
	frame        int
	renderedRows int
	stop         chan struct{}
}

func newTurnPanel(out io.Writer, color bool) *turnPanel {
	return &turnPanel{out: out, color: color}
}

// begin activates the panel for a new turn with the given spinner
// label; an already active panel only switches labels.
func (p *turnPanel) begin(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active {
		p.spinnerLabel = label
		p.redraw()
		return
	}
	p.active = true
	p.lines = nil
	p.spinnerLabel = label
	p.renderedRows = 0
	p.startTicker()
	p.redraw()
}

// setSpinner replaces the running label.
func (p *turnPanel) setSpinner(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spinnerLabel = label
	p.redraw()
}

// push appends one completed progress line to the region.
func (p *turnPanel) push(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active {
		io.WriteString(p.out, line+"\n")
		return
	}
	p.lines = append(p.lines, line)
	p.redraw()
}

// printAbove erases the region, writes permanent text and redraws the
// region below it. Inactive panels pass the text straight through.
func (p *turnPanel) printAbove(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active {
		io.WriteString(p.out, s)
		return
	}
	p.erase()
	if s != "" {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		io.WriteString(p.out, s)
	}
	p.redraw()
}

// suspend erases the region for an inline modal (permission picker)
// and stops the spinner; resume repaints it below the modal.
func (p *turnPanel) suspend() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopTicker()
	p.erase()
	p.active = false
}

func (p *turnPanel) resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	p.renderedRows = 0
	p.startTicker()
	p.redraw()
}

// finish erases the region and deactivates it, optionally printing a
// permanent final line where the region used to be.
func (p *turnPanel) finish(final string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopTicker()
	p.erase()
	p.lines = nil
	p.spinnerLabel = ""
	p.active = false
	if final != "" {
		io.WriteString(p.out, final+"\n")
	}
}

// erase clears the painted rows; callers must hold mu. After a redraw
// the cursor parks at column 1 of the region's last row, so moving up
// renderedRows-1 and clearing below removes the whole region.
func (p *turnPanel) erase() {
	if p.renderedRows > 1 {
		fmt.Fprintf(p.out, "\x1b[%dA", p.renderedRows-1)
	}
	io.WriteString(p.out, "\r\x1b[J")
	p.renderedRows = 0
}

// redraw repaints the region; callers must hold mu.
func (p *turnPanel) redraw() {
	rows := panelRows(p.lines, p.spinnerRow(), p.width(), p.height())
	var sb strings.Builder
	if p.renderedRows > 1 {
		fmt.Fprintf(&sb, "\x1b[%dA", p.renderedRows-1)
	}
	sb.WriteString("\r")
	for i, row := range rows {
		sb.WriteString("\x1b[2K")
		sb.WriteString(row)
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\x1b[J\r")
	io.WriteString(p.out, sb.String())
	p.renderedRows = len(rows)
}

func (p *turnPanel) spinnerRow() string {
	if p.spinnerLabel == "" {
		return ""
	}
	return activityFrames[p.frame%len(activityFrames)] + " " + p.spinnerLabel
}

func (p *turnPanel) width() int {
	if w, _ := cachedTermSize(); w > 0 {
		return w
	}
	return 80
}

func (p *turnPanel) height() int {
	if _, h := cachedTermSize(); h > 0 {
		return h
	}
	return 24
}

// startTicker animates the spinner frame; callers must hold mu.
func (p *turnPanel) startTicker() {
	if !p.color || p.stop != nil {
		return
	}
	p.stop = make(chan struct{})
	stop := p.stop
	go func() {
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				p.mu.Lock()
				if p.active && p.spinnerLabel != "" {
					p.frame++
					p.redraw()
				}
				p.mu.Unlock()
			}
		}
	}()
}

// stopTicker halts the animation goroutine; callers must hold mu.
func (p *turnPanel) stopTicker() {
	if p.stop != nil {
		close(p.stop)
		p.stop = nil
	}
}
