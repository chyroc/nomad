package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chyroc/nomad/internal/control"
)

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
	br := bufio.NewReader(in)
	fmt.Fprintln(out, "Log in to Volcengine Ark (cross-device OAuth).")
	fmt.Fprintln(out, "Open this URL in a browser, approve, then paste the code shown on the page:")

	err := app.Login(ctx,
		func(url string) { fmt.Fprintf(out, "\n%s%s%s\n\n", a.style(cCyan, ""), url, cReset) },
		func() (string, error) {
			fmt.Fprint(out, "Paste authorization code: ")
			line, err := br.ReadString('\n')
			return strings.TrimSpace(line), err
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
