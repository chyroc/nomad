package cli

import "fmt"

// askPermissionChoice is the inline tool permission selector.
// Returns one of: allow / session / deny. It renders a few lines
// above the input and erases them on exit (no alternate screen).
func (a *App) askPermissionChoice(name, args string) string {
	a.stopSpinner()
	return a.withModal(func() string {
		items := []pickItem{
			{id: "allow", label: "Allow once", desc: fmt.Sprintf("run %s this time", name)},
			{id: "session", label: "Allow for session", desc: fmt.Sprintf("auto-allow %s until restart", name)},
			{id: "deny", label: "Deny", desc: "refuse and report back to the model"},
		}
		pk := newPickerFull(a.in, a.out, items, 0,
			"Allow tool "+name,
			args,
			nil, 0)
		id, ok := pk.Run()
		if !ok || id == "" {
			return "deny"
		}
		return id
	})
}
