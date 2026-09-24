package cli

import (
	"fmt"
	"strings"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/settings"
)

// saveUserAllowRule appends an allow rule to the user settings file,
// updates the merged in-memory settings and the live runner.
func (a *App) saveUserAllowRule(rule settings.Rule) error {
	allow, deny := a.userRuleStrings()
	if containsString(allow, rule.String()) {
		return nil
	}
	allow = append(allow, rule.String())
	if err := settings.SaveUserRules(a.paths.UserSettingsFile(), allow, deny); err != nil {
		return err
	}
	a.applyUserRules(allow, deny)
	if r, ok := a.runner.(*ark.Runner); ok && r != nil {
		r.AddAllowRule(rule)
	}
	return nil
}

func (a *App) userRuleStrings() (allow, deny []string) {
	if a.appSettings != nil && a.appSettings.User != nil {
		allow = append(allow, a.appSettings.User.AllowStrings()...)
		deny = append(deny, a.appSettings.User.DenyStrings()...)
	}
	return allow, deny
}

func (a *App) applyUserRules(allow, deny []string) {
	loaded, err := settings.Load(a.paths.UserSettingsFile(), a.paths.ProjectSettingsFile(),
		a.paths.CompatSettingsFile())
	if err != nil {
		a.printf("%sreload settings failed: %v%s\n", cRed, err, cReset)
		return
	}
	a.appSettings = loaded
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// cmdPermissionRules manages persistent allow/deny rules.
func (a *App) cmdPermissionRules(arg string) error {
	sub, rest := split2(arg)
	if sub == "" {
		a.printPermissionRules()
		return nil
	}
	switch sub {
	case "list":
		a.printPermissionRules()
	case "allow":
		return a.addUserRule(rest, true)
	case "deny":
		return a.addUserRule(rest, false)
	case "remove":
		return a.removeUserRules()
	default:
		return fmt.Errorf("usage: /permissions [list|allow <rule>|deny <rule>|remove]")
	}
	return nil
}

func split2(s string) (string, string) {
	s = trimSpace(s)
	for i, c := range s {
		if c == ' ' || c == '\t' {
			return s[:i], trimSpace(s[i+1:])
		}
	}
	return s, ""
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func (a *App) printPermissionRules() {
	if a.appSettings == nil {
		a.printf("(no settings loaded)\n")
		return
	}
	groups := []struct {
		title string
		file  *settings.SourcedFile
	}{
		{"user", a.appSettings.User},
		{"project", a.appSettings.Project},
	}
	for _, g := range groups {
		if g.file == nil {
			continue
		}
		a.printf("%s[%s]%s %s\n", cBold, g.title, cReset, g.file.Path)
		for _, r := range g.file.AllowStrings() {
			a.printf("  %sallow%s %s\n", cGreen, cReset, r)
		}
		for _, r := range g.file.DenyStrings() {
			a.printf("  %sdeny%s  %s\n", cRed, cReset, r)
		}
	}
	for _, f := range a.appSettings.Compat {
		a.printf("%s[compat]%s %s\n", cBold, cReset, f.Path)
		for _, r := range f.AllowStrings() {
			a.printf("  %sallow%s %s\n", cGreen, cReset, r)
		}
		for _, r := range f.DenyStrings() {
			a.printf("  %sdeny%s  %s\n", cRed, cReset, r)
		}
	}
	if a.appSettings.DefaultMode != "" {
		a.printf("default mode: %s\n", a.appSettings.DefaultMode)
	}
}

func (a *App) addUserRule(raw string, allow bool) error {
	rule, ok := settings.ParseRule(raw)
	if !ok {
		return fmt.Errorf("invalid rule %q (e.g. read or bash(git status*))", raw)
	}
	userAllow, userDeny := a.userRuleStrings()
	if allow {
		if !containsString(userAllow, rule.String()) {
			userAllow = append(userAllow, rule.String())
		}
	} else {
		if !containsString(userDeny, rule.String()) {
			userDeny = append(userDeny, rule.String())
		}
	}
	if err := settings.SaveUserRules(a.paths.UserSettingsFile(), userAllow, userDeny); err != nil {
		return err
	}
	a.applyUserRules(userAllow, userDeny)
	a.printf("%ssaved rule %s%s\n", cGreen, rule.String(), cReset)
	return nil
}

func (a *App) removeUserRules() error {
	userAllow, userDeny := a.userRuleStrings()
	if len(userAllow)+len(userDeny) == 0 {
		a.printf("(no user rules)\n")
		return nil
	}
	items := make([]pickItem, 0, len(userAllow)+len(userDeny))
	for _, r := range userAllow {
		items = append(items, pickItem{id: "allow:" + r, label: r, tag: "allow"})
	}
	for _, r := range userDeny {
		items = append(items, pickItem{id: "deny:" + r, label: r, tag: "deny"})
	}
	pk := newPickerFull(a.in, a.out, items, 0,
		"Remove rules", "Space toggles, Enter confirms, Esc cancels.", nil, 0).withMultiSelect(nil)
	removed, ok := pk.RunMulti()
	if !ok || len(removed) == 0 {
		return nil
	}
	skip := map[string]bool{}
	for _, id := range removed {
		skip[strings.TrimPrefix(strings.TrimPrefix(id, "allow:"), "deny:")] = true
	}
	newAllow, newDeny := userAllow[:0], userDeny[:0]
	for _, r := range userAllow {
		if !skip[r] {
			newAllow = append(newAllow, r)
		}
	}
	for _, r := range userDeny {
		if !skip[r] {
			newDeny = append(newDeny, r)
		}
	}
	if err := settings.SaveUserRules(a.paths.UserSettingsFile(), newAllow, newDeny); err != nil {
		return err
	}
	a.applyUserRules(newAllow, newDeny)
	a.printf("%sremoved %d rule(s)%s\n", cGreen, len(removed), cReset)
	return nil
}
