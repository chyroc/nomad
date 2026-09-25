package cli

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

type pickItem struct {
	id    string
	label string
	tag   string
	desc  string
}

type picker struct {
	in       io.Reader
	out      io.Writer
	fd       int
	items    []pickItem
	initial  int
	title    string
	subtitle string
	efforts  []string
	effort0  int
	session  bool
	multi    bool
	checked  map[string]bool
}

func newPicker(in io.Reader, out io.Writer, items []pickItem, initial int) *picker {
	return newPickerFull(in, out, items, initial, "", "", nil, 0)
}

func newPickerFull(in io.Reader, out io.Writer, items []pickItem, initial int,
	title, subtitle string, efforts []string, effortIdx int) *picker {
	fd := -1
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		fd = int(f.Fd())
	}
	if initial < 0 || initial >= len(items) {
		initial = 0
	}
	return &picker{in: in, out: out, fd: fd, items: items, initial: initial,
		title: title, subtitle: subtitle, efforts: efforts, effort0: effortIdx}
}

// withSessionSave enables the "s to use this session only" action.
func (p *picker) withSessionSave() *picker {
	p.session = true
	return p
}

// withMultiSelect turns the picker into a checkbox list: Space toggles
// the highlighted row, a toggles all visible rows, Enter confirms the
// checked set.
func (p *picker) withMultiSelect(initial []string) *picker {
	p.multi = true
	p.checked = map[string]bool{}
	for _, id := range initial {
		p.checked[id] = true
	}
	return p
}

type pickResult struct {
	id        string
	ids       []string
	confirmed bool
	session   bool
	effortIdx int
}

func (p *picker) Run() (string, bool) {
	r, ok := p.RunFull()
	return r.id, ok && r.confirmed
}

// RunMulti returns the checked item ids after a multi-select run.
func (p *picker) RunMulti() ([]string, bool) {
	r, ok := p.RunFull()
	return r.ids, ok && r.confirmed
}

// RunFull shows the inline picker and blocks until the user confirms
// or cancels. The panel is anchored near the cursor (moved up first so
// the bubbletea frame paints over the rows today's panel would occupy)
// and fully erased on exit; a plain bubbletea program in inline mode
// never enters the alternate screen.
func (p *picker) RunFull() (pickResult, bool) {
	if p.fd < 0 || !term.IsTerminal(p.fd) {
		return pickResult{}, false
	}
	width, height := cachedTermSize()
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	cursor := p.anchorRow()
	m := newPickerModel(p, width, height)
	if cursor > 0 {
		if cursor > height {
			cursor = height
		}
		needed := m.panelRows()
		anchor := cursor
		if anchor+needed-1 > height {
			anchor = height - needed + 1
		}
		if anchor < 1 {
			anchor = 1
		}
		if cursor > anchor {
			fmt.Fprintf(p.out, "\x1b[%dA\r", cursor-anchor)
		}
	}
	prog := tea.NewProgram(m, tea.WithInput(p.in), tea.WithOutput(p.out), tea.WithoutSignalHandler())
	stopRelay := relayResize(prog)
	final, err := prog.Run()
	stopRelay()
	io.WriteString(p.out, "\n")
	if err != nil {
		return pickResult{}, false
	}
	res, _ := final.(*pickerModel)
	if res == nil || !res.result.confirmed {
		return pickResult{}, false
	}
	return res.result, true
}

// anchorRow returns the terminal row the cursor sits on, or 0 when the
// DSR reply is unavailable.
func (p *picker) anchorRow() int {
	row, ok := queryCursorRow(p.in, p.out)
	if !ok {
		return 0
	}
	return row
}

func newPickerModel(p *picker, width, height int) *pickerModel {
	footRows := 1
	if len(p.efforts) > 0 {
		footRows++
	}
	m := &pickerModel{
		items:    p.items,
		title:    p.title,
		subtitle: p.subtitle,
		efforts:  p.efforts,
		session:  p.session,
		multi:    p.multi,
		checked:  p.checked,
		sel:      p.initial,
		effort:   p.effort0,
		width:    width,
		height:   height,
		footRows: footRows,
	}
	m.refilter()
	return m
}

// pickerModel renders the picker panel as plain lines and keeps the
// selection, filter query and effort index for the bubbletea loop.
type pickerModel struct {
	items    []pickItem
	title    string
	subtitle string
	efforts  []string
	session  bool
	multi    bool
	checked  map[string]bool

	query  []rune
	vis    []pickItem
	sel    int
	effort int
	top    int
	width  int
	height int

	footRows int
	result   pickResult
	done     bool
}

func (m *pickerModel) Init() tea.Cmd { return nil }

func (m *pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case resizeMsg:
		if w, h := cachedTermSize(); w > 0 && h > 0 {
			m.width, m.height = w, h
		}
		return m, nil
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}
	return m, nil
}

func (m *pickerModel) View() string {
	if m.done {
		return ""
	}
	var lines []string
	lines = append(lines, cDim+strings.Repeat("─", 64)+cReset)
	lines = append(lines, cBold+"  "+m.title+cReset)
	if m.subtitle != "" {
		lines = append(lines, cDim+"  "+truncateToWidth(m.subtitle, max(m.width-4, 0))+cReset)
	}
	lines = append(lines, "")
	shown := m.shownCount()
	if shown == 0 {
		lines = append(lines, cDim+"    (no matches)"+cReset)
	}
	for i := 0; i < shown; i++ {
		idx := m.top + i
		if idx >= len(m.vis) {
			break
		}
		lines = append(lines, m.itemRow(idx))
	}
	if len(m.efforts) > 0 && m.effort < len(m.efforts) {
		lines = append(lines, cDim+"  ◉ "+m.efforts[m.effort]+" effort  ←/→ to adjust"+cReset)
	}
	lines = append(lines, cDim+m.footer()+cReset)
	return strings.Join(lines, "\n")
}

