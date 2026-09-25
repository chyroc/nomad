package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/goal"
)

func newStatusBarApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	return &App{
		out:   ioDiscard{},
		color: true,
		paths: config.Paths{Home: dir, DataDir: dir, Workspace: dir},
		model: "doubao-seed-evolving",
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func goalActive(iterations int) *goal.State {
	return &goal.State{Status: goal.StatusActive, Iterations: iterations}
}

func TestStatusBarInfoLineShape(t *testing.T) {
	a := newStatusBarApp(t)
	a.opts.ReasoningEffort = "max"
	a.opts.PermissionMode = "bypassPermissions"
	bar := newStatusBar()
	bar.clock = func() time.Time { return time.Date(2026, 9, 24, 23, 38, 0, 0, time.Local) }

	line1, line2 := bar.render(a, 120)
	plain := ansi.Strip(line1)
	for _, want := range []string{"[23:38] ", "seed-evolving[max]", " | ark"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("info line missing %q: %q", want, plain)
		}
	}
	if strings.HasPrefix(plain, "[23:38] |") {
		t.Fatalf("clock must be followed by the repo, not a pipe: %q", plain)
	}
	if !strings.HasPrefix(plain, "[23:38] ") || !strings.Contains(plain, "/") {
		t.Fatalf("info line should lead with clock + repo segment: %q", plain)
	}
	hint := ansi.Strip(line2)
	for _, want := range []string{"⏵⏵ bypass permissions on (shift+tab to cycle)", "← for agents"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint line missing %q: %q", want, hint)
		}
	}
}

func TestStatusBarInfoLineGitBranch(t *testing.T) {
	a := newStatusBarApp(t)
	if got := gitSegment(a.paths.Workspace); got != "" {
		t.Fatalf("non-git dir should yield empty git segment, got %q", got)
	}
	if got := repoSegment("/home/alice/proj/nomad"); got != "proj/nomad" {
		t.Fatalf("repoSegment=%q want proj/nomad", got)
	}
}

func TestStatusBarInfoLineGoal(t *testing.T) {
	a := newStatusBarApp(t)
	a.goal = goalActive(3)
	bar := newStatusBar()
	line1, _ := bar.render(a, 200)
	plain1 := ansi.Strip(line1)
	if !strings.Contains(plain1, "◉ goal 3") {
		t.Fatalf("info line missing goal: %q", plain1)
	}
}

func TestStatusBarHintLineShape(t *testing.T) {
	a := newStatusBarApp(t)
	a.sessionID = "sess-123456789abc"
	bar := newStatusBar()
	_, line2 := bar.render(a, 200)
	plain2 := ansi.Strip(line2)
	if !strings.Contains(plain2, "← for agents") {
		t.Fatalf("hint line should end with agents hint: %q", plain2)
	}
	if strings.Contains(plain2, "session") {
		t.Fatalf("hint line should not carry the session id: %q", plain2)
	}
}

func TestStatusBarTruncatesToWidth(t *testing.T) {
	a := newStatusBarApp(t)
	bar := newStatusBar()
	line1, line2 := bar.render(a, 20)
	if w := ansi.StringWidth(ansi.Strip(line1)); w > 19 {
		t.Fatalf("line1 width %d exceeds width-1: %q", w, line1)
	}
	if w := ansi.StringWidth(ansi.Strip(line2)); w > 19 {
		t.Fatalf("line2 width %d exceeds width-1: %q", w, line2)
	}
}

func TestPermissionModeHintAllModes(t *testing.T) {
	a := newStatusBarApp(t)
	for _, tc := range []struct{ mode, want string }{
		{"default", "manual mode"},
		{"acceptEdits", "accept edits on"},
		{"plan", "plan mode on"},
		{"bypassPermissions", "bypass permissions on"},
	} {
		got := ansi.Strip(permissionModeHint(a, tc.mode))
		if !strings.Contains(got, "⏵⏵ "+tc.want+" (shift+tab to cycle)") {
			t.Fatalf("mode %s hint=%q", tc.mode, got)
		}
	}
}

func TestCyclePermissionMode(t *testing.T) {
	a := newStatusBarApp(t)
	a.opts.PermissionMode = "default"
	want := []string{"acceptEdits", "plan", "bypassPermissions", "default"}
	for i, w := range want {
		if got := a.cyclePermissionMode(); got != w {
			t.Fatalf("cycle %d = %q want %q", i, got, w)
		}
		if a.effectivePermissionMode() != w {
			t.Fatalf("effective mode not updated: %q", a.effectivePermissionMode())
		}
	}
}

func TestRenderInputFrameShape(t *testing.T) {
	a := newStatusBarApp(t)
	a.bar = newStatusBar()
	f := a.renderInputFrame(80)
	if w := displayWidth(f.top); w < 70 || w > 79 {
		t.Fatalf("top rule width=%d want at most 79 cells (one-cell wrap margin)", w)
	}
	if w := displayWidth(f.rows[0]); w < 70 || w > 79 {
		t.Fatalf("bottom rule width=%d want at most 79 cells (one-cell wrap margin)", w)
	}
	if len(f.rows) != 3 {
		t.Fatalf("frame rows=%d want 3 (rule + 2 status)", len(f.rows))
	}
	if f.rows[0] != f.top {
		t.Fatalf("first pinned row must be the bottom rule")
	}
	joined := strings.Join(append([]string{f.top}, f.rows...), "\n")
	for _, want := range []string{"[", "]", "ark", "shift+tab"} {
		if !strings.Contains(ansi.Strip(joined), want) {
			t.Fatalf("frame missing %q:\n%s", want, ansi.Strip(joined))
		}
	}
}

func TestRenderInputFrameNoColor(t *testing.T) {
	a := newStatusBarApp(t)
	a.color = false
	a.bar = newStatusBar()
	f := a.renderInputFrame(40)
	if !strings.Contains(f.top, strings.Repeat("-", 39)) {
		t.Fatalf("non-color rule should use dashes: %q", f.top)
	}
}

func TestDockChromeRowsShape(t *testing.T) {
	a := newStatusBarApp(t)
	a.bar = newStatusBar()
	rows := a.dockChromeRows(80)
	if len(rows) != 5 {
		t.Fatalf("dock rows=%d want 5 (top rule, input, bottom rule, 2 status)", len(rows))
	}
	if ansi.Strip(rows[0]) != ansi.Strip(rows[2]) {
		t.Fatalf("top and bottom rules should match: %q vs %q", rows[0], rows[2])
	}
	if !strings.Contains(ansi.Strip(rows[1]), "❯") {
		t.Fatalf("second dock row should be the static input row: %q", rows[1])
	}
	if ansi.StringWidth(ansi.Strip(rows[3])) == 0 || ansi.StringWidth(ansi.Strip(rows[4])) == 0 {
		t.Fatalf("status rows must not be empty: %q / %q", rows[3], rows[4])
	}
}
