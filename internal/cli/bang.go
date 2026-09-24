package cli

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// runBangCommand executes a shell command directly (cc-style "!cmd"
// prefix), streaming combined output into the scrollback without
// involving the model.
func (a *App) runBangCommand(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		a.printf("%susage: !<shell command>%s\n", cDim, cReset)
		return
	}
	a.printf("%s⏺ %s%s\n", cDim, command, cReset)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = a.paths.Workspace
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimRight(buf.String(), "\n")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			a.printf("%s  %s%s\n", cDim, line, cReset)
		}
	}
	if err != nil {
		a.printf("%s  ✗ %v%s\n", cRed, err, cReset)
		return
	}
	a.printf("%s  ✓ exit 0%s\n", cDim, cReset)
}
