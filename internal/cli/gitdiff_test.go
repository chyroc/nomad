package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitDiffNotARepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, err := gitDiff(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("want not-a-git-repository error, got %v", err)
	}
}

func TestGitDiffCleanAndDirty(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %v %s", err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.com")
	run("config", "user.name", "T")
	run("commit", "--allow-empty", "-q", "-m", "init")

	if diff, err := gitDiff(context.Background(), dir); err != nil || diff != "" {
		t.Fatalf("clean tree: diff=%q err=%v", diff, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("orig\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "add file")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff, err := gitDiff(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "f.txt") || !strings.Contains(diff, "+changed") {
		t.Fatalf("dirty diff missing change:\n%s", diff)
	}
}
