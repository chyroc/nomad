package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

func (a *App) statusline() string {
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
	parts := []string{
		a.style(cCyan, a.model),
		a.style(cDim, "effort:"+a.effortLabel()),
	}
	if br := gitBranch(a.paths.Workspace); br != "" {
		parts = append(parts, a.style(cGreen, "⎇ "+br))
	}
	parts = append(parts, a.style(cDim, dir))
	if a.sessionID != "" {
		parts = append(parts, a.style(cDim, shortID(a.sessionID)))
	}
	return strings.Join(parts, a.style(cDim, " · "))
}

func (a *App) effortLabel() string {
	if a.opts.ReasoningEffort == "" {
		return "max"
	}
	return a.opts.ReasoningEffort
}

func (a *App) printStatusline() {
	width := 80
	if a.editor != nil && a.editor.fd > 0 {
		if _, w, err := term.GetSize(a.editor.fd); err == nil && w > 0 {
			width = w
		}
	}
	line := runewidth.Truncate(a.statusline(), width-1, "…")
	a.printf("%s%s%s\n", cDim, line, cReset)
}
