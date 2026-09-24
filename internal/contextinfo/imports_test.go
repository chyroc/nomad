package contextinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func expandInDir(t *testing.T, dir, content string) string {
	t.Helper()
	exp := newImportExpander(t.TempDir())
	path := filepath.Join(dir, "NOMAD.md")
	return exp.expandFile(path, content)
}

func TestExpandImportsRelativeAndAbsolute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes", "a.md"), "A body")
	writeFile(t, filepath.Join(dir, "b.md"), "B body")

	out := expandInDir(t, dir, "before\n@notes/a.md\nafter\n@"+filepath.Join(dir, "b.md"))
	if !strings.Contains(out, "before") || !strings.Contains(out, "A body") ||
		!strings.Contains(out, "B body") || !strings.Contains(out, "after") {
		t.Fatalf("imports not inlined:\n%s", out)
	}
}

func TestExpandImportsTilde(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "shared.md"), "home body")
	dir := t.TempDir()
	exp := newImportExpander(home)
	out := exp.expandFile(filepath.Join(dir, "NOMAD.md"), "see @~/shared.md")
	if !strings.Contains(out, "home body") {
		t.Fatalf("tilde import failed:\n%s", out)
	}
}

func TestExpandImportsFragmentStripped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "fragment body")
	out := expandInDir(t, dir, "@./a.md#section")
	if !strings.Contains(out, "fragment body") {
		t.Fatalf("fragment import failed:\n%s", out)
	}
}

func TestExpandImportsSkipsCode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "SHOULD NOT APPEAR")
	content := "```\n@./a.md\n```\n`@./a.md` inline\n~~~\n@./a.md\n~~~\nnormal text"
	out := expandInDir(t, dir, content)
	if strings.Contains(out, "SHOULD NOT APPEAR") {
		t.Fatalf("import scanned inside code:\n%s", out)
	}
	if !strings.Contains(out, "@./a.md") {
		t.Fatalf("code text should be preserved verbatim:\n%s", out)
	}
}

func TestExpandImportsMissingSilent(t *testing.T) {
	dir := t.TempDir()
	out := expandInDir(t, dir, "see @./nope.md please")
	if !strings.Contains(out, "@./nope.md") {
		t.Fatalf("missing import token should be preserved:\n%s", out)
	}
}

func TestExpandImportsBinarySkipped(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bin.dat"), "head\x00binary")
	out := expandInDir(t, dir, "@./bin.dat")
	if strings.Contains(out, "binary") {
		t.Fatalf("binary file was imported:\n%s", out)
	}
}

func TestExpandImportsDepthLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= importMaxDepth+1; i++ {
		name := "f" + string(rune('a'+i)) + ".md"
		next := "f" + string(rune('a'+i+1)) + ".md"
		body := "level " + string(rune('a'+i)) + "\n"
		if i <= importMaxDepth {
			body += "@./" + next + "\n"
		}
		writeFile(t, filepath.Join(dir, name), body)
	}
	out := expandInDir(t, dir, "@./fa.md")
	if strings.Contains(out, "level "+string(rune('a'+importMaxDepth+1))) {
		t.Fatalf("import exceeded depth limit:\n%s", out)
	}
	if !strings.Contains(out, "level a") {
		t.Fatalf("shallow imports missing:\n%s", out)
	}
}

func TestExpandImportsSelfCycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loop.md"), "loop start\n@./loop.md\nloop end")
	out := expandInDir(t, dir, "@./loop.md")
	if strings.Count(out, "loop start") != 1 {
		t.Fatalf("self cycle not stopped:\n%s", out)
	}
}

func TestExpandImportsMutualCycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "A\n@./b.md")
	writeFile(t, filepath.Join(dir, "b.md"), "B\n@./a.md")
	out := expandInDir(t, dir, "@./a.md")
	if strings.Count(out, "\nA")+strings.Count(out, "A\n") > 2 || strings.Count(out, "B") > 1 {
		t.Fatalf("mutual cycle not stopped:\n%s", out)
	}
}

func TestExpandImportsSymlinkDedup(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.md"), "shared body")
	if err := os.Symlink(filepath.Join(dir, "real.md"), filepath.Join(dir, "link.md")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	out := expandInDir(t, dir, "@./real.md\n@./link.md")
	if strings.Count(out, "shared body") != 1 {
		t.Fatalf("symlinked import not deduped:\n%s", out)
	}
}

func TestExpandImportsListMarker(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "listed body")
	out := expandInDir(t, dir, "- @./a.md\n> @./a.md")
	if strings.Count(out, "listed body") != 2 {
		t.Fatalf("list/quote marker imports failed:\n%s", out)
	}
}

func TestExpandImportsEmailNotTouched(t *testing.T) {
	dir := t.TempDir()
	out := expandInDir(t, dir, "contact user@example.com")
	if !strings.Contains(out, "user@example.com") {
		t.Fatalf("email-like text altered:\n%s", out)
	}
}
