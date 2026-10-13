package appserver

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/contextinfo"
	"github.com/chyroc/nomad/internal/control"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/settings"
)

// ErrNotLoggedIn is returned by backend calls that need credentials.
var ErrNotLoggedIn = errors.New("not logged in")

// Status describes the account and defaults a client renders.
type Status struct {
	LoggedIn bool   `json:"loggedIn"`
	Model    string `json:"model"`
	Effort   string `json:"effort"`
}

// Model is one selectable model.
type Model struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Efforts []string `json:"efforts,omitempty"`
}

// RunnerSpec describes the session a turn runs in.
type RunnerSpec struct {
	SessionID      string
	Workspace      string
	Model          string
	Effort         string
	PermissionMode string
	Ask            func(tool, arguments string) string
}

// Backend is the managed-agents surface the server drives.
type Backend interface {
	Status() Status
	Models(ctx context.Context) ([]Model, error)
	SetDefaults(model, effort string) error
	NewRunner(ctx context.Context, spec RunnerSpec) (loop.Runner, error)
	Login(ctx context.Context, onURL func(string), code func() (string, error)) error
	Logout() error
}

// ArkBackend implements Backend on top of the Ark managed-agents client.
type ArkBackend struct {
	paths config.Paths

	mu          sync.Mutex
	app         *control.App
	provisioned bool
}

// NewArkBackend loads cached credentials if present.
func NewArkBackend(paths config.Paths) *ArkBackend {
	b := &ArkBackend{paths: paths}
	if app, err := control.Load(paths); err == nil {
		b.app = app
	}
	return b
}

// Status implements Backend.
func (b *ArkBackend) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.app == nil {
		return Status{}
	}
	return Status{LoggedIn: true, Model: b.app.Profile.Model, Effort: b.app.Profile.Effort}
}

func (b *ArkBackend) ready(ctx context.Context) (*control.App, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.app == nil {
		return nil, ErrNotLoggedIn
	}
	if !b.provisioned {
		if err := b.app.EnsureProvision(ctx); err != nil {
			return nil, err
		}
		b.provisioned = true
	}
	return b.app, nil
}

// Models implements Backend.
func (b *ArkBackend) Models(ctx context.Context) ([]Model, error) {
	app, err := b.ready(ctx)
	if err != nil {
		return nil, err
	}
	list, err := app.Client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(list))
	for _, m := range list {
		name := m.Name
		if name == "" {
			name = m.SelectID()
		}
		out = append(out, Model{ID: m.SelectID(), Name: name, Efforts: m.Efforts})
	}
	return out, nil
}

// SetDefaults implements Backend.
func (b *ArkBackend) SetDefaults(model, effort string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.app == nil {
		return ErrNotLoggedIn
	}
	if model != "" {
		b.app.Profile.Model = model
	}
	if effort != "" {
		b.app.Profile.Effort = effort
	}
	return b.app.SaveProfile()
}

// NewRunner implements Backend.
func (b *ArkBackend) NewRunner(ctx context.Context, spec RunnerSpec) (loop.Runner, error) {
	app, err := b.ready(ctx)
	if err != nil {
		return nil, err
	}
	paths := b.paths
	paths.Workspace = spec.Workspace
	model := spec.Model
	if model == "" {
		model = app.Profile.Model
	}
	effort := spec.Effort
	if effort == "" {
		effort = app.Profile.Effort
	}
	opts := ark.RunnerOptions{
		Profile:         app.Profile,
		SessionID:       spec.SessionID,
		Workspace:       spec.Workspace,
		SystemPrompt:    systemAddendum(ctx, paths, model, effort),
		Model:           model,
		ReasoningEffort: effort,
		Permission:      permissionMode(spec.PermissionMode),
		Ask:             spec.Ask,
	}
	if st, err := settings.Load(paths.UserSettingsFile(), paths.ProjectSettingsFile(), paths.CompatSettingsFile()); err == nil {
		opts.AllowRules = st.AllowRules
		opts.DenyRules = st.DenyRules
	}
	if f, err := os.OpenFile(filepath.Join(paths.DataDir, "worker.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		opts.WorkerLogger = log.New(f, "", log.LstdFlags)
	}
	return app.NewRunner(ctx, opts)
}

// Login implements Backend.
func (b *ArkBackend) Login(ctx context.Context, onURL func(string), code func() (string, error)) error {
	app := &control.App{Paths: b.paths, BaseURL: ark.DefaultBaseURL}
	if err := app.Login(ctx, onURL, code, nil); err != nil {
		return err
	}
	loaded, err := control.Load(b.paths)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.app = loaded
	b.provisioned = false
	b.mu.Unlock()
	_, err = b.ready(ctx)
	return err
}

// Logout implements Backend.
func (b *ArkBackend) Logout() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.app = nil
	b.provisioned = false
	if err := os.Remove(b.paths.AuthFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func permissionMode(mode string) ark.PermissionMode {
	switch mode {
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

func systemAddendum(ctx context.Context, paths config.Paths, model, effort string) string {
	bundle := contextinfo.Load(paths.DataDir, paths.Home, paths.Workspace)
	for _, d := range contextinfo.DiscoverSkills(paths.Home, paths.Workspace, paths.SkillsDir()) {
		bundle.Skills = append(bundle.Skills, d.Skill)
	}
	parts := []string{}
	if s := strings.TrimSpace(bundle.SystemAddendum()); s != "" {
		parts = append(parts, s)
	}
	parts = append(parts, contextinfo.GatherEnvironment(ctx, paths.Workspace, model, effort).Render())
	return strings.Join(parts, "\n\n")
}
