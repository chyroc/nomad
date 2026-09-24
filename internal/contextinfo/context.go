// Package contextinfo loads project/global context injected into the
// agent: global instruction files, workspace instruction hierarchy and
// skills.
package contextinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Skill is an on-demand instruction package.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// Bundle is the assembled extra context for a session.
type Bundle struct {
	GlobalFiles  []InstructionFile
	ProjectFiles []InstructionFile
	Skills       []Skill
}

// InstructionFile is one loaded global or project instruction file.
type InstructionFile struct {
	Path    string
	Private bool
	Content string
}

// Load reads global instruction files and the project instruction
// hierarchy from the workspace up to the filesystem root, outermost
// directories first.
func Load(globalDir, home, workspace string) Bundle {
	var b Bundle
	b.GlobalFiles = loadGlobalFiles(globalDir)
	b.ProjectFiles = loadProjectFiles(workspace)
	return b
}

func loadGlobalFiles(globalDir string) []InstructionFile {
	candidates := []InstructionFile{
		{Path: filepath.Join(globalDir, "NOMAD.md")},
		{Path: filepath.Join(globalDir, "NOMAD.local.md"), Private: true},
		{Path: filepath.Join(globalDir, "MEMORY.md")},
	}
	return dedupInstructionFiles(readExisting(candidates))
}

func loadProjectFiles(workspace string) []InstructionFile {
	var dirs []string
	for dir := filepath.Clean(workspace); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	var picked []InstructionFile
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		picked = append(picked, firstExisting([]InstructionFile{
			{Path: filepath.Join(dir, "NOMAD.md")},
			{Path: filepath.Join(dir, "CLAUDE.md")},
			{Path: filepath.Join(dir, "AGENTS.md")},
			{Path: filepath.Join(dir, ".nomad", "NOMAD.md")},
			{Path: filepath.Join(dir, ".claude", "CLAUDE.md")},
		}))
	}
	picked = append(picked, readExisting([]InstructionFile{
		{Path: filepath.Join(workspace, "NOMAD.local.md"), Private: true},
		{Path: filepath.Join(workspace, "CLAUDE.local.md"), Private: true},
	})...)
	return dedupInstructionFiles(picked)
}

func firstExisting(candidates []InstructionFile) InstructionFile {
	files := readExisting(candidates)
	if len(files) == 0 {
		return InstructionFile{}
	}
	return files[0]
}

func readExisting(candidates []InstructionFile) []InstructionFile {
	var out []InstructionFile
	for _, c := range candidates {
		if data, ok := readTextFile(c.Path); ok {
			c.Content = data
			out = append(out, c)
		}
	}
	return out
}

func dedupInstructionFiles(files []InstructionFile) []InstructionFile {
	seen := map[string]bool{}
	var out []InstructionFile
	for _, c := range files {
		if c.Path == "" {
			continue
		}
		real, err := filepath.EvalSymlinks(c.Path)
		if err != nil {
			real = c.Path
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		out = append(out, c)
	}
	return out
}

func readTextFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// SkillDirs returns the directories searched for skills, in priority
// order: project-local first, then user-global, across the .claude/.agents
// conventions and nomad's own dir.
func SkillDirs(home, workspace, nomadSkillsDir string) []string {
	return []string{
		filepath.Join(workspace, ".claude", "skills"),
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".agents", "skills"),
		nomadSkillsDir,
	}
}

// DiscoveredSkill pairs a parsed skill with its on-disk location.
type DiscoveredSkill struct {
	Skill
	Dir string
}

// DiscoverSkills scans all standard skill directories and returns de-duplicated
// skills (the first directory in priority order wins per name).
func DiscoverSkills(home, workspace, nomadSkillsDir string) []DiscoveredSkill {
	byName := map[string]DiscoveredSkill{}
	var order []string
	for _, dir := range SkillDirs(home, workspace, nomadSkillsDir) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			bundle := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(bundle, "SKILL.md")); err != nil {
				continue
			}
			data, err := os.ReadFile(filepath.Join(bundle, "SKILL.md"))
			if err != nil {
				continue
			}
			s := parseSkill(e.Name(), string(data))
			if _, exists := byName[s.Name]; exists {
				continue
			}
			byName[s.Name] = DiscoveredSkill{Skill: s, Dir: bundle}
			order = append(order, s.Name)
		}
	}
	sort.Strings(order)
	out := make([]DiscoveredSkill, 0, len(order))
	for _, n := range order {
		out = append(out, byName[n])
	}
	return out
}

// LoadSkills scans a single directory (kept for the nomad skills dir).
func LoadSkills(skillsDir string) []Skill {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(skillsDir, e.Name(), "SKILL.md"))
		if err != nil {
			continue
		}
		out = append(out, parseSkill(e.Name(), string(data)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LoadSkill loads one skill by name.
func LoadSkill(skillsDir, name string) (Skill, bool) {
	data, err := os.ReadFile(filepath.Join(skillsDir, name, "SKILL.md"))
	if err != nil {
		return Skill{}, false
	}
	return parseSkill(name, string(data)), true
}

func parseSkill(dirName, content string) Skill {
	s := Skill{Name: dirName, Body: content}
	desc, body, ok := splitFrontMatter(content)
	if ok {
		s.Body = strings.TrimSpace(body)
		if v := frontMatterValue(desc, "name"); v != "" {
			s.Name = v
		}
		s.Description = strings.TrimSpace(frontMatterValue(desc, "description"))
	}
	return s
}

func splitFrontMatter(content string) (front, body string, ok bool) {
	content = strings.TrimPrefix(content, "\ufeff")
	if !strings.HasPrefix(content, "---") {
		return "", content, false
	}
	rest := strings.TrimPrefix(content, "---")
	rest = strings.TrimPrefix(rest, "\r\n")
	rest = strings.TrimPrefix(rest, "\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content, false
	}
	front = rest[:idx]
	body = rest[idx+4:]
	return front, body, true
}

func frontMatterValue(front, key string) string {
	prefix := key + ":"
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), `"'`)
		}
	}
	return ""
}

// SystemAddendum renders global and project instructions plus a skill
// catalogue as extra system text.
func (b Bundle) SystemAddendum() string {
	var sb strings.Builder
	if len(b.GlobalFiles) > 0 {
		sb.WriteString("# User memory (global)\n")
		for _, f := range b.GlobalFiles {
			writeInstructionFile(&sb, f)
		}
		sb.WriteString("\n")
	}
	if len(b.ProjectFiles) > 0 {
		sb.WriteString("# Project instructions\n")
		for _, f := range b.ProjectFiles {
			writeInstructionFile(&sb, f)
		}
		sb.WriteString("\n")
	}
	if len(b.Skills) > 0 {
		sb.WriteString("# Available skills\nUse the relevant skill when its description matches the task. ")
		sb.WriteString("Ask the user to invoke it or follow it directly.\n")
		for _, s := range b.Skills {
			sb.WriteString("- ")
			sb.WriteString(s.Name)
			if s.Description != "" {
				sb.WriteString(": " + s.Description)
			}
			sb.WriteString("\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

func writeInstructionFile(sb *strings.Builder, f InstructionFile) {
	fmt.Fprintf(sb, "## %s\n", f.Path)
	if f.Private {
		sb.WriteString("_private, not checked in_\n")
	}
	sb.WriteString(f.Content)
	sb.WriteString("\n\n")
}
