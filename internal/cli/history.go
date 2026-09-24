package cli

import (
	"bufio"
	"os"
	"strings"
)

const maxHistoryEntries = 1000

// encodeHistoryEntry escapes backslashes and newlines so one entry fits
// on one physical line.
func encodeHistoryEntry(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// decodeHistoryEntry reverses encodeHistoryEntry.
func decodeHistoryEntry(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// loadHistory reads persisted entries from path; a missing file yields
// no entries and no error.
func loadHistory(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, decodeHistoryEntry(line))
	}
	return out
}

// appendHistoryEntry persists one submitted line, deduping against the
// previous entry and rewriting atomically once the cap is exceeded.
func appendHistoryEntry(path string, history []string, entry string) []string {
	entry = strings.TrimRight(entry, "\n")
	if strings.TrimSpace(entry) == "" {
		return history
	}
	if len(history) > 0 && history[len(history)-1] == entry {
		return history
	}
	history = append(history, entry)
	if len(history) <= maxHistoryEntries {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(encodeHistoryEntry(entry) + "\n")
			_ = f.Close()
		}
		return history
	}
	history = history[len(history)-maxHistoryEntries:]
	rewriteHistory(path, history)
	return history
}

func rewriteHistory(path string, history []string) {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	for _, h := range history {
		_, _ = f.WriteString(encodeHistoryEntry(h) + "\n")
	}
	_ = f.Close()
	_ = os.Rename(tmp, path)
}
