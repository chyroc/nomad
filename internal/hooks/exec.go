// Package hooks runs local lifecycle commands configured in settings.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// Event names.
const (
	EventPreToolUse       = "PreToolUse"
	EventPostToolUse      = "PostToolUse"
	EventUserPromptSubmit = "UserPromptSubmit"
	EventSessionStart     = "SessionStart"
	EventStop             = "Stop"
)

// Hook is one configured command for a lifecycle event.
type Hook struct {
	Matcher        string `json:"matcher,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// Envelope is the JSON document passed to a hook on stdin.
type Envelope struct {
	SessionID string          `json:"session_id,omitempty"`
	CWD       string          `json:"cwd"`
	Event     string          `json:"hook_event_name,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	Prompt    string          `json:"prompt,omitempty"`
}

// Outcome is the interpreted result of running matching hooks.
type Outcome struct {
	Blocked           bool
	Reason            string
	AdditionalContext string
}

// CommandRunner abstracts command execution for tests.
type CommandRunner interface {
	Run(ctx context.Context, cwd, command string, stdin []byte) (stdout, stderr []byte, exitCode int, err error)
}

// Executor runs hooks for one workspace.
type Executor struct {
	Runner       CommandRunner
	Timeout      time.Duration
	MinTimeout   time.Duration
	MaxTimeout   time.Duration
	DefaultBlock bool
}

// NewExecutor builds an executor using sh -c with the given timeout.
func NewExecutor(timeout time.Duration) *Executor {
	return &Executor{
		Runner:     shellRunner{},
		Timeout:    timeout,
		MinTimeout: time.Second,
		MaxTimeout: 10 * time.Minute,
	}
}

type shellRunner struct{}

func (shellRunner) Run(ctx context.Context, cwd, command string, stdin []byte) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return out.Bytes(), errBuf.Bytes(), exitErr.ExitCode(), nil
	}
	if err != nil {
		return out.Bytes(), errBuf.Bytes(), -1, err
	}
	return out.Bytes(), errBuf.Bytes(), 0, nil
}

// Matches reports whether the hook matcher applies to the tool/event.
func (h Hook) Matches(target string) bool {
	matcher := strings.TrimSpace(h.Matcher)
	if matcher == "" || matcher == "*" {
		return true
	}
	for _, part := range strings.Split(matcher, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == target {
			return true
		}
		if strings.ContainsAny(part, "*?[") && globMatch(part, target) {
			return true
		}
	}
	return false
}

func (e *Executor) timeoutFor(h Hook) time.Duration {
	if h.TimeoutSeconds > 0 {
		d := time.Duration(h.TimeoutSeconds) * time.Second
		if d < e.MinTimeout {
			return e.MinTimeout
		}
		if d > e.MaxTimeout {
			return e.MaxTimeout
		}
		return d
	}
	if e.Timeout > 0 {
		return e.Timeout
	}
	return 60 * time.Second
}

// Run executes all matching hooks in order and aggregates the outcome:
// exit code 2 or a deny decision blocks; stdout contributes context.
func (e *Executor) Run(ctx context.Context, hooks []Hook, env Envelope) Outcome {
	var out Outcome
	var contexts []string
	stdin, _ := json.Marshal(env)
	for _, h := range hooks {
		if env.ToolName != "" && !h.Matches(env.ToolName) {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, e.timeoutFor(h))
		stdout, stderr, code, err := e.runner().Run(runCtx, env.CWD, h.Command, stdin)
		cancel()
		if err != nil {
			out.Blocked = true
			out.Reason = firstNonEmpty(strings.TrimSpace(err.Error()), "hook failed to start")
			return out
		}
		decision := parseHookOutput(stdout)
		switch {
		case code == 2 || decision.Block:
			out.Blocked = true
			out.Reason = firstNonEmpty(strings.TrimSpace(decision.Reason), truncate(stderr), "blocked by hook")
			return out
		case code != 0:
			if e.DefaultBlock || env.Event == EventPreToolUse {
				out.Blocked = true
				out.Reason = firstNonEmpty(truncate(stderr), "hook exited with error")
				return out
			}
		}
		if ctx := firstNonEmpty(decision.Context, strings.TrimSpace(string(stdout))); ctx != "" {
			contexts = append(contexts, ctx)
		}
	}
	out.AdditionalContext = strings.Join(contexts, "\n")
	return out
}

func (e *Executor) runner() CommandRunner {
	if e.Runner != nil {
		return e.Runner
	}
	return shellRunner{}
}

type hookDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Context  string `json:"additionalContext"`
	Block    bool   `json:"-"`
}

func parseHookOutput(stdout []byte) hookDecision {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return hookDecision{}
	}
	var d hookDecision
	if json.Unmarshal(trimmed, &d) != nil {
		return hookDecision{}
	}
	d.Block = strings.EqualFold(d.Decision, "deny") || strings.EqualFold(d.Decision, "block")
	return d
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 2048 {
		s = s[:2048]
	}
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func globMatch(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	starP, starT, i, j := -1, 0, 0, 0
	for j < len(t) {
		switch {
		case i < len(p) && (p[i] == t[j] || p[i] == '?'):
			i++
			j++
		case i < len(p) && p[i] == '*':
			starP, starT, i = i, j, i+1
		case starP >= 0:
			i = starP + 1
			starT++
			j = starT
		default:
			return false
		}
	}
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}
