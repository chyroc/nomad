package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/chyroc/nomad/internal/appserver"
	"github.com/chyroc/nomad/internal/store"
)

func (a *App) runAppServer(ctx context.Context) error {
	transcript, err := store.NewSessionStore(a.paths.SessionsDir())
	if err != nil {
		return err
	}
	srv := appserver.New(appserver.NewArkBackend(a.paths), transcript, Version, os.Stdout)
	srv.ChatDir = filepath.Join(a.paths.DataDir, "chats")
	return srv.Serve(ctx, a.in)
}
