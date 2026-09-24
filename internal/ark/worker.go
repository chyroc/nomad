package ark

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/nomad/internal/hooks"
	"github.com/chyroc/nomad/internal/settings"
	"github.com/volcengine/ark-runtime-go/arkruntime/selfhosted"
	"github.com/volcengine/ark-runtime-go/arkruntime/toolset"
)

// readOnlyTools never mutate the workspace.
var readOnlyTools = map[string]bool{
	"read": true, "glob": true, "grep": true,
}

// gatedTool wraps a toolset tool with a permission decision, hooks and
// a turn cap.
type gatedTool struct {
	inner            toolset.Tool
	decide           func(name string, input json.RawMessage) (bool, string)
	preHookProvider  func() func(context.Context, string, json.RawMessage) hooks.Outcome
	postHookProvider func() func(context.Context, string, json.RawMessage, bool) hooks.Outcome
	collect          func(hooks.Outcome)
	counter          *turnCounter
	max              int
}

type turnCounter struct{ n int }

// ruleSet is a mutex-guarded mutable collection of settings rules so
// newly granted "always allow" rules take effect on the live runner.
type ruleSet struct {
	mu    sync.Mutex
	allow []settings.Rule
	deny  []settings.Rule
}

func (s *ruleSet) snapshot() ([]settings.Rule, []settings.Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]settings.Rule(nil), s.allow...), append([]settings.Rule(nil), s.deny...)
}

// AddAllowRule appends an allow rule to the live set.
func (s *ruleSet) AddAllowRule(rule settings.Rule) {
	s.mu.Lock()
	s.allow = append(s.allow, rule)
	s.mu.Unlock()
}

func (g *gatedTool) Name() string { return g.inner.Name() }

func (g *gatedTool) Execute(ctx context.Context, input json.RawMessage) toolset.Result {
	if g.max > 0 {
		g.counter.n++
		if g.counter.n > g.max {
			return toolset.ErrorResult("reached --max-turns tool-call limit")
		}
	}
	allow, reason := g.decide(g.inner.Name(), input)
	if !allow {
		return toolset.ErrorResult("permission denied: " + reason)
	}
	var pre func(context.Context, string, json.RawMessage) hooks.Outcome
	if g.preHookProvider != nil {
		pre = g.preHookProvider()
	}
	if pre != nil {
		if out := pre(ctx, g.inner.Name(), input); out.Blocked {
			return toolset.ErrorResult("hook denied: " + out.Reason)
		} else if g.collect != nil {
			g.collect(out)
		}
	}
	result := g.inner.Execute(ctx, input)
	var post func(context.Context, string, json.RawMessage, bool) hooks.Outcome
	if g.postHookProvider != nil {
		post = g.postHookProvider()
	}
	if post != nil {
		if out := post(ctx, g.inner.Name(), input, result.IsError); g.collect != nil {
			g.collect(out)
		}
	}
	return result
}

// newGatedToolSet builds the default coding toolset with every tool
// wrapped by the permission gate and an optional turn cap.
func newGatedToolSet(workspace string, toolTimeout time.Duration, g gateOptions) (*toolset.Set, error) {
	limits := toolset.DefaultLimits()
	resolver, err := toolset.NewResolverWithOptions(workspace, false)
	if err != nil {
		return nil, err
	}
	bash, err := toolset.NewBashTool(toolset.Options{Workdir: workspace, ToolTimeout: toolTimeout})
	if err != nil {
		return nil, err
	}
	tools := []toolset.Tool{
		bash,
		toolset.NewReadTool(resolver, limits),
		toolset.NewWriteTool(resolver, limits),
		toolset.NewEditTool(resolver, limits),
		toolset.NewGlobTool(resolver, limits),
		toolset.NewGrepTool(resolver, limits),
	}
	set, err := toolset.NewDefault(toolset.Options{Workdir: workspace, ToolTimeout: toolTimeout})
	if err != nil {
		return nil, err
	}
	counter := &turnCounter{}
	byName := map[string]toolset.Tool{}
	for _, t := range tools {
		byName[t.Name()] = t
	}
	for _, name := range []string{"bash", "read", "write", "edit", "glob", "grep"} {
		set.Register(&gatedTool{
			inner:            byName[name],
			decide:           g.decide(),
			preHookProvider:  g.preHookProvider,
			postHookProvider: g.postHookProvider,
			collect:          g.collect,
			counter:          counter,
			max:              g.maxToolTurns,
		})
	}
	return set, nil
}

