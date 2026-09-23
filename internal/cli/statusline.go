package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
	var parts []string
	parts = append(parts, a.style(cCyan, a.model))
	parts = append(parts, a.style(cDim, "effort:"+a.effortLabel()))
	if br := gitBranch(a.paths.Workspace); br != "" {
		parts = append(parts, a.style(cGreen, "⎇ "+br))
	}
	if a.sessionID != "" {
		parts = append(parts, a.style(cDim, shortID(a.sessionID)))
	}
	parts = append(parts, a.style(cDim, dir))
	sep := a.style(cDim, " · ")
	return strings.Join(parts, sep)
}

func (a *App) effortLabel() string {
	if a.opts.ReasoningEffort == "" {
		return "max"
	}
	return a.opts.ReasoningEffort
}

func (a *App) printStatusline() {
	width, _ := cachedTermSize()
	line := ansi.Truncate(a.statusline(), width-1, "…")
	a.printf("%s\n", line)
}
