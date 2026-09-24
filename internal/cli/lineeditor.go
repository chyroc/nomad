package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// ErrInterrupt reports that the user pressed Ctrl+C while editing.
var ErrInterrupt = errors.New("interrupt")

// lineEditor reads one logical input line from the terminal. The
// interactive path runs a short-lived bubbletea program around the
// bubbles textinput; the non-TTY path falls back to a plain buffered
// read so headless invocations never hang.
type lineEditor struct {
	raw         io.Reader
	in          *bufio.Reader
	out         io.Writer
	fd          int
	history     []string
	historyPath string
	histIdx     int
	saved       string
	completer   func(line string) []string
	onFold      func() []string

	searching   bool
	searchQuery []rune
	searchHit   int
}

func newLineEditor(in io.Reader, out io.Writer, history []string, historyPath string) *lineEditor {
	fd := -1
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		fd = int(f.Fd())
	}
	if len(history) == 0 && historyPath != "" {
		history = loadHistory(historyPath)
	}
	return &lineEditor{
		raw:         in,
		out:         out,
		fd:          fd,
		history:     history,
		historyPath: historyPath,
		histIdx:     len(history),
	}
}

func (e *lineEditor) setCompleter(f func(string) []string) { e.completer = f }

// History returns the in-memory history entries.
func (e *lineEditor) History() []string { return e.history }

func (e *lineEditor) width() int {
	if w, _ := cachedTermSize(); w > 0 {
		return w
	}
	return 80
}

// ReadLine reads one submission. Multi-line bracketed pastes are kept
// as chips while editing and expanded into the returned text.
func (e *lineEditor) ReadLine(prompt string) (string, error) {
	if e.fd < 0 || !term.IsTerminal(e.fd) {
		return e.readLinePlain(prompt)
	}
	e.histIdx = len(e.history)
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = 0
	_ = ti.Cursor.SetMode(cursor.CursorStatic)
	m := &editorModel{ed: e, prompt: prompt, ti: ti, width: e.width()}
	m.syncWidth()
	p := tea.NewProgram(m, tea.WithInput(e.raw), tea.WithOutput(e.out), tea.WithoutSignalHandler())
	m.prog = p
	stopRelay := relayResize(p)
	_, err := p.Run()
	stopRelay()
	if err != nil {
		return "", err
	}
	switch m.status {
	case editorSubmitted:
		full := m.fullText()
		if line := strings.TrimSpace(full); line != "" {
			e.history = appendHistoryEntry(e.historyPath, e.history, line)
		}
		e.histIdx = len(e.history)
		return full, nil
	case editorInterrupted:
		return "", ErrInterrupt
	default:
		return "", io.EOF
	}
}

func (e *lineEditor) readLinePlain(prompt string) (string, error) {
	if e.in == nil {
		e.in = bufio.NewReader(e.raw)
	}
	io.WriteString(e.out, prompt)
	line, err := e.in.ReadString('\n')
	return strings.TrimRight(line, "\n"), err
}

type editorStatus int

const (
	editorEditing editorStatus = iota
	editorSubmitted
	editorInterrupted
	editorEOF
)

// editorModel is the bubbletea model behind ReadLine's interactive
// path. It always renders two rows (input plus slash hints) and leaves
// a final echo of the submission in the scrollback on exit.
type editorModel struct {
	ed     *lineEditor
	prompt string
	ti     textinput.Model
	pasted []string
	echo   string
	status editorStatus
	width  int
	prog   *tea.Program
}

func (m *editorModel) Init() tea.Cmd {
	return m.ti.Focus()
}

