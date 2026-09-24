package contextinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSkillsFollowsSymlink(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "skill-source", "ego-browser")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(target, "SKILL.md"),
		"---\nname: ego-browser\ndescription: drive a browser\n---\n# body")

	skillsDir := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(skillsDir, "ego-browser")); err != nil {
		t.Fatal(err)
	}

	got := DiscoverSkills(home, t.TempDir(), filepath.Join(home, ".nomad", "skills"))
	for _, s := range got {
		if s.Name == "ego-browser" {
			return
		}
	}
	t.Fatal("symlinked ego-browser skill was not discovered")
}
