package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

var ErrInterrupt = errors.New("interrupt")

type lineEditor struct {
	in        *bufio.Reader
	out       io.Writer
	fd        int
	history   []string
	histIdx   int
	saved     string
	completer func(line string) []string
	onMouse   func(button, x, y int) bool
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
	if e.fd < 0 {
		return 80
	}
	if _, w, err := term.GetSize(e.fd); err == nil && w > 0 {
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
		term.Restore(e.fd, old)
	}
	defer restore()

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

	drainPaste := func() string {
		var b strings.Builder
		for {
			n := e.in.Buffered()
			if n == 0 {
				break
			}
			buf2 := make([]byte, n)
			nn, _ := e.in.Read(buf2)
			b.Write(buf2[:nn])
		}
		return b.String()
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
				rest := drainPaste()
				pasted = append(pasted, normalizePasted(rest))
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

		case r == 21:
			buf, pasted, cursor = nil, nil, 0

		case r == 11:
			buf, pasted = buf[:cursor], nil

		case r == 1:
			cursor = 0

		case r == 5:
			cursor = len(buf)

		case r == 15:
			if e.onMouse != nil {
				e.onMouse(-1, 0, 0)
			}

		case r == 27:
			r2, _, err2 := e.in.ReadRune()
			if err2 != nil {
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
					rest := make([]rune, 0, 3)
					rest = append(rest, mustRune(e.in), mustRune(e.in), mustRune(e.in))
					switch string(rest) {
					case "00~":
						pasting = true
						continue
					case "01~":
						pasting = false
						continue
					}
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
			b.WriteString(drainPaste())
			pasted = append(pasted, normalizePasted(b.String()))

		case r == 9:
			if len(pasted) == 0 {
				if c := slashComplete(e.completer, string(buf)); c != "" {
					buf = []rune(c)
					cursor = len(buf)
				}
			}

		case r >= 32:
			buf = append(buf[:cursor], append([]rune{r}, buf[cursor:]...)...)
			cursor++
		}

		redraw()
	}
}

func (e *lineEditor) History() []string { return e.history }

func mustRune(in *bufio.Reader) rune {
	r, _, _ := in.ReadRune()
	return r
}

func normalizePasted(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.TrimRight(s, "\n")
}

func visiblePromptWidth(s string) int {
	return runewidth.StringWidth(stripANSI(s))
}

func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 1
		if j < len(s) {
			switch s[j] {
			case '[', '?':
				j++
				for j < len(s) && !isCSIFinal(s[j]) {
					j++
				}
				if j < len(s) {
					j++
				}
			case ']':
				j++
				for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
					j++
				}
				if j < len(s) {
					if s[j] == 0x1b {
						j += 2
					} else {
						j++
					}
				}
			default:
				j++
			}
		}
		i = j
	}
	return b.String()
}

func isCSIFinal(c byte) bool { return c >= 0x40 && c <= 0x7e }

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
	w := 0
	var b strings.Builder
	for _, r := range s {
		cw := runewidth.RuneWidth(r)
		if w+cw > width {
			break
		}
		b.WriteRune(r)
		w += cw
	}
	return b.String()
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
		out[i] = stripANSI(s)
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
