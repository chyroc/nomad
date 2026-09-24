package cli

import "strings"

// printResumeHint shows how to resume the current session when the TUI
// exits with at least one turn on record.
func (a *App) printResumeHint() {
	id := strings.TrimSpace(a.sessionID)
	if id == "" {
		return
	}
	a.printf("\n%sResume this session with:%s\n", cBold, cReset)
	a.printf("  nomad --resume %s\n", id)
	a.printf("%s(or run `nomad -c` for the most recent session, or /resume inside the TUI)%s\n", cDim, cReset)
}
