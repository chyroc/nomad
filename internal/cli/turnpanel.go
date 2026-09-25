package cli

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

var activityFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinner verbs, matching the reference agent's whimsical progress
// vocabulary; a stable one is picked per turn.
var spinnerVerbs = []string{
	"Inferring", "Waddling", "Cogitating", "Contemplating", "Pondering",
	"Percolating", "Ruminating", "Musing", "Brewing", "Simmering",
	"Processing", "Crunching", "Churning", "Marinating", "Mulling",
}

type spinnerPhase string

const (
	phaseIdle     spinnerPhase = ""
	phaseThinking spinnerPhase = "thinking"
	phaseWorking  spinnerPhase = "working"
)

// panelRows truncates the panel's lines to the width and caps them to
// the height left above the docked chrome, dropping the oldest rows
// first; the spinner row is appended last when a label is set.
func panelRows(lines []string, spinner string, width, height, dockRows int) []string {
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
	max := height - dockRows - 1
	if max < 1 {
		max = 1
	}
	if len(rows) > max {
		rows = rows[len(rows)-max:]
	}
	return rows
}

// turnPanel is the live region a turn renders its progress into. Tool
// steps, thoughts and the spinner repaint in place above the docked
// chrome while the turn runs; the chrome (input rules and status rows)
// stays pinned at the bottom at all times. When the turn ends the
// dynamic region is collapsed so only the final answer, the summary
// line and the pinned chrome remain. Text that must survive (streamed
// answer chunks, errors) is printed above the region via printAbove.
// The region never enters the alternate screen.
type turnPanel struct {
	out   io.Writer
	color bool

	mu             sync.Mutex
	active         bool
	chromeFn       func(width int) []string
	chromeWidth    int
	chrome         []string
	lines          []string
	spinnerLabel   string
	phase          spinnerPhase
	turnStart      time.Time
	verb           string
	outputTokens   int
	estChars       int
	usingRealUsage bool
	frame          int
	renderedRows   int
	curRow         int
	stop           chan struct{}
}

func newTurnPanel(out io.Writer, color bool) *turnPanel {
	return &turnPanel{out: out, color: color}
}

// setChrome registers the builder for the pinned rows rendered
// beneath the dynamic region (top rule, static input row, bottom rule
// and two status rows). It is rebuilt on the first paint and on
// resizes; spinner ticks reuse the snapshot.
func (p *turnPanel) setChrome(fn func(width int) []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chromeFn = fn
	p.chromeWidth = 0
}

// begin activates the panel for a new turn. A non-empty label pins a
// static spinner line; otherwise the spinner tracks the live phase
// (thinking/working, elapsed seconds and output tokens).
func (p *turnPanel) begin(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.active {
		p.spinnerLabel = label
		if label == "" {
			p.phase = phaseWorking
		}
		p.redraw()
		return
	}
	p.active = true
	p.lines = nil
	p.spinnerLabel = label
	p.phase = phaseIdle
	p.turnStart = time.Now()
	p.outputTokens = 0
	p.estChars = 0
	p.usingRealUsage = false
	p.verb = spinnerVerbs[int(time.Now().UnixNano())%len(spinnerVerbs)]
	p.chromeWidth = 0
	p.renderedRows = 0
	p.seedCursorRow()
	if label == "" {
		p.phase = phaseWorking
	}
	p.startTicker()
	p.setCursor(false)
	p.redraw()
}

// setSpinner pins a static spinner label (browsing gerunds and other
// explicit progress phrases).
func (p *turnPanel) setSpinner(label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spinnerLabel = label
	p.redraw()
}

// isPinned reports whether a static label is currently shown.
func (p *turnPanel) isPinned() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spinnerLabel != ""
}

// setPhase switches the dynamic spinner phase; the elapsed-time clock
// is turn-scoped and keeps running across phases.
func (p *turnPanel) setPhase(phase spinnerPhase) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spinnerLabel = ""
	p.phase = phase
	p.redraw()
}

