package contextinfo

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	importMaxDepth = 5
	importMaxBytes = 1 << 20
	importProbeLen = 8192
)

type importExpander struct {
	home      string
	stack     map[string]bool
	firstPath map[string]string
	budget    int
}

func newImportExpander(home string) *importExpander {
	return &importExpander{
		home:      home,
		stack:     map[string]bool{},
		firstPath: map[string]string{},
		budget:    importMaxBytes,
	}
}

// expand resolves @path references in an instruction file, inlining the
// referenced text files. Fenced code blocks and inline code are never
// scanned.
func (e *importExpander) expand(content, baseDir string, depth int) string {
	var out strings.Builder
	lines := strings.Split(content, "\n")
	inFence := false
	fenceMarker := ""
	for _, line := range lines {
		if marker, ok := fenceStart(line); ok {
			if !inFence {
				inFence = true
				fenceMarker = marker
			} else if marker == fenceMarker {
				inFence = false
				fenceMarker = ""
			}
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		if inFence {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		out.WriteString(e.expandLine(line, baseDir, depth))
		out.WriteByte('\n')
	}
	return strings.TrimRight(out.String(), "\n")
}

func fenceStart(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "```") {
		return "```", true
	}
	if strings.HasPrefix(trimmed, "~~~") {
		return "~~~", true
	}
	return "", false
}

func (e *importExpander) expandLine(line, baseDir string, depth int) string {
	masked := maskInlineCode(line)
	var out strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] != '@' || masked[i] != '@' {
			out.WriteByte(line[i])
			continue
		}
		token, next := scanImportToken(line, i)
		if token == "" || !importPositionOK(line, i) {
			out.WriteByte(line[i])
			continue
		}
		path, ok := resolveImportPath(token, baseDir, e.home)
		if !ok {
			out.WriteByte(line[i])
			continue
		}
		included, ok := e.includePath(path, depth)
		if !ok {
			out.WriteString(line[i:next])
			i = next - 1
			continue
		}
		out.WriteString(included)
		i = next - 1
	}
	return out.String()
}

// maskInlineCode returns a copy where bytes inside backtick code spans
// are replaced with spaces, keeping @ positions outside spans intact.
func maskInlineCode(line string) string {
	buf := []byte(line)
	inSpan := false
	for i := 0; i < len(buf); i++ {
		if buf[i] == '`' {
			inSpan = !inSpan
			continue
		}
		if inSpan {
			buf[i] = ' '
		}
	}
	return string(buf)
}

func scanImportToken(line string, at int) (string, int) {
	i := at + 1
	start := i
	for i < len(line) {
		c := line[i]
		if c == '#' || isImportTokenChar(c) {
			i++
			continue
		}
		break
	}
	token := line[start:i]
	if idx := strings.IndexByte(token, '#'); idx >= 0 {
		token = token[:idx]
	}
	if !looksLikeImportPath(token) {
		return "", i
	}
	return token, i
}

func isImportTokenChar(c byte) bool {
	return c == '/' || c == '.' || c == '-' || c == '_' || c == '~' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func looksLikeImportPath(token string) bool {
	return strings.HasPrefix(token, "~/") ||
		strings.HasPrefix(token, "/") ||
		strings.HasPrefix(token, "./") ||
		strings.HasPrefix(token, "../") ||
		strings.Contains(token, "/")
}

func importPositionOK(line string, at int) bool {
	if at == 0 {
		return true
	}
	prev := line[at-1]
	if prev == ' ' || prev == '\t' || prev == '(' || prev == '[' {
		return true
	}
	if prev == '-' || prev == '>' {
		return at >= 2 && line[at-2] == ' '
	}
	return false
}

func resolveImportPath(token, baseDir, home string) (string, bool) {
	path := token
	switch {
	case strings.HasPrefix(path, "~/"):
		if home == "" {
			return "", false
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	case strings.HasPrefix(path, "/"):
	case strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../"):
		path = filepath.Join(baseDir, path)
	default:
		if !strings.Contains(path, "/") {
			return "", false
		}
		path = filepath.Join(baseDir, path)
	}
	return filepath.Clean(path), true
}

func (e *importExpander) includePath(path string, depth int) (string, bool) {
	if depth >= importMaxDepth || e.budget <= 0 {
		return "", false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	if e.stack[real] {
		return "", false
	}
	cleanPath := filepath.Clean(path)
	if first, ok := e.firstPath[real]; ok && first != cleanPath {
		return "", false
	}
	data, ok := readTextFile(path)
	if !ok || !isLikelyText(data) {
		return "", false
	}
	if _, ok := e.firstPath[real]; !ok {
		e.firstPath[real] = cleanPath
	}
	e.stack[real] = true
	defer delete(e.stack, real)
	expanded := e.expand(data, filepath.Dir(real), depth+1)
	if len(expanded) > e.budget {
		expanded = expanded[:e.budget]
	}
	e.budget -= len(expanded)
	return expanded, true
}

func isLikelyText(content string) bool {
	probe := content
	if len(probe) > importProbeLen {
		probe = probe[:importProbeLen]
	}
	if strings.IndexByte(probe, 0) >= 0 {
		return false
	}
	if !utf8.ValidString(probe) {
		return false
	}
	return true
}

// expandFile resolves imports in a top-level instruction file.
func (e *importExpander) expandFile(path, content string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	e.stack[real] = true
	defer delete(e.stack, real)
	return e.expand(content, filepath.Dir(real), 0)
}