// Permission answers returned by an interactive Ask callback.
const (
	AnswerDeny    = "deny"
	AnswerAllow   = "allow"
	AnswerOnce    = "once"
	AnswerSession = "session"
)

// gateOptions collects every input to one permission decision.
type gateOptions struct {
	mode             PermissionMode
	allowed          map[string]bool
	disallowed       map[string]bool
	sessionAllowed   map[string]bool
	allowRules       []settings.Rule
	denyRules        []settings.Rule
	rulesProvider    func() ([]settings.Rule, []settings.Rule)
	sessionGranted   func() map[string]bool
	ask              func(string, string) string
	preHookProvider  func() func(context.Context, string, json.RawMessage) hooks.Outcome
	postHookProvider func() func(context.Context, string, json.RawMessage, bool) hooks.Outcome
	collect          func(hooks.Outcome)
	maxToolTurns     int
}

// decide builds the per-call decision closure with live rule snapshots.
func (g gateOptions) decide() func(string, json.RawMessage) (bool, string) {
	return func(name string, input json.RawMessage) (bool, string) {
		allowRules, denyRules := g.allowRules, g.denyRules
		if g.rulesProvider != nil {
			allowRules, denyRules = g.rulesProvider()
		}
		sessionAllowed := g.sessionAllowed
		if g.sessionGranted != nil {
			sessionAllowed = g.sessionGranted()
		}
		return decidePermission(gateOptions{
			mode:           g.mode,
			allowed:        g.allowed,
			disallowed:     g.disallowed,
			sessionAllowed: sessionAllowed,
			allowRules:     allowRules,
			denyRules:      denyRules,
			ask:            g.ask,
		}, name, input)
	}
}

// decidePermission implements the permission policy decision:
// flag disable, settings deny, settings allow, flag allow, session
// grant, mode policy, then interactive ask.
func decidePermission(g gateOptions, name string, input json.RawMessage) (bool, string) {
	if g.disallowed[name] {
		return false, "tool is disabled"
	}
	if rule, ok := settings.MatchAny(g.denyRules, name, input); ok {
		return false, "denied by settings rule " + rule.String()
	}
	if rule, ok := settings.MatchAny(g.allowRules, name, input); ok {
		_ = rule
		return true, ""
	}
	if len(g.allowed) > 0 && g.allowed[name] {
		return true, ""
	}
	if len(g.sessionAllowed) > 0 && g.sessionAllowed[name] {
		return true, ""
	}
	switch g.mode {
	case PermBypass:
		return true, ""
	case PermPlan:
		if readOnlyTools[name] {
			return true, ""
		}
		return false, "plan mode forbids modifying tools"
	case PermEdit:
		if readOnlyTools[name] || name == "write" || name == "edit" {
			return true, ""
		}
	case PermDefault, "":
		if readOnlyTools[name] {
			return true, ""
		}
	}
	if len(g.allowed) > 0 {
		return false, "tool not in allow list"
	}
	if g.ask == nil {
		return false, "non-interactive session denied tool (use --permission-mode bypassPermissions to allow)"
	}
	switch strings.ToLower(strings.TrimSpace(g.ask(name, compactArgs(input)))) {
	case AnswerAllow, AnswerOnce, AnswerSession, "y", "yes":
		return true, ""
	}
	return false, "user denied tool execution"
}

// startToolWorker launches the SDK session tool loop with gated tools.
func (r *Runner) startToolWorker(ctx context.Context) (<-chan selfhosted.ToolCallResult, *selfhosted.SessionToolRunner, error) {
	runner := selfhosted.NewSessionToolRunner(ctx, r.api, r.sessionID, selfhosted.SessionToolRunnerOptions{
		Tools:             r.tools,
		ToolTimeout:       r.cfg.ToolTimeout,
		EventPollInterval: 2 * time.Second,
	})
	results := make(chan selfhosted.ToolCallResult, 16)
	go func() {
		defer close(results)
		for runner.Next() {
			select {
			case <-ctx.Done():
				return
			case results <- runner.Current():
			}
		}
	}()
	return results, runner, nil
}

func compactArgs(input json.RawMessage) string {
	s := strings.TrimSpace(string(input))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