// setOutputTokens records real usage, replacing the streaming
// estimate for the rest of the turn.
func (p *turnPanel) setOutputTokens(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.outputTokens = n
	p.usingRealUsage = true
	p.redraw()
}

// addStreamedChars accumulates answer characters while streaming so
// the spinner can estimate ~1 token per four characters until real
// usage arrives.
func (p *turnPanel) addStreamedChars(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.usingRealUsage {
		return
	}
	p.estChars += n
	p.redraw()
}

// shownOutputTokens returns the real count once known, otherwise the
// four-characters-per-token estimate.
func (p *turnPanel) shownOutputTokens() int {
	if p.usingRealUsage {
		return p.outputTokens
	}
	return p.estChars / 4
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

// printAbove erases the dynamic region and docked chrome, writes
// permanent text and redraws everything below it. Inactive panels
// pass the text straight through.
func (p *turnPanel) printAbove(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	s = expandTabsDisplay(s)
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
		p.advanceText(s)
	}
	p.redraw()
}

// suspend erases the dynamic region and docked chrome for an inline
// modal (permission picker) and stops the spinner; resume repaints
// them below the modal.
func (p *turnPanel) suspend() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopTicker()
	p.erase()
	p.active = false
	p.setCursor(true)
}

func (p *turnPanel) resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = true
	p.renderedRows = 0
	p.seedCursorRow()
	p.startTicker()
	p.setCursor(false)
	p.redraw()
}

// collapseDynamic clears the spinner and progress rows while keeping
// the docked chrome painted, used when the server reports end_turn but
// the runner has not returned yet.
func (p *turnPanel) collapseDynamic() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active {
		return
	}
	p.stopTicker()
	p.lines = nil
	p.spinnerLabel = ""
	p.phase = phaseIdle
	p.redraw()
}

// finish erases the dynamic region and docked chrome and deactivates
// the panel, optionally printing a permanent final line where the
// region used to be. The next editor invocation repaints the chrome.
func (p *turnPanel) finish(final string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopTicker()
	p.erase()
	p.lines = nil
	p.spinnerLabel = ""
	p.phase = phaseIdle
	p.active = false
	p.setCursor(true)
	if final != "" {
		io.WriteString(p.out, final+"\n")
	}
}

func (p *turnPanel) setCursor(visible bool) {
	if visible {
		io.WriteString(p.out, "\x1b[?25h")
	} else {
		io.WriteString(p.out, "\x1b[?25l")
	}
}

// erase clears the painted rows (dynamic region plus chrome); callers
// must hold mu. After a redraw the cursor parks at column 1 of the
// last chrome row, so moving up renderedRows-1 and clearing below
// removes the whole painted block.
func (p *turnPanel) erase() {
	if p.renderedRows > 1 {
		fmt.Fprintf(p.out, "\x1b[%dA", p.renderedRows-1)
		p.curRow -= p.renderedRows - 1
		if p.curRow < 1 {
			p.curRow = 1
		}
	}
	io.WriteString(p.out, "\r\x1b[J")
	p.renderedRows = 0
}

