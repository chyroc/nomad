package cli

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// statusBar renders the two pinned status rows that live directly
// under the input frame's bottom rule.
type statusBar struct {
	clock func() time.Time
}

func newStatusBar() *statusBar {
	return &statusBar{clock: time.Now}
}

// render returns the two status rows truncated to fit one cell short
// of the width, counting East Asian ambiguous runes as two cells so
// the rows cannot wrap on any terminal.
func (b *statusBar) render(a *App, width int) (string, string) {
	if width < 8 {
		width = 8
	}
	return truncateStatusRow(b.infoLine(a), width-2), truncateStatusRow(b.hintLine(a), width-2)
}

func truncateStatusRow(s string, w int) string {
	if displayWidth(s) <= w {
		return s
	}
	return truncateDisplayWidth(s, w-1) + "…"
}

func (b *statusBar) infoLine(a *App) string {
	var parts []string
	parts = append(parts, repoSegment(a.paths.Workspace))
	if seg := gitSegment(a.paths.Workspace); seg != "" {
		parts = append(parts, a.style(cGreen, seg))
	}
	parts = append(parts, a.style(cCyan, modelShort(a.model))+a.style(cDim, "["+a.effortLabel()+"]"))
	parts = append(parts, a.style(cDim, "ark"))
	if a.goal != nil && a.goal.Active() {
		parts = append(parts, a.style(cGreen, "◉ goal "+strconv.Itoa(a.goal.Iterations)))
	}
	return a.style(cDim, "["+b.clock().Format("15:04")+"]") + " " + strings.Join(parts, a.style(cDim, " | "))
}

func (b *statusBar) hintLine(a *App) string {
	return permissionModeHint(a, a.effectivePermissionMode()) + a.style(cDim, " · ← for agents")
}

func permissionModeHint(a *App, mode string) string {
	label := "manual mode"
	switch mode {
	case "bypassPermissions":
		label = "bypass permissions on"
	case "acceptEdits":
		label = "accept edits on"
	case "plan":
		label = "plan mode on"
	}
	return a.style(cRed, "⏵⏵ "+label+" (shift+tab to cycle)")
}

func modelShort(id string) string {
	return strings.TrimPrefix(id, "doubao-")
}

func repoSegment(dir string) string {
	base := filepath.Base(dir)
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "" || parent == "." || parent == string(filepath.Separator) {
		return base
	}
	return parent + "/" + base
}

func gitSegment(dir string) string {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--branch").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	head := strings.TrimPrefix(lines[0], "## ")
	branch := head
	if i := strings.Index(branch, "..."); i >= 0 {
		branch = branch[:i]
	}
	if i := strings.IndexAny(branch, " "); i >= 0 {
		branch = branch[:i]
	}
	if branch == "" {
		return ""
	}
	if len(lines) > 1 {
		return branch + "+"
	}
	return branch
}

func (a *App) effortLabel() string {
	if a.opts.ReasoningEffort == "" {
		return "max"
	}
	return a.opts.ReasoningEffort
}
