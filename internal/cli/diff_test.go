package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffIdentical(t *testing.T) {
	if lines := unifiedDiff("a\nb\nc", "a\nb\nc"); len(lines) != 0 {
		t.Fatalf("identical input produced diff: %+v", lines)
	}
}

func TestUnifiedDiffInsert(t *testing.T) {
	lines := unifiedDiff("a\nc", "a\nb\nc")
	var plus, minus, at int
	for _, l := range lines {
		switch l.Kind {
		case '+':
			plus++
			if l.Text != "b" {
				t.Errorf("added line = %q", l.Text)
			}
		case '-':
			minus++
		case '@':
			at++
			if !strings.HasPrefix(l.Text, "@@ ") {
				t.Errorf("hunk header malformed: %q", l.Text)
			}
		}
	}
	if plus != 1 || minus != 0 || at != 1 {
		t.Fatalf("insert diff wrong: plus=%d minus=%d at=%d lines=%+v", plus, minus, at, lines)
	}
}

func TestUnifiedDiffReplace(t *testing.T) {
	lines := unifiedDiff("one\ntwo\nthree", "one\nTWO\nthree")
	var plus, minus int
	for _, l := range lines {
		switch l.Kind {
		case '+':
			plus++
		case '-':
			minus++
		}
	}
	if plus != 1 || minus != 1 {
		t.Fatalf("replace diff wrong: plus=%d minus=%d", plus, minus)
	}
}

func TestUnifiedDiffDeleteAll(t *testing.T) {
	lines := unifiedDiff("a\nb", "")
	minus := 0
	for _, l := range lines {
		if l.Kind == '-' {
			minus++
		}
	}
	if minus != 2 {
		t.Fatalf("delete-all diff wrong: %+v", lines)
	}
}

func TestToolCallDiffEdit(t *testing.T) {
	args := `{"file_path":"x.go","old_string":"a\nc","new_string":"a\nb\nc"}`
	_, ok := toolCallDiff("edit", args, t.TempDir())
	if !ok {
		t.Fatal("edit diff not produced")
	}
}

func TestToolCallDiffWriteExisting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := `{"file_path":"f.txt","content":"new\n"}`
	_, ok := toolCallDiff("write", args, dir)
	if !ok {
		t.Fatal("write diff not produced")
	}
}

func TestToolCallDiffUnknownTool(t *testing.T) {
	if _, ok := toolCallDiff("bash", `{"command":"ls"}`, t.TempDir()); ok {
		t.Fatal("bash should not produce a diff")
	}
}

func TestToolCallDiffMalformedArgs(t *testing.T) {
	if _, ok := toolCallDiff("edit", `{not json`, t.TempDir()); ok {
		t.Fatal("malformed args should return ok=false")
	}
	if _, ok := toolCallDiff("edit", `{"file_path":""}`, t.TempDir()); ok {
		t.Fatal("missing file_path should return ok=false")
	}
}
