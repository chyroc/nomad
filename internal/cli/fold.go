package cli

import (
	"fmt"
	"strings"
)

const (
	foldHead = 6
	foldTail = 2
	foldMin  = foldHead + foldTail + 3
)

type foldBlock struct {
	id     int
	header string
	lines  []string
}

func foldLines(header, body string) (head []string, tail []string, more int, ok bool) {
	all := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(all) <= foldMin {
		return all, nil, 0, false
	}
	return all[:foldHead], all[len(all)-foldTail:], len(all) - foldHead - foldTail, true
}

func foldBar(more int) string {
	return fmt.Sprintf("\x1b[2m\x1b[4m  … %d more lines — press Ctrl+O to expand  \x1b[0m", more)
}
