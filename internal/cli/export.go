package cli

import (
	"os"
	"strings"

	"github.com/chyroc/nomad/internal/store"
)

func (a *App) cmdExport(transcript *store.SessionStore, arg string) error {
	if strings.TrimSpace(a.sessionID) == "" {
		a.printf("%sno active session to export%s\n", cDim, cReset)
		return nil
	}
	target := strings.TrimSpace(arg)
	if target == "" {
		return transcript.ExportMarkdown(a.sessionID, a.out)
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if strings.HasSuffix(target, ".jsonl") {
		err = transcript.Export(a.sessionID, f)
	} else {
		err = transcript.ExportMarkdown(a.sessionID, f)
	}
	if err != nil {
		return err
	}
	a.printf("%sexported session to %s%s\n", cGreen, target, cReset)
	return nil
}
