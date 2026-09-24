package contextinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectHierarchyOutermostFirst(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	inner := filepath.Join(outer, "inner")
	ws := filepath.Join(inner, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(outer, "NOMAD.md"), "outer note")
	writeFile(t, filepath.Join(inner, "CLAUDE.md"), "inner note")
	writeFile(t, filepath.Join(ws, "NOMAD.md"), "ws note")
	writeFile(t, filepath.Join(ws, "NOMAD.local.md"), "private note")

	b := Load(t.TempDir(), t.TempDir(), ws)
	if len(b.ProjectFiles) != 4 {
		t.Fatalf("want 4 project files, got %d: %+v", len(b.ProjectFiles), b.ProjectFiles)
	}
	want := []string{"outer note", "inner note", "ws note", "private note"}
	for i, w := range want {
		if b.ProjectFiles[i].Content != w {
			t.Errorf("file %d = %q, want %q", i, b.ProjectFiles[i].Content, w)
		}
	}
	if !b.ProjectFiles[3].Private {
		t.Error("NOMAD.local.md should be marked private")
	}
}

func TestLoadProjectFirstCandidateWinsPerDir(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "NOMAD.md"), "nomad")
	writeFile(t, filepath.Join(ws, "CLAUDE.md"), "claude")
	writeFile(t, filepath.Join(ws, "AGENTS.md"), "agents")

	b := Load(t.TempDir(), t.TempDir(), ws)
	var projectNotes []string
	for _, f := range b.ProjectFiles {
		projectNotes = append(projectNotes, f.Content)
	}
	if len(projectNotes) != 1 || projectNotes[0] != "nomad" {
		t.Fatalf("per-dir priority wrong: %v", projectNotes)
	}
}

func TestLoadGlobalLegacyMemory(t *testing.T) {
	global := t.TempDir()
	writeFile(t, filepath.Join(global, "MEMORY.md"), "legacy memory")
	b := Load(global, t.TempDir(), t.TempDir())
	if len(b.GlobalFiles) != 1 || b.GlobalFiles[0].Content != "legacy memory" {
		t.Fatalf("legacy MEMORY.md not loaded: %+v", b.GlobalFiles)
	}
}

func TestLoadProjectRealpathDedup(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "NOMAD.md"), "real note")
	if err := os.Symlink(filepath.Join(ws, "NOMAD.md"), filepath.Join(ws, "CLAUDE.md")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	b := Load(t.TempDir(), t.TempDir(), ws)
	if len(b.ProjectFiles) != 1 {
		t.Fatalf("symlinked duplicate not deduped: %+v", b.ProjectFiles)
	}
}

func TestLoadNestedNomadDir(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, ".nomad", "NOMAD.md"), "nested note")
	b := Load(t.TempDir(), t.TempDir(), ws)
	found := false
	for _, f := range b.ProjectFiles {
		if f.Content == "nested note" {
			found = true
		}
	}
	if !found {
		t.Fatalf(".nomad/NOMAD.md not loaded: %+v", b.ProjectFiles)
	}
}
