package contextinfo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	envProbeTimeout = 2 * time.Second
	envProbeSingle  = 1200 * time.Millisecond
	envStatusLimit  = 2000
)

// EnvInfo is the per-session snapshot of the local working environment
// rendered into the system prompt.
type EnvInfo struct {
	WorkingDir string
	IsGitRepo  bool
	Branch     string
	GitStatus  string
	RecentLog  string
	GitUser    string
	Platform   string
	OSVersion  string
	Shell      string
	Date       string
	Model      string
	Effort     string
}

// GatherEnvironment collects a bounded snapshot of cwd, git state,
// platform and model metadata. Every probe degrades silently and the
// whole call returns within envProbeTimeout.
func GatherEnvironment(ctx context.Context, workspace, model, effort string) EnvInfo {
	info := EnvInfo{
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Shell:    os.Getenv("SHELL"),
		Date:     time.Now().Format("2006-01-02 Monday"),
		Model:    model,
		Effort:   effort,
	}
	if wd, err := filepath.Abs(workspace); err == nil {
		info.WorkingDir = wd
	} else {
		info.WorkingDir = workspace
	}
	if v := probeOSRelease(); v != "" {
		info.OSVersion = v
	}

	ctx, cancel := context.WithTimeout(ctx, envProbeTimeout)
	defer cancel()

	var wg sync.WaitGroup
	probe := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	probe(func() {
		if gitOutput(ctx, info.WorkingDir, "rev-parse", "--is-inside-work-tree") != "true" {
			return
		}
		info.IsGitRepo = true
		if v := gitOutput(ctx, info.WorkingDir, "rev-parse", "--abbrev-ref", "HEAD"); v != "" {
			info.Branch = v
		}
		if v := gitOutput(ctx, info.WorkingDir, "status", "--short", "--branch"); v != "" {
			info.GitStatus = truncateRunes(strings.TrimSpace(v), envStatusLimit)
		}
		if v := gitOutput(ctx, info.WorkingDir, "log", "-5", "--oneline"); v != "" {
			info.RecentLog = strings.TrimSpace(v)
		}
		if v := gitOutput(ctx, info.WorkingDir, "config", "user.name"); v != "" {
			info.GitUser = v
		}
	})
	probe(func() {
		if info.OSVersion != "" {
			return
		}
		if v := runCmd(ctx, "", "uname", "-sr"); v != "" {
			info.OSVersion = v
		}
	})
	wg.Wait()
	return info
}

// Render formats the snapshot as a system-prompt section.
func (e EnvInfo) Render() string {
	var sb strings.Builder
	sb.WriteString("# Environment\n")
	kv := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", key, value)
		}
	}
	kv("Working directory", e.WorkingDir)
	kv("Platform", e.Platform)
	kv("OS version", e.OSVersion)
	kv("Shell", e.Shell)
	if e.IsGitRepo {
		sb.WriteString("- Git repository: yes\n")
		kv("Current branch", e.Branch)
		kv("Git user", e.GitUser)
	} else {
		sb.WriteString("- Git repository: no\n")
	}
	kv("Model", e.Model)
	kv("Reasoning effort", e.Effort)
	kv("Today's date", e.Date)
	if e.GitStatus != "" {
		sb.WriteString("\nWorking tree status at session start:\n```\n")
		sb.WriteString(e.GitStatus)
		sb.WriteString("\n```\n")
	}
	if e.RecentLog != "" {
		sb.WriteString("\nRecent commits:\n```\n")
		sb.WriteString(e.RecentLog)
		sb.WriteString("\n```\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func gitOutput(ctx context.Context, dir string, args ...string) string {
	return runCmd(ctx, dir, "git", args...)
}

func runCmd(parent context.Context, dir, name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(parent, envProbeSingle)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func probeOSRelease() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	var name, version string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "PRETTY_NAME="):
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"'`)
		case strings.HasPrefix(line, "NAME=") && name == "":
			name = strings.Trim(strings.TrimPrefix(line, "NAME="), `"'`)
		case strings.HasPrefix(line, "VERSION=") && version == "":
			version = strings.Trim(strings.TrimPrefix(line, "VERSION="), `"'`)
		}
	}
	return strings.TrimSpace(name + " " + version)
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + " …"
}
