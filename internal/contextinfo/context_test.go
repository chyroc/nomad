package contextinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMemory(t *testing.T) {
	global := t.TempDir()
	ws := t.TempDir()
	mem := filepath.Join(global, "NOMAD.md")
	os.WriteFile(mem, []byte("global note"), 0o600)
	os.WriteFile(filepath.Join(ws, "NOMAD.md"), []byte("project note"), 0o600)

	b := Load(global, t.TempDir(), ws)
	if len(b.GlobalFiles) != 1 || b.GlobalFiles[0].Content != "global note" {
		t.Fatalf("global files wrong: %+v", b.GlobalFiles)
	}
	if len(b.ProjectFiles) != 1 || b.ProjectFiles[0].Content != "project note" {
		t.Fatalf("project files wrong: %+v", b.ProjectFiles)
	}
	add := b.SystemAddendum()
	if !strings.Contains(add, "global note") || !strings.Contains(add, "project note") {
		t.Fatalf("addendum missing notes:\n%s", add)
	}
}

func TestLoadSkills(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "deploy")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(
		"---\nname: deploy\ndescription: Deploy the service\n---\n\nRun make deploy.\n"), 0o600)

	skills := LoadSkills(root)
	if len(skills) != 1 || skills[0].Name != "deploy" || skills[0].Description != "Deploy the service" {
		t.Fatalf("skills = %+v", skills)
	}
	if s, ok := LoadSkill(root, "deploy"); !ok || !strings.Contains(s.Body, "make deploy") {
		t.Fatalf("load skill failed: %+v ok=%v", s, ok)
	}
	b := Bundle{Skills: skills}
	if !strings.Contains(b.SystemAddendum(), "- deploy: Deploy the service") {
		t.Fatalf("catalogue missing")
	}
}

func TestDiscoverSkills_MultiDirPriority(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	nomad := t.TempDir()
	write := func(dir, body string) {
		full := filepath.Join(dir, "s1", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(ws, ".claude", "skills"),
		"---\nname: s1\ndescription: project version\n---\nproject body\n")
	write(filepath.Join(home, ".agents", "skills"),
		"---\nname: s1\ndescription: global version\n---\nglobal body\n")
	write(filepath.Join(home, ".claude", "skills"),
		"---\nname: s2\ndescription: other\n---\n\nbody\n")

	got := DiscoverSkills(home, ws, nomad)
	if len(got) != 2 {
		t.Fatalf("want 2 deduped skills, got %d: %+v", len(got), got)
	}
	byName := map[string]DiscoveredSkill{}
	for _, d := range got {
		byName[d.Name] = d
	}
	if byName["s1"].Description != "project version" {
		t.Fatalf("priority wrong: %+v", byName["s1"])
	}
	if !strings.Contains(byName["s1"].Dir, filepath.Join(".claude", "skills")) {
		t.Fatalf("s1 should come from project dir: %s", byName["s1"].Dir)
	}
	if byName["s2"].Description != "other" {
		t.Fatalf("s2 missing: %+v", byName["s2"])
	}
}