func (m *pickerModel) itemRow(idx int) string {
	it := m.vis[idx]
	marker := "    "
	label := it.label
	if m.multi {
		box := "○"
		if m.checked[it.id] {
			box = "◉"
		}
		if idx == m.sel {
			marker = "  " + cCyan + "❯" + cReset + " " + cCyan + box + cReset + " "
			label = cCyan + it.label + cReset
		} else if m.checked[it.id] {
			marker = "    " + cGreen + box + cReset + " "
		} else {
			marker = "    " + box + " "
		}
	} else if idx == m.sel {
		marker = "  " + cCyan + "❯" + cReset + " "
		label = cCyan + it.label + cReset
	}
	line := label
	if it.tag != "" {
		line += "  " + it.tag
	}
	if m.multi && it.desc != "" {
		line += "  " + it.desc
	}
	return marker + truncateToWidth(line, max(m.width-6, 0))
}

func (m *pickerModel) footer() string {
	foot := "  Enter to confirm · Esc to cancel"
	switch {
	case m.multi:
		n := 0
		for _, it := range m.vis {
			if m.checked[it.id] {
				n++
			}
		}
		foot = fmt.Sprintf("  Space to toggle · * to toggle all · Enter to confirm %d selected · Esc to cancel", n)
	case m.session:
		foot = "  Enter to set as default · s to use this session only · Esc to cancel"
	}
	return foot
}

const pickerHeaderRows = 4

func (m *pickerModel) shownCount() int {
	maxItems := m.height - pickerHeaderRows - m.footRows
	if maxItems < 1 {
		maxItems = 1
	}
	shown := len(m.vis)
	if shown > maxItems {
		shown = maxItems
	}
	return shown
}

// panelRows reports the frame height of the initial view so the
// caller can move the cursor up before the program starts.
func (m *pickerModel) panelRows() int {
	shown := m.shownCount()
	if shown == 0 {
		shown = 1
	}
	return pickerHeaderRows + shown + m.footRows
}

// refilter recomputes the visible rows after a query or size change
// and keeps the selection inside the result.
func (m *pickerModel) refilter() {
	m.vis = filterPickItems(m.items, string(m.query))
	if m.sel >= len(m.vis) {
		m.sel = 0
		m.top = 0
	}
	m.syncTop()
}

func (m *pickerModel) syncTop() {
	m.top = scrollWindow(len(m.vis), m.shownCount(), m.sel, m.top)
}

// handleKey applies one keystroke and returns tea.Quit when the
// interaction finished.
func (m *pickerModel) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyEscape:
		m.done = true
		return tea.Quit
	case tea.KeyEnter, tea.KeyCtrlJ:
		if m.multi {
			var ids []string
			for _, it := range m.items {
				if m.checked[it.id] {
					ids = append(ids, it.id)
				}
			}
			m.result = pickResult{ids: ids, confirmed: true}
			m.done = true
			return tea.Quit
		}
		if m.sel < len(m.vis) {
			m.result = pickResult{id: m.vis[m.sel].id, confirmed: true, effortIdx: m.effort}
			m.done = true
			return tea.Quit
		}
	case tea.KeyUp:
		if m.sel > 0 {
			m.sel--
		}
		m.syncTop()
	case tea.KeyDown:
		if m.sel < len(m.vis)-1 {
			m.sel++
		}
		m.syncTop()
	case tea.KeyRight:
		if len(m.efforts) > 0 && m.effort < len(m.efforts)-1 {
			m.effort++
		}
	case tea.KeyLeft:
		if len(m.efforts) > 0 && m.effort > 0 {
			m.effort--
		}
	case tea.KeyBackspace:
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.refilter()
		}
	case tea.KeySpace:
		if m.multi {
			if m.sel < len(m.vis) {
				id := m.vis[m.sel].id
				m.checked[id] = !m.checked[id]
			}
			return nil
		}
		m.appendQuery(' ')
	case tea.KeyRunes:
		if msg.Paste {
			return nil
		}
		for _, r := range msg.Runes {
			if m.done {
				break
			}
			m.appendQuery(r)
		}
	}
	return nil
}

// appendQuery applies one typed rune: '*' toggles every visible row in
// multi-select, 's' confirms with session scope when enabled, anything
// else extends the filter query.
func (m *pickerModel) appendQuery(r rune) {
	if r == '*' && m.multi {
		allChecked := len(m.vis) > 0
		for _, it := range m.vis {
			if !m.checked[it.id] {
				allChecked = false
				break
			}
		}
		for _, it := range m.vis {
			m.checked[it.id] = !allChecked
		}
		return
	}
	if r == 's' && m.session && !m.multi {
		if m.sel < len(m.vis) {
			m.result = pickResult{id: m.vis[m.sel].id, confirmed: true, session: true, effortIdx: m.effort}
			m.done = true
		}
		return
	}
	m.query = append(m.query, r)
	m.refilter()
}

func scrollWindow(total, shown, sel, top int) int {
	if shown >= total {
		return 0
	}
	if top > sel {
		top = sel
	}
	if sel >= top+shown {
		top = sel - shown + 1
	}
	if top+shown > total {
		top = total - shown
	}
	if top < 0 {
		top = 0
	}
	return top
}