func (m *editorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.syncWidth()
		return m, nil
	case resizeMsg:
		if w, _ := cachedTermSize(); w > 0 {
			m.width = w
			m.syncWidth()
		}
		return m, nil
	case tea.KeyMsg:
		if cmd, handled := m.handleKey(msg); handled {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m *editorModel) View() string {
	if m.status == editorSubmitted {
		return m.echo + "\n"
	}
	return m.inputRow() + "\n" + m.hintRow()
}

func (m *editorModel) inputRow() string {
	row := m.ti.View()
	for _, p := range m.pasted {
		row += pasteChip(p)
	}
	return row
}

func (m *editorModel) hintRow() string {
	hints := slashHints(m.ed.completer, m.displayed())
	if len(hints) == 0 {
		return ""
	}
	line := strings.Join(stripAnsiList(hints), "  ")
	return cDim + truncateToWidth(line, max(m.width-2, 0)) + cReset
}

// syncWidth keeps the textinput's value budget inside the terminal
// after subtracting the prompt, the paste chips and one cursor cell.
func (m *editorModel) syncWidth() {
	chipWidth := 0
	for _, p := range m.pasted {
		chipWidth += visiblePromptWidth(pasteChip(p))
	}
	usable := m.width - visiblePromptWidth(m.prompt) - chipWidth - 1
	if usable < 8 {
		usable = 8
	}
	m.ti.Width = usable
}

// handleKey implements the nomad bindings on top of textinput. It
// returns handled=true when the key must not reach textinput.
func (m *editorModel) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlJ:
		full := m.fullText()
		if len(m.pasted) == 0 {
			if h := slashUnique(m.ed.completer, strings.TrimSpace(m.ti.Value())); h != "" {
				full = h
			}
		}
		m.echo = m.prompt + full
		m.status = editorSubmitted
		return tea.Quit, true
	case msg.Type == tea.KeyCtrlC:
		m.status = editorInterrupted
		return tea.Quit, true
	case msg.Type == tea.KeyCtrlD:
		if m.ti.Value() == "" && len(m.pasted) == 0 {
			m.status = editorEOF
			return tea.Quit, true
		}
		return nil, true
	case msg.Type == tea.KeyRunes && msg.Paste:
		m.pasted = append(m.pasted, normalizePasted(string(msg.Runes)))
		m.syncWidth()
		return nil, true
	case msg.Type == tea.KeyUp:
		if m.ed.histIdx > 0 {
			if m.ed.histIdx == len(m.ed.history) {
				m.ed.saved = m.ti.Value()
			}
			m.ed.histIdx--
			m.setLine(m.ed.history[m.ed.histIdx])
		}
		return nil, true
	case msg.Type == tea.KeyDown:
		if m.ed.histIdx < len(m.ed.history) {
			m.ed.histIdx++
			if m.ed.histIdx == len(m.ed.history) {
				m.setLine(m.ed.saved)
			} else {
				m.setLine(m.ed.history[m.ed.histIdx])
			}
		}
		return nil, true
	case msg.Type == tea.KeyCtrlR:
		if nb, _, found := m.ed.searchHistory([]rune(m.ti.Value())); found {
			m.setLine(string(nb))
		}
		return nil, true
	case msg.Type == tea.KeyTab:
		if len(m.pasted) == 0 {
			if c := slashComplete(m.ed.completer, m.ti.Value()); c != "" {
				m.setLine(c)
			}
		}
		return nil, true
	case msg.Type == tea.KeyCtrlU:
		m.ti.Reset()
		m.pasted = nil
		m.syncWidth()
		return nil, true
	case msg.Type == tea.KeyCtrlW:
		m.deleteWordLeft()
		return nil, true
	case msg.Type == tea.KeyCtrlO:
		if m.prog != nil && m.ed.onFold != nil {
			for _, l := range m.ed.onFold() {
				m.prog.Println(l)
			}
		}
		return nil, true
	case msg.Type == tea.KeyCtrlV:
		return nil, true
	case msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == 'b':
		m.ti.SetCursor(wordLeft([]rune(m.ti.Value()), m.ti.Position()))
		return nil, true
	case msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == 'f':
		m.ti.SetCursor(wordRight([]rune(m.ti.Value()), m.ti.Position()))
		return nil, true
	case msg.Type == tea.KeyBackspace && m.ti.Position() == 0 && len(m.pasted) > 0:
		m.pasted = m.pasted[:len(m.pasted)-1]
		m.syncWidth()
		return nil, true
	}
	return nil, false
}

func (m *editorModel) setLine(s string) {
	m.ti.SetValue(s)
	m.ti.CursorEnd()
}

// deleteWordLeft removes the whitespace and the word left of the
// cursor, matching the classic Ctrl+W behavior (no trailing space is
// kept).
func (m *editorModel) deleteWordLeft() {
	runes := []rune(m.ti.Value())
	pos := m.ti.Position()
	i := pos
	for i > 0 && (runes[i-1] == ' ' || runes[i-1] == '\t') {
		i--
	}
	for i > 0 && runes[i-1] != ' ' && runes[i-1] != '\t' {
		i--
	}
	m.ti.SetValue(string(runes[:i]) + string(runes[pos:]))
	m.ti.SetCursor(i)
}

