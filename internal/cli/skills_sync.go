package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/contextinfo"
)

// discoverLocalSkills finds skills across the standard directories.
func (a *App) discoverLocalSkills() []contextinfo.DiscoveredSkill {
	home, _ := os.UserHomeDir()
	return contextinfo.DiscoverSkills(home, a.paths.Workspace, a.paths.SkillsDir())
}

// syncSkills runs the consent -> multi-select -> upload -> bind -> symlink
// flow. selected may list skill names pre-approved non-interactively; if
// nonInteractive, no TUI prompt is shown and only those names are uploaded.
func (a *App) syncSkills(ctx context.Context, preSelected []string, nonInteractive bool) error {
	discovered := a.discoverLocalSkills()
	if len(discovered) == 0 {
		return fmt.Errorf("no skills found in the standard skill directories")
	}

	var chosen []contextinfo.DiscoveredSkill
	byName := map[string]contextinfo.DiscoveredSkill{}
	for _, d := range discovered {
		byName[d.Name] = d
	}

	if nonInteractive {
		if len(preSelected) == 0 {
			return fmt.Errorf("non-interactive skill sync requires explicit --sync-skills <name1,name2,...> (user consent)")
		}
		for _, name := range preSelected {
			d, ok := byName[strings.TrimSpace(name)]
			if !ok {
				return fmt.Errorf("skill %q not found", name)
			}
			chosen = append(chosen, d)
		}
	} else {
		approved, err := a.multiSelectSkills(discovered)
		if err != nil {
			return err
		}
		if len(approved) == 0 {
			a.printf("%sNo skills selected; nothing uploaded.%s\n", cDim, cReset)
			return nil
		}
		chosen = approved
	}

	if !nonInteractive {
		a.printf("%sAbout to upload ONLY name+description (no SKILL.md body) for %d skill(s) and bind them to agent %s.%s\n",
			cYellow, len(chosen), a.ctrl.Profile.AgentID, cReset)
		a.printf("%sProceed? type y to confirm, anything else cancels:%s\n", cBold, cReset)
		line, _ := a.editor.ReadLine("> ", readLineOptions{})
		if !isYes(line) {
			a.printf("%sAborted; no data uploaded.%s\n", cDim, cReset)
			return nil
		}
	}

	var bindings []ark.SkillBinding
	for _, d := range chosen {
		a.printf("%suploading %s metadata…%s\n", cDim, d.Name, cReset)
		b, err := a.ctrl.Client.RegisterSkill(ctx, d.Name, d.Description)
		if err != nil {
			return fmt.Errorf("upload skill %s: %w", d.Name, err)
		}
		b.LocalDir = d.Dir
		bindings = append(bindings, b)
		a.printf("%s✓ registered %s (metadata only)%s\n", cGreen, d.Name, cReset)

		a.printf("%slinking %s into workspace…%s\n", cDim, d.Name, cReset)
		if err := ark.LinkLocalSkill(a.paths.Workspace, d.Name, d.Dir); err != nil {
			return fmt.Errorf("link local skill %s: %w", d.Name, err)
		}
	}

	existing := a.ctrl.Profile.SkillBindings
	merged := mergeSkillBindings(existing, bindings)
	a.printf("%sbinding %d skill(s) to agent…%s\n", cDim, len(merged), cReset)
	if err := a.ctrl.Client.BindSkillsToAgent(ctx, a.ctrl.Profile.AgentID, merged); err != nil {
		return fmt.Errorf("bind skills to agent: %w", err)
	}
	a.ctrl.Profile.SkillBindings = merged
	if err := a.ctrl.SaveProfile(); err != nil {
		return err
	}
	a.printf("%s✓ %d skill(s) bound to agent; local bodies linked under %s%s\n",
		cGreen, len(bindings), ark.SkillLinkDir(a.paths.Workspace), cReset)
	return nil
}

// multiSelectSkills shows a checkbox picker over discovered skills,
// pre-checking skills already bound to the agent.
func (a *App) multiSelectSkills(discovered []contextinfo.DiscoveredSkill) ([]contextinfo.DiscoveredSkill, error) {
	bound := map[string]bool{}
	for _, b := range a.ctrl.Profile.SkillBindings {
		bound[b.Name] = true
	}
	items := make([]pickItem, 0, len(discovered))
	var preChecked []string
	for _, d := range discovered {
		desc := strings.ReplaceAll(d.Description, "\n", " ")
		items = append(items, pickItem{id: d.Name, label: d.Name, desc: truncate(desc, 60)})
		if bound[d.Name] {
			preChecked = append(preChecked, d.Name)
		}
	}
	pk := newPickerFull(a.in, a.out, items, 0,
		"Sync skills",
		"Only name+description are uploaded; SKILL.md bodies stay local.",
		nil, 0).withMultiSelect(preChecked)
	ids, ok := pk.RunMulti()
	if !ok {
		return nil, nil
	}
	byName := map[string]contextinfo.DiscoveredSkill{}
	for _, d := range discovered {
		byName[d.Name] = d
	}
	var out []contextinfo.DiscoveredSkill
	for _, id := range ids {
		if d, ok := byName[id]; ok {
			out = append(out, d)
		}
	}
	return out, nil
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes" || s == "allow"
}

func mergeSkillBindings(existing, fresh []ark.SkillBinding) []ark.SkillBinding {
	byID := map[string]ark.SkillBinding{}
	var order []string
	add := func(b ark.SkillBinding) {
		key := b.Name
		if _, ok := byID[key]; !ok {
			order = append(order, key)
		}
		byID[key] = b
	}
	for _, b := range existing {
		add(b)
	}
	for _, b := range fresh {
		add(b)
	}
	out := make([]ark.SkillBinding, 0, len(order))
	for _, k := range order {
		out = append(out, byID[k])
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// relinkBoundSkills recreates workspace symlinks for skills already bound
// to the agent, matching each binding to a currently-discovered local dir
// when possible and falling back to the stored LocalDir.
func (a *App) relinkBoundSkills() {
	local := map[string]string{}
	for _, d := range a.discoverLocalSkills() {
		local[d.Name] = d.Dir
	}
	for _, b := range a.ctrl.Profile.SkillBindings {
		dir := b.LocalDir
		if d, ok := local[b.Name]; ok {
			dir = d
		}
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		_ = ark.LinkLocalSkill(a.paths.Workspace, b.Name, dir)
	}
}

// boundSkillLocalHint renders a system-prompt section telling the agent
// that bound skills are locally executable and where to read their body.
func (a *App) boundSkillLocalHint() string {
	if len(a.ctrl.Profile.SkillBindings) == 0 {
		return ""
	}
	root := ark.SkillLinkDir(a.paths.Workspace)
	var b strings.Builder
	b.WriteString("# Bound skills (local execution)\n")
	b.WriteString("The following skills are registered with the platform for discovery, but their ")
	b.WriteString("instruction bodies live on this machine. When a bound skill is triggered or the user ")
	b.WriteString("invokes it, read its real SKILL.md from the local path with the read tool and follow it. ")
	b.WriteString("Do not expect the platform package to contain the body.\n")
	for _, bd := range a.ctrl.Profile.SkillBindings {
		fmt.Fprintf(&b, "- %s: %s\n", bd.Name, filepath.Join(root, bd.Name, "SKILL.md"))
	}
	return b.String()
}
