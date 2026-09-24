package cli

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const gitDiffTimeout = 30 * time.Second

func (a *App) cmdDiff(ctx context.Context) error {
	diff, err := gitDiff(ctx, a.paths.Workspace)
	if err != nil {
		if strings.Contains(err.Error(), "not a git repository") {
			a.printf("%snot a git repository%s\n", cDim, cReset)
			return nil
		}
		a.printf("%s%s%s\n", cRed, err.Error(), cReset)
		return nil
	}
	if strings.TrimSpace(diff) == "" {
		a.printf("%sworking tree clean%s\n", cDim, cReset)
		return nil
	}
	a.renderFoldable("git diff", strings.TrimRight(diff, "\n"), cDim)
	return nil
}

// gitDiff returns the working-tree unified diff, distinguishing
// non-repository errors with a sentinel message.
func gitDiff(ctx context.Context, dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", &gitDiffError{"git not installed"}
	}
	if out, err := runGit(ctx, dir, "rev-parse", "--is-inside-work-tree"); err != nil || out != "true" {
		return "", &gitDiffError{"not a git repository"}
	}
	diff, err := runGitCombined(ctx, dir, "diff")
	if err != nil {
		return "", &gitDiffError{diff}
	}
	return diff, nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func runGitCombined(ctx context.Context, dir string, args ...string) (string, error) {
	runCtx, cancel := context.WithTimeout(ctx, gitDiffTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type gitDiffError struct{ msg string }

func (e *gitDiffError) Error() string { return e.msg }