func (m *editorModel) displayed() string {
	d := m.ti.Value()
	for _, p := range m.pasted {
		d += pasteChip(p)
	}
	return d
}

// fullText joins the typed text and every pasted chip into the text
// that is actually submitted.
func (m *editorModel) fullText() string {
	out := m.ti.Value()
	for _, p := range m.pasted {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += strings.TrimRight(p, "\n")
	}
	return out
}

// searchHistory performs a substring reverse search over history. The
// first press uses the current input as the query and jumps to the most
// recent match; repeated presses walk to older matches. Returns false if
// nothing matched so the key can be ignored.
func (e *lineEditor) searchHistory(query []rune) (newBuf []rune, newCursor int, found bool) {
	q := strings.ToLower(strings.TrimSpace(string(query)))
	if q == "" {
		return nil, 0, false
	}
	start := len(e.history) - 1
	if e.searching && string(e.searchQuery) == string(query) && e.searchHit >= 0 {
		start = e.searchHit - 1
	}
	for i := start; i >= 0; i-- {
		if strings.Contains(strings.ToLower(e.history[i]), q) {
			e.searching = true
			e.searchQuery = append(e.searchQuery[:0], query...)
			e.searchHit = i
			e.histIdx = i
			r := []rune(e.history[i])
			return r, len(r), true
		}
	}
	return nil, 0, false
}

func normalizePasted(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n")
}

func pasteChip(text string) string {
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	return fmt.Sprintf(" [Pasted text +%d lines] ", lines-1)
}

func visiblePromptWidth(s string) int {
	return ansi.StringWidthWc(s)
}

func isWordChar(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		(r >= 0x4E00 && r <= 0x9FFF)
}

func isCJK(r rune) bool { return r >= 0x4E00 && r <= 0x9FFF }

func wordLeft(buf []rune, cursor int) int {
	i := cursor
	for i > 0 && !isWordChar(buf[i-1]) {
		i--
	}
	if i > 0 && isCJK(buf[i-1]) {
		return i - 1
	}
	for i > 0 && isWordChar(buf[i-1]) && !isCJK(buf[i-1]) {
		i--
	}
	return i
}

func wordRight(buf []rune, cursor int) int {
	i := cursor
	for i < len(buf) && !isWordChar(buf[i]) {
		i++
	}
	if i < len(buf) && isCJK(buf[i]) {
		return i + 1
	}
	for i < len(buf) && isWordChar(buf[i]) && !isCJK(buf[i]) {
		i++
	}
	return i
}

func truncateToWidth(s string, width int) string {
	return ansi.TruncateWc(s, width, "")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func slashHints(complete func(string) []string, line string) []string {
	if complete == nil {
		return nil
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") || strings.ContainsAny(trimmed, " \t") {
		return nil
	}
	matches := complete(line)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		m = strings.TrimSpace(m)
		if !strings.HasPrefix(m, trimmed) || m == trimmed || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

func stripAnsiList(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = ansi.Strip(s)
	}
	return out
}

func slashUnique(complete func(string) []string, line string) string {
	if complete == nil || !strings.HasPrefix(line, "/") {
		return ""
	}
	matches := complete(line)
	if len(matches) == 1 && matches[0] != line {
		return matches[0]
	}
	return ""
}

func slashComplete(complete func(string) []string, line string) string {
	if complete == nil {
		return ""
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") || strings.ContainsAny(trimmed, " \t") {
		return ""
	}
	var matches []string
	seen := map[string]bool{}
	for _, m := range complete(trimmed) {
		m = strings.TrimSpace(m)
		if !strings.HasPrefix(m, trimmed) || m == trimmed || seen[m] {
			continue
		}
		seen[m] = true
		matches = append(matches, m)
	}
	switch len(matches) {
	case 0:
		return ""
	case 1:
		return matches[0]
	}
	prefix := matches[0]
	for _, m := range matches[1:] {
		for !strings.HasPrefix(m, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if len(prefix) > len(trimmed) {
		return prefix
	}
	return ""
}
