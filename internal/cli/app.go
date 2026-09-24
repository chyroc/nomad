// Package cli implements the nomad terminal UI and headless runner.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/contextinfo"
	"github.com/chyroc/nomad/internal/control"
	"github.com/chyroc/nomad/internal/loop"
)

const (
	cReset  = "\x1b[0m"
	cDim    = "\x1b[2m"
	cCyan   = "\x1b[36m"
	cPurple = "\x1b[35m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cRed    = "\x1b[31m"
	cBold   = "\x1b[1m"
)

// App is the CLI application.
type App struct {
	paths config.Paths
	opts  Options
	in    io.Reader
	out   io.Writer
	color bool

	ctrl *control.App

	editor *lineEditor
	act    *activityLine

	sessionID string
	runner    loop.Runner
	model     string

	turnMu        sync.Mutex
	turnCancel    context.CancelFunc
	interruptMu   sync.Mutex
	lastInterrupt time.Time
	quit          bool

	lastAnswer string
	skills     []contextinfo.DiscoveredSkill

	toolName string
	toolArgs string

	thinkingBuf   strings.Builder
	thinkingStart time.Time

	resetTerminalModes func()
	restoreRawTerm     func()

	modalMu      sync.Mutex
	modalActive  bool
	modalPending []func()

	foldMu     sync.Mutex
	folds      map[int]*foldBlock
	foldOrder  []int
	foldSeen   map[int]bool
	nextFoldID int
	mouseOn    bool
}

// withModal buffers event rendering while an inline modal (permission
// picker) is on screen, then replays it in order after the modal closes.
func (a *App) withModal(run func() string) string {
	a.modalMu.Lock()
	a.modalActive = true
	a.modalMu.Unlock()
	answer := run()
	a.modalMu.Lock()
	a.modalActive = false
	pending := a.modalPending
	a.modalPending = nil
	a.modalMu.Unlock()
	for _, fn := range pending {
		fn()
	}
	return answer
}

// New constructs the app (no network yet).
func New(opts Options, in io.Reader, out io.Writer) *App {
	paths := config.DefaultPaths()
	for _, d := range opts.AddDirs {
		_ = d
	}
	interactive := isTerminal(out) && opts.OutputFormat == FormatText && !opts.Print
	wrapped := newProfileWriter(out)
	if fd := fdOf(wrapped); fd >= 0 {
		startSizeWatcher(fd)
	}
	return &App{
		paths: paths,
		opts:  opts,
		in:    in,
		out:   wrapped,
		color: interactive,
	}
}

func (a *App) printf(format string, args ...interface{}) {
	fmt.Fprintf(a.out, format, args...)
}

func (a *App) style(c, s string) string {
	if !a.color {
		return s
	}
	return c + s + cReset
}

// Run dispatches to login/logout, headless or TUI.
func (a *App) Run(ctx context.Context) error {
	if err := a.paths.Ensure(); err != nil {
		return err
	}
	switch a.opts.Subcommand {
	case "login":
		return a.runLogin(ctx)
	case "logout":
		return a.runLogout()
	}

	app, err := control.Load(a.paths)
	if errors.Is(err, control.ErrNotLoggedIn) {
		a.printf("%sNot logged in yet — starting first-run login.%s\n", cYellow, cReset)
		app = &control.App{Paths: a.paths, BaseURL: ark.DefaultBaseURL}
		if err := a.interactiveLogin(ctx, app); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	a.ctrl = app
	a.model = a.opts.Model
	if a.model == "" {
		a.model = app.Profile.Model
	}
	if !a.opts.EffortSet && app.Profile.Effort != "" {
		a.opts.ReasoningEffort = app.Profile.Effort
	}

	if err := app.EnsureProvision(ctx); err != nil {
		return fmt.Errorf("provision managed-agents resources: %w\n(run `nomad login` to refresh credentials)", err)
	}
	if a.model == "" {
		a.model = app.Profile.Model
	}

	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = app.Client.ListModels(cctx)
	}()

	a.relinkBoundSkills()

	if len(a.opts.SyncSkills) > 0 {
		if err := a.syncSkills(ctx, a.opts.SyncSkills, true); err != nil {
			return err
		}
	}

	a.skills = a.discoverLocalSkills()

	if a.opts.Print {
		return a.runHeadless(ctx)
	}
	return a.runTUI(ctx)
}

// sessionSystem builds the system prompt addendum from memory/skills and
// CLI flags.
func (a *App) sessionSystem() string {
	b := contextinfo.Load(a.paths.MemoryFile(), a.paths.Workspace)
	for _, d := range a.skills {
		b.Skills = append(b.Skills, d.Skill)
	}
	parts := []string{}
	if s := strings.TrimSpace(b.SystemAddendum()); s != "" {
		parts = append(parts, s)
	}
	if s := a.boundSkillLocalHint(); s != "" {
		parts = append(parts, s)
	}
	if a.opts.SystemPrompt != "" {
		if data, err := os.ReadFile(strings.TrimPrefix(a.opts.SystemPrompt, "@")); err == nil &&
			strings.HasPrefix(a.opts.SystemPrompt, "@") {
			parts = append(parts, string(data))
		} else {
			parts = append(parts, a.opts.SystemPrompt)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (a *App) permMode() ark.PermissionMode {
	switch a.opts.PermissionMode {
	case "acceptEdits":
		return ark.PermEdit
	case "plan":
		return ark.PermPlan
	case "bypassPermissions":
		return ark.PermBypass
	default:
		return ark.PermDefault
	}
}

func (a *App) toolSets() (allowed, disallowed map[string]bool) {
	toSet := func(list []string) map[string]bool {
		if len(list) == 0 {
			return nil
		}
		m := make(map[string]bool, len(list))
		for _, t := range list {
			m[t] = true
		}
		return m
	}
	return toSet(a.opts.AllowedTools), toSet(a.opts.DisallowedTools)
}

func (a *App) cancelTurn() {
	a.turnMu.Lock()
	r := a.runner
	c := a.turnCancel
	a.turnMu.Unlock()
	if in, ok := r.(loop.Interruptor); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = in.Interrupt(ctx)
		cancel()
	}
	if c != nil {
		c()
	}
}

func (a *App) closeRunner() {
	a.turnMu.Lock()
	r := a.runner
	a.runner = nil
	a.turnMu.Unlock()
	if r != nil {
		_ = r.Close()
	}
}

var _ = syscall.SIGINT
