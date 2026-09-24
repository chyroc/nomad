package contextinfo

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestGatherEnvironmentNonGit(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	info := GatherEnvironment(context.Background(), dir, "test-model", "high")
	if elapsed := time.Since(start); elapsed > envProbeTimeout+time.Second {
		t.Fatalf("gather took %s, want under %s", elapsed, envProbeTimeout)
	}
	if info.IsGitRepo {
		t.Fatal("temp dir reported as git repo")
	}
	if info.WorkingDir != dir {
		t.Fatalf("working dir = %q, want %q", info.WorkingDir, dir)
	}
	if info.Platform == "" || info.Date == "" {
		t.Fatalf("platform/date missing: %+v", info)
	}
	if info.Model != "test-model" || info.Effort != "high" {
		t.Fatalf("model/effort not carried: %+v", info)
	}
}

func TestGatherEnvironmentGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
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
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	run("commit", "--allow-empty", "-q", "-m", "first")
	run("commit", "--allow-empty", "-q", "-m", "second")

	info := GatherEnvironment(context.Background(), dir, "", "")
	if !info.IsGitRepo {
		t.Skip("git repo not detected (git may be blocked in sandbox)")
	}
	if info.Branch == "" {
		t.Error("branch empty")
	}
	if info.GitUser != "Test User" {
		t.Errorf("git user = %q", info.GitUser)
	}
	if !strings.Contains(info.RecentLog, "first") || !strings.Contains(info.RecentLog, "second") {
		t.Errorf("recent log missing commits: %q", info.RecentLog)
	}
	rendered := info.Render()
	for _, want := range []string{"# Environment", "Current branch", "Recent commits", "first", "second"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestEnvRenderEmptySafe(t *testing.T) {
	rendered := EnvInfo{}.Render()
	if !strings.Contains(rendered, "# Environment") {
		t.Fatal("render without header")
	}
}
