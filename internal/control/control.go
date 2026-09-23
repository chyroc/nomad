// Package control wires credentials, provisioned profile, the MA client
// and a per-session runner together.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/config"
)

// App holds the authenticated client and cached provisioning profile.
type App struct {
	Paths   config.Paths
	Creds   *ark.Credentials
	Profile ark.Profile
	Client  *ark.Client
	BaseURL string
}

// Load reads credentials and profile without network access.
func Load(paths config.Paths) (*App, error) {
	creds, err := ark.Load(paths.AuthFile())
	if err != nil {
		return nil, ErrNotLoggedIn
	}
	if creds.APIKey == "" {
		return nil, ErrNotLoggedIn
	}
	app := &App{
		Paths:   paths,
		Creds:   creds,
		BaseURL: ark.DefaultBaseURL,
		Client:  newArkClient(ark.DefaultBaseURL, creds, paths),
	}
	if data, err := os.ReadFile(paths.ProfileFile()); err == nil {
		_ = json.Unmarshal(data, &app.Profile)
	}
	return app, nil
}

// ErrNotLoggedIn means no cached API key exists; the caller runs login.
var ErrNotLoggedIn = errors.New("not logged in")

// Login runs the interactive cross-device OAuth flow and mints an Ark
// API key. onURL is called with the authorization link; promptCode reads
// the pasted callback code from the user.
func (a *App) Login(ctx context.Context, onURL func(string), promptCode func() (string, error)) error {
	begin, err := ark.BeginLogin()
	if err != nil {
		return err
	}
	onURL(begin.AuthorizeURL)
	pasted, err := promptCode()
	if err != nil {
		return err
	}
	httpClient := httpClientForLogin()
	creds, err := begin.Complete(ctx, httpClient, pasted)
	if err != nil {
		return err
	}
	ak, sk, token, err := creds.STS(ctx)
	if err != nil {
		return err
	}
	info, err := ark.CreateArkAPIKey(ctx, ak, sk, token, "nomad-cli-"+timestampName())
	if err != nil {
		return err
	}
	creds.APIKey = info.APIKey
	creds.ProjectName = info.ProjectName
	if err := ark.Save(a.Paths.AuthFile(), creds); err != nil {
		return err
	}
	a.Creds = creds
	a.Client = newArkClient(a.BaseURL, creds, a.Paths)
	return nil
}

// EnsureProvision creates the self-hosted environment/coding agent on
// first use and caches their ids locally.
func (a *App) EnsureProvision(ctx context.Context) error {
	prof, err := a.Client.Provision(ctx, a.Profile)
	if err != nil {
		return err
	}
	a.Profile = prof
	return saveJSON(a.Paths.ProfileFile(), prof)
}

// SetModel persists the selected model for future sessions.
func (a *App) SetModel(model string) error {
	a.Profile.Model = model
	return saveJSON(a.Paths.ProfileFile(), a.Profile)
}

// SaveProfile persists the current provisioned profile.
func (a *App) SaveProfile() error {
	return saveJSON(a.Paths.ProfileFile(), a.Profile)
}

// NewRunner creates or attaches to a session.
func (a *App) NewRunner(ctx context.Context, o ark.RunnerOptions) (*ark.Runner, error) {
	o.Client = a.Client
	if o.Profile.AgentID == "" {
		o.Profile = a.Profile
	}
	return ark.NewRunner(ctx, o)
}

func newArkClient(baseURL string, creds *ark.Credentials, paths config.Paths) *ark.Client {
	sts := func() (ak, sk, token string, ok bool) {
		if creds.HasSTS() {
			return creds.AK, creds.SK, creds.SessionToken, true
		}
		return ark.DiscoverArkCLISTS()
	}
	return ark.NewClient(baseURL, creds.APIKey, creds.ProjectName,
		ark.WithSTSProvider(sts),
		ark.WithModelCache(paths.ModelsCache()),
	)
}

func saveJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