// redraw repaints the transient progress rows and the docked block
// (spinner status + input chrome); callers must hold mu. The spinner
// lives in the dock directly above the input frame so it always stays
// pinned at the bottom instead of scrolling with the tool blocks.
// When the block no longer fits between the cursor and the bottom of
// the screen the terminal is scrolled by exactly the deficit first,
// so painting never spills past the last row and the block can always
// be erased again by moving the cursor up.
func (p *turnPanel) redraw() {
	width := p.width()
	height := p.height()
	if p.curRow > height {
		p.curRow = height
	}
	if p.chromeFn != nil && width != p.chromeWidth {
		p.chrome = p.chromeFn(width)
		p.chromeWidth = width
	}
	spinner := p.spinnerRow()
	if spinner != "" {
		spinner = truncateDisplayWidth(spinner, width-2)
	}
	dock := append([]string{}, p.chrome...)
	if spinner != "" {
		dock = append([]string{spinner, ""}, dock...)
	}
	dyn := panelRows(p.lines, "", width, height, len(dock)+1)
	rows := dyn
	if len(rows) > 0 {
		rows = append(append([]string{""}, rows...), dock...)
	} else {
		rows = append(rows, dock...)
	}
	var sb strings.Builder
	if p.renderedRows > 0 {
		if p.renderedRows > 1 {
			fmt.Fprintf(&sb, "\x1b[%dA", p.renderedRows-1)
			p.curRow -= p.renderedRows - 1
			if p.curRow < 1 {
				p.curRow = 1
			}
		}
		sb.WriteString("\r\x1b[J")
	}
	if p.curRow+len(rows)-1 > height && len(rows) > 1 {
		sb.WriteString(strings.Repeat("\n", len(rows)-1))
		fmt.Fprintf(&sb, "\x1b[%dA", len(rows)-1)
		p.curRow = height - len(rows) + 1
	}
	sb.WriteString("\r")
	for i, row := range rows {
		sb.WriteString("\x1b[2K")
		sb.WriteString(row)
		if i < len(rows)-1 {
			sb.WriteString("\n")
		}
	}
	io.WriteString(p.out, sb.String())
	p.curRow += len(rows) - 1
	p.renderedRows = len(rows)
}

// seedCursorRow anchors the cursor-row model at the bottom row. The
// conservative guess keeps make-room from ever under-scrolling: a DSR
// query can consume a stale reply left by an earlier probe and seed a
// row above the real cursor, which leaks painted frames into
// scrollback. Docking at the bottom matches the reference layout and
// costs at most a few blank lines.
func (p *turnPanel) seedCursorRow() {
	p.curRow = p.height()
}

// advanceText moves the cursor-row model past committed text whose
// lines may wrap at the terminal width.
func (p *turnPanel) advanceText(s string) {
	rows := 0
	width := p.width()
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		rows += textRows(line, width)
	}
	p.advanceRows(rows)
}

// advanceRows moves the cursor-row model down, stopping at the bottom
// row where the terminal scrolls instead of moving the cursor.
func (p *turnPanel) advanceRows(n int) {
	height := p.height()
	if p.curRow+n > height {
		p.curRow = height
		return
	}
	p.curRow += n
}

// textRows returns the screen rows one text line occupies at the given
// width, counting East Asian ambiguous runes as two cells and assuming
// immediate wrap: a line that exactly fills the width already moves
// the cursor to the next row. Overestimating by one row only docks the
// frame early; underestimating leaks painted frames into scrollback.
func textRows(line string, width int) int {
	w := displayWidth(line)
	if w <= 0 || width <= 0 {
		return 1
	}
	return 1 + w/width
}

func (p *turnPanel) spinnerRow() string {
	if p.spinnerLabel != "" {
		return activityFrames[p.frame%len(activityFrames)] + " " + p.spinnerLabel
	}
	if p.phase == phaseIdle {
		return ""
	}
	verb := p.verb
	if p.phase == phaseWorking {
		verb = "Working"
	}
	if p.phase == phaseThinking {
		verb = "Inferring"
	}
	elapsed := ""
	if !p.turnStart.IsZero() {
		elapsed = formatThoughtDuration(time.Since(p.turnStart))
	}
	suffix := ""
	if n := p.shownOutputTokens(); n > 0 {
		suffix = fmt.Sprintf(" (%s · ↓ %d tokens)", elapsed, n)
	} else if p.phase == phaseThinking {
		suffix = fmt.Sprintf(" (%s · thinking)", elapsed)
	} else {
		suffix = fmt.Sprintf(" (%s)", elapsed)
	}
	return activityFrames[p.frame%len(activityFrames)] + " " + verb + "…" + cDim + suffix + cReset
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
				if p.active && (p.spinnerLabel != "" || p.phase != phaseIdle) {
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
