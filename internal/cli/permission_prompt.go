package cli

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// askPermissionChoice is the inline tool permission selector.
// Returns one of: allow / session / deny. It renders a few lines
// above the input and erases them on exit (no alternate screen).
func (a *App) askPermissionChoice(name, argsJSON string) string {
	return a.withModal(func() string {
		sub := argsJSON
		if pretty := toolInvocation(name, argsJSON, 100); pretty != "" {
			sub = ansi.Strip(pretty)
		}
		items := []pickItem{
			{id: "allow", label: "Allow once", desc: fmt.Sprintf("run %s this time", name)},
			{id: "session", label: "Allow for session", desc: fmt.Sprintf("auto-allow %s until restart", name)},
			{id: "deny", label: "Deny", desc: "refuse and report back to the model"},
		}
		pk := newPickerFull(a.in, a.out, items, 0,
			"Allow tool "+name,
			sub,
			nil, 0)
		id, ok := pk.Run()
		if !ok || id == "" {
			return "deny"
		}
		return id
	})
}
