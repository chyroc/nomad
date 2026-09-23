package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

type statusPart struct {
	plain  string
	styled string
}

func (a *App) statusParts(width int) []statusPart {
	dir := a.paths.Workspace
	if h := os.Getenv("HOME"); h != "" && strings.HasPrefix(dir, h) {
		rel := strings.TrimPrefix(dir, h)
		if rel == "" || rel == "/" {
			dir = "~"
		} else {
			dir = "~/" + strings.TrimPrefix(rel, "/")
		}
	} else {
		dir = filepath.Base(dir)
	}
	parts := []statusPart{
		{plain: a.model, styled: a.style(cCyan, a.model)},
		{plain: "effort:" + a.effortLabel(), styled: a.style(cDim, "effort:"+a.effortLabel())},
	}
	if br := gitBranch(a.paths.Workspace); br != "" {
		parts = append(parts, statusPart{plain: "⎇ " + br, styled: a.style(cGreen, "⎇ "+br)})
	}
	if a.sessionID != "" {
		parts = append(parts, statusPart{plain: shortID(a.sessionID), styled: a.style(cDim, shortID(a.sessionID))})
	}
	dirPart := statusPart{plain: dir, styled: a.style(cDim, dir)}

	fixed := 0
	for _, p := range parts {
		fixed += runewidth.StringWidth(p.plain)
	}
	seps := len(parts) // separators between parts and before dir
	if width > 0 {
		room := width - 1 - fixed - seps*3
		if w := runewidth.StringWidth(dir); room <= 0 {
			dir = ""
		} else if w > room {
			dir = runewidth.Truncate(dir, room, "…")
			dirPart = statusPart{plain: dir, styled: a.style(cDim, dir)}
		}
	}
	if dir != "" {
		parts = append(parts, dirPart)
	}
	return parts
}

func (a *App) effortLabel() string {
	if a.opts.ReasoningEffort == "" {
		return "max"
	}
	return a.opts.ReasoningEffort
}

func (a *App) printStatusline() {
	width := 0
	if a.editor != nil && a.editor.fd > 0 {
		if _, w, err := term.GetSize(a.editor.fd); err == nil && w > 0 {
			width = w
		}
	}
	parts := a.statusParts(width)
	sep := a.style(cDim, " · ")
	styled := make([]string, 0, len(parts))
	for _, p := range parts {
		styled = append(styled, p.styled)
	}
	a.printf("%s%s%s\n", cDim, strings.Join(styled, sep), cReset)
}
