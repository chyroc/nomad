package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (a *App) statusline() string {
	var parts []string
	parts = append(parts, a.style(cBold, modelShort(a.model)))
	parts = append(parts, a.style(cDim, "effort:"+a.effortLabel()))
	if br := gitBranch(a.paths.Workspace); br != "" {
		parts = append(parts, a.style(cGreen, "⎇ "+br))
	}
	if a.sessionID != "" {
		parts = append(parts, a.style(cDim, shortID(a.sessionID)))
	}
	if a.goal != nil && a.goal.Active() {
		parts = append(parts, a.style(cGreen, "◉ goal "+strconv.Itoa(a.goal.Iterations)))
	}
	parts = append(parts, a.style(cDim, a.workdirShort()))
	sep := a.style(cDim, " · ")
	return strings.Join(parts, sep)
}

func modelShort(id string) string {
	name := strings.TrimPrefix(id, "doubao-")
	return name
}

func (a *App) workdirShort() string {
	dir := a.paths.Workspace
	if h := os.Getenv("HOME"); h != "" && strings.HasPrefix(dir, h) {
		rel := strings.TrimPrefix(dir, h)
		if rel == "" || rel == "/" {
			return "~"
		}
		return "~" + rel
	}
	return filepath.Base(dir)
}

func (a *App) modeLine() string {
	mode := a.opts.PermissionMode
	switch mode {
	case "bypassPermissions":
		return a.style(cYellow, "⏵ bypass permissions on")
	case "acceptEdits":
		return a.style(cYellow, "⏵ accept edits on")
	case "plan":
		return a.style(cYellow, "⏵ plan mode on")
	default:
		return a.style(cDim, "⏵ manual mode · Enter confirms prompts")
	}
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
