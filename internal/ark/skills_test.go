package ark

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStubSkillZip_OnlyFrontmatter(t *testing.T) {
	data, fname, err := stubSkillZip("my skill", "does a thing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(fname, ".zip") {
		t.Fatalf("filename = %s", fname)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("want exactly 1 file, got %d", len(zr.File))
	}
	f := zr.File[0]
	if !strings.HasSuffix(f.Name, "my-skill/SKILL.md") {
		t.Fatalf("unexpected path %s", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(rc)
	rc.Close()
	s := string(body)
	if !strings.Contains(s, "name: my-skill") || !strings.Contains(s, "description: does a thing") {
		t.Fatalf("missing frontmatter:\n%s", s)
	}
	if strings.Contains(s, "SECRET-BODY") {
		t.Fatal("instruction body must not be uploaded")
	}
}

func TestSanitizeSkillName(t *testing.T) {
	cases := map[string]string{
		"deploy app":  "deploy-app",
		"ok_name-x.1": "ok_name-x-1",
		"!!!":         "skill",
	}
	for in, want := range cases {
		if got := sanitizeSkillName(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestLinkLocalSkill(t *testing.T) {
	ws := t.TempDir()
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "SKILL.md"), []byte("# real body"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LinkLocalSkill(ws, "demo", local); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(ws, ".nomad", "skills", "demo", "SKILL.md")
	got, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("symlink target unreadable: %v", err)
	}
	if string(got) != "# real body" {
		t.Fatalf("linked content = %q", got)
	}
}
