package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chyroc/nomad/internal/control"
)

// chooseProject shows an up/down selector for the account's IAM
// projects. Esc or any read failure falls back to the first project.
func chooseProject(in io.Reader, out io.Writer, projects []string) string {
	if len(projects) == 0 {
		return ""
	}
	items := make([]pickItem, 0, len(projects))
	for _, p := range projects {
		items = append(items, pickItem{id: p, label: p})
	}
	fmt.Fprintln(out)
	pk := newPickerFull(in, out, items, 0,
		"Select project",
		"The new API key will be scoped to this project.",
		nil, 0)
	id, ok := pk.Run()
	if !ok || id == "" {
		return projects[0]
	}
	return id
}

// readLine reads one line without buffering ahead, so a later raw
// consumer of the same reader still sees every following byte.
func readLine(r io.Reader) (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				return strings.TrimRight(string(buf), "\r"), nil
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			return strings.TrimRight(string(buf), "\r"), err
		}
	}
}

func (a *App) interactiveLogin(ctx context.Context, app *control.App) error {
	return a.loginFlow(ctx, app, a.in, a.out)
}

func (a *App) runLogin(ctx context.Context) error {
	app, err := control.Load(a.paths)
	if err != nil {
		app = &control.App{Paths: a.paths}
	}
	return a.loginFlow(ctx, app, a.in, a.out)
}

func (a *App) loginFlow(ctx context.Context, app *control.App, in io.Reader, out io.Writer) error {
	fmt.Fprintln(out, "Log in to Volcengine Ark (cross-device OAuth).")
	fmt.Fprintln(out, "Open this URL in a browser, approve, then paste the code shown on the page:")

	err := app.Login(ctx,
		func(url string) { fmt.Fprintf(out, "\n%s%s%s\n\n", a.style(cCyan, ""), url, cReset) },
		func() (string, error) {
			fmt.Fprint(out, "Paste authorization code: ")
			line, err := readLine(in)
			return strings.TrimSpace(line), err
		},
		func(projects []string) string {
			return chooseProject(in, out, projects)
		},
	)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	a.ctrl = app
	fmt.Fprintln(out, a.style(cGreen, "✓ Logged in."))
	return nil
}

func (a *App) runLogout() error {
	if err := os.Remove(a.paths.AuthFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Fprintln(a.out, "Logged out (removed local credentials).")
	return nil
}
