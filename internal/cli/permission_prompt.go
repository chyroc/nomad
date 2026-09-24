package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/chyroc/nomad/internal/settings"
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
		}
		if rule, ok := a.alwaysAllowRule(name, argsJSON); ok {
			items = append(items, pickItem{
				id:    "always",
				label: "Always allow " + rule.String(),
				desc:  "save to user settings and never ask again",
			})
		}
		items = append(items, pickItem{id: "deny", label: "Deny", desc: "refuse and report back to the model"})
		pk := newPickerFull(a.in, a.out, items, 0,
			"Allow tool "+name,
			sub,
			nil, 0)
		id, ok := pk.Run()
		if !ok || id == "" {
			return "deny"
		}
		if id == "always" {
			rule, ok := a.alwaysAllowRule(name, argsJSON)
			if !ok {
				return "deny"
			}
			if err := a.saveUserAllowRule(rule); err != nil {
				a.printf("%sfailed to save rule: %v%s\n", cRed, err, cReset)
				return "deny"
			}
			a.printf("%ssaved rule %s%s\n", cGreen, rule.String(), cReset)
			return "allow"
		}
		return id
	})
}

// alwaysAllowRule builds the persistent rule for a tool: a bare rule
// for file tools, a two-token bash(prefix*) rule for shell commands.
func (a *App) alwaysAllowRule(name, argsJSON string) (settings.Rule, bool) {
	if name != "bash" {
		return settings.ParseRule(name)
	}
	var parsed struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil {
		return settings.Rule{}, false
	}
	cmd := strings.TrimSpace(firstNonEmptyStr(parsed.Command, parsed.Cmd))
	if cmd == "" {
		return settings.Rule{}, false
	}
	fields := strings.Fields(cmd)
	prefix := fields[0]
	if len(fields) >= 2 {
		prefix = fields[0] + " " + fields[1]
	}
	return settings.ParseRule(fmt.Sprintf("bash(%s*)", prefix))
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
