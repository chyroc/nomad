package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

var ErrInterrupt = errors.New("interrupt")

var pasteEndMarker = []byte("\x1b[201~")

type lineEditor struct {
	in        *bufio.Reader
	out       io.Writer
	fd        int
	history   []string
	histIdx   int
	saved     string
	completer func(line string) []string
	onMouse   func(button, x, y int) bool
	rawState  *term.State

	searching   bool
	searchQuery []rune
	searchHit   int
}

func newLineEditor(in io.Reader, out io.Writer, history []string) *lineEditor {
	br, _ := in.(*bufio.Reader)
	if br == nil {
		br = bufio.NewReader(in)
	}
	fd := -1
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		fd = int(f.Fd())
	}
	return &lineEditor{in: br, out: out, fd: fd, history: history, histIdx: len(history)}
}

func (e *lineEditor) setCompleter(f func(string) []string) { e.completer = f }

func (e *lineEditor) width() int {
	if w, _ := cachedTermSize(); w > 0 {
		return w
	}
	return 80
}

func (e *lineEditor) ReadLine(prompt string) (string, error) {
	if e.fd < 0 {
		io.WriteString(e.out, prompt)
		line, err := e.in.ReadString('\n')
		return strings.TrimRight(line, "\n"), err
	}

	old, err := term.MakeRaw(e.fd)
	if err != nil {
		io.WriteString(e.out, prompt)
		line, lerr := e.in.ReadString('\n')
		return strings.TrimRight(line, "\n"), lerr
	}
	restore := func() {
		e.rawState = nil
		term.Restore(e.fd, old)
	}
	defer restore()
	e.rawState = old

	_ = prompt
	var buf []rune
	cursor := 0
	e.histIdx = len(e.history)
	var pasted []string
	pasting := false

	chip := func(text string) string {
		lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
		return fmt.Sprintf(" [Pasted text +%d lines] ", lines-1)
	}
	displayed := func() string {
		d := string(buf)
		for _, p := range pasted {
			d += chip(p)
		}
		return d
	}
	fullText := func() string {
		out := string(buf)
		for _, p := range pasted {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += strings.TrimRight(p, "\n")
		}
		return out
	}

	redraw := func() {
		shown := displayed()
		promptWidth := visiblePromptWidth(prompt)
		width := e.width()
		usable := width - promptWidth
		if usable < 8 {
			usable = 8
		}
		cursorCol := runewidth.StringWidth(string(buf[:cursor]))
		for _, p := range pasted {
			cursorCol += runewidth.StringWidth(chip(p))
		}
		startRune := 0
		if cursorCol > usable {
			startRune, _ = indexAtWidth(shown, cursorCol-usable)
		}
		view := truncateToWidth(shown[startRune:], usable)
		viewStart := runewidth.StringWidth(shown[:startRune])
		cursorTerm := promptWidth + cursorCol - viewStart

		hints := slashHints(e.completer, shown)
		newCount := len(hints)

		var sb strings.Builder
		sb.WriteString("\r\x1b[2K")
		sb.WriteString(prompt)
		sb.WriteString(view)
		sb.WriteString("\n\r\x1b[2K")
		if newCount > 0 {
			line := strings.Join(stripAnsiList(hints), "  ")
			sb.WriteString("\x1b[2m" + truncateToWidth(line, e.width()-2) + "\x1b[0m")
		}
		sb.WriteString("\x1b[1A")
		sb.WriteString("\r\x1b[" + itoa(cursorTerm+1) + "G")
		io.WriteString(e.out, sb.String())
	}

	// drainPaste consumes buffered paste content but never the end
	// marker. Real terminals deliver start + content + end in one
	// packet, so a blind drain would swallow ESC[201~ as text and leave
	// the editor stuck in paste mode (Enter then does nothing). It
	// returns the content and ended=true when the end marker is present.
	drainPaste := func() (string, bool) {
		n := e.in.Buffered()
		if n == 0 {
			return "", false
		}
		peek, _ := e.in.Peek(n)
		end := n
		ended := false
		if i := bytes.Index(peek, pasteEndMarker); i >= 0 {
			end = i
			ended = true
		} else {
			for k := len(pasteEndMarker) - 1; k >= 1; k-- {
				if bytes.HasSuffix(peek, pasteEndMarker[:k]) {
					end = n - k
					break
				}
			}
		}
		if end <= 0 {
			return "", ended
		}
		buf2 := make([]byte, end)
		nn, _ := io.ReadFull(e.in, buf2)
		return string(buf2[:nn]), ended
	}
	consumePasteEnd := func() {
		n := len(pasteEndMarker)
		if e.in.Buffered() < n {
			return
		}
		peek, _ := e.in.Peek(n)
		if bytes.Equal(peek, pasteEndMarker) {
			_, _ = e.in.Discard(n)
		}
	}
	bufferedHasNewline := func() bool {
		n := e.in.Buffered()
		if n == 0 {
			return false
		}
		peek, _ := e.in.Peek(n)
		return strings.ContainsAny(string(peek), "\r\n")
	}

	redraw()
	for {
		r, _, err := e.in.ReadRune()
		if err != nil {
			return "", err
		}

		switch {
		case r == '\r' || r == '\n':
			if pasting {
				continue
			}
			if e.in.Buffered() > 0 && bufferedHasNewline() {
				rest, ended := drainPaste()
				pasted = append(pasted, normalizePasted(rest))
				if ended {
					pasting = false
					consumePasteEnd()
				}
				continue
			}
			full := fullText()
			if len(pasted) == 0 {
				if h := slashUnique(e.completer, strings.TrimSpace(string(buf))); h != "" {
					full = h
				}
			}
			var clear strings.Builder
			clear.WriteString("\r\x1b[2K")
			clear.WriteString("\n\r\x1b[2K\x1b[1A")
			io.WriteString(e.out, clear.String()+prompt+displayed()+"\r\n")
			if line := strings.TrimSpace(full); line != "" {
				if len(e.history) == 0 || e.history[len(e.history)-1] != line {
					e.history = append(e.history, line)
				}
			}
			e.histIdx = len(e.history)
			return full, nil

		case r == 3:
			io.WriteString(e.out, "\r\n")
			return "", ErrInterrupt

		case r == 4:
			if len(buf) == 0 && len(pasted) == 0 {
				io.WriteString(e.out, "\r\n")
				return "", io.EOF
			}

		case r == 127 || r == 8:
			if cursor > 0 {
				buf = append(buf[:cursor-1], buf[cursor:]...)
				cursor--
			} else if len(pasted) > 0 {
				pasted = pasted[:len(pasted)-1]
			}

		case r == 1:
			cursor = 0

		case r == 5:
			cursor = len(buf)

		case r == 18:
			// Ctrl+R: reverse history search. Matches history entries
			// containing the current text; repeated presses walk older.
			if nb, nc, found := e.searchHistory(buf); found {
				buf, cursor = nb, nc
				continue
			}

		case r == 23:
			i := cursor
			for i > 0 && (buf[i-1] == ' ' || buf[i-1] == '\t') {
				i--
			}
			for i > 0 && buf[i-1] != ' ' && buf[i-1] != '\t' {
				i--
			}
			buf = append(buf[:i], buf[cursor:]...)
			cursor = i

		case r == 11:
			buf = buf[:cursor]

		case r == 21:
			buf, pasted, cursor = nil, nil, 0

		case r == 15:
			if e.onMouse != nil {
				e.onMouse(-1, 0, 0)
			}

		case r == 27:
			if e.in.Buffered() == 0 {
				continue
			}
			r2, _, err2 := e.in.ReadRune()
			if err2 != nil {
				continue
			}
			if r2 == 'b' {
				cursor = wordLeft(buf, cursor)
				continue
			}
			if r2 == 'f' {
				cursor = wordRight(buf, cursor)
				continue
			}
			if r2 == '[' {
				r3, _, _ := e.in.ReadRune()
				if r3 == '<' {
					if b, x, y, ok := readSGRMouse(e.in); ok && e.onMouse != nil && b == 0 {
						e.onMouse(0, x, y)
					}
					continue
				}
				if r3 == '2' {
					param := []rune{r3}
					for {
						rn, _, err := e.in.ReadRune()
						if err != nil {
							break
						}
						if rn == '~' {
							break
						}
						param = append(param, rn)
					}
					switch string(param) {
					case "200", "2004":
						pasting = true
					case "201":
						pasting = false
					}
					continue
				}
				switch r3 {
				case 'A':
					if e.histIdx > 0 {
						if e.histIdx == len(e.history) {
							e.saved = string(buf)
						}
						e.histIdx--
						buf = []rune(e.history[e.histIdx])
						cursor = len(buf)
					}
				case 'B':
					if e.histIdx < len(e.history) {
						e.histIdx++
						if e.histIdx == len(e.history) {
							buf = []rune(e.saved)
						} else {
							buf = []rune(e.history[e.histIdx])
						}
						cursor = len(buf)
					}
				case 'C':
					if cursor < len(buf) {
						cursor++
					}
				case 'D':
					if cursor > 0 {
						cursor--
					}
				}
			}

		case pasting:
			var b strings.Builder
			b.WriteRune(r)
			rest, ended := drainPaste()
			b.WriteString(rest)
			pasted = append(pasted, normalizePasted(b.String()))
			if ended {
				pasting = false
				consumePasteEnd()
			}

		case r == 9:
			if len(pasted) == 0 {
				if c := slashComplete(e.completer, string(buf)); c != "" {
					buf = []rune(c)
					cursor = len(buf)
				}
			}

		case r >= 32:
			e.searching = false
			buf = append(buf[:cursor], append([]rune{r}, buf[cursor:]...)...)
			cursor++
		}

		redraw()
	}
}

func (e *lineEditor) History() []string { return e.history }

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

func indexAtWidth(s string, target int) (int, int) {
	w, idx := 0, 0
	for _, r := range s {
		cw := runewidth.RuneWidth(r)
		if w+cw > target {
			return idx, w
		}
		w += cw
		idx++
	}
	return idx, w
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

func readSGRMouse(in *bufio.Reader) (button, x, y int, ok bool) {
	var b strings.Builder
	for {
		r, _, err := in.ReadRune()
		if err != nil {
			return 0, 0, 0, false
		}
		if r == 'M' || r == 'm' {
			break
		}
		b.WriteRune(r)
	}
	parts := strings.Split(b.String(), ";")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	for i, p := range []*int{&button, &x, &y} {
		n := 0
		for _, ch := range parts[i] {
			if ch < '0' || ch > '9' {
				return 0, 0, 0, false
			}
			n = n*10 + int(ch-'0')
		}
		*p = n
	}
	return button, x, y, true
}
