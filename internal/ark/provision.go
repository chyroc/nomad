package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Profile is the locally cached provisioned resource set.
type Profile struct {
	EnvironmentID string         `json:"environment_id"`
	AgentID       string         `json:"agent_id"`
	Model         string         `json:"model"`
	Effort        string         `json:"effort,omitempty"`
	SkillBindings []SkillBinding `json:"skill_bindings,omitempty"`
}

// codingSystemPrompt is nomad's built-in agent system prompt: an
// interactive CLI coding assistant that explores before editing, acts
// carefully with risky operations and reports concisely.
const codingSystemPrompt = `You are Nomad, an interactive command-line coding agent operating inside the user's local git repository.

# Doing tasks
- Engineering requests are concrete work, not questions: when the user asks for a rename, refactor or behavior change, edit the code instead of describing the edit. Interpret vague requests using repository context — build files, README, naming and surrounding conventions.
- Explore before changing: read the relevant files and search for existing patterns with the dedicated tools. Never propose or make changes to a file you have not read; do not assume structure or invent APIs.
- Make the smallest change that fully satisfies the request and match the surrounding code's style, naming and idiom. Do not add features, refactors, abstractions, config options, error handling or doc comments beyond what the task needs; three similar lines beat a premature helper. When deleting code, remove it completely instead of leaving commented-out code or stubs.
- Validate inputs only at trust boundaries: user input, file contents and network responses.
- On failure, read the actual error and inspect the relevant state, then apply a focused fix; do not blindly retry the same action or abandon the task at the first obstacle. When genuinely blocked, report the blocker clearly.
- After changing code, verify with the project's build, tests, linters or type-checkers through the shell tool, and fix failures you introduced. If verification is not possible, say so explicitly.
- Never print, log or exfiltrate secrets, tokens or credentials found in the environment or repository. Avoid introducing common security issues such as command injection, path traversal or unescaped interpolation into shell commands.

# Executing actions with care
- Weigh reversibility and blast radius before acting. Local, reversible edits and read-only checks are safe to run; hard-to-reverse, shared-state or outward-facing actions require explicit user confirmation first.
- One approval covers only the approved action in its context; it is not standing authorization for similar actions later. Durable authorization must come from repository instructions.
- Confirm before destructive or risky actions, including: recursive deletes such as rm -rf, overwriting uncommitted work, git reset --hard and force pushes, amending published commits, dropping databases or cloud resources, killing processes, and sending messages, pull requests, issues or comments visible to others.
- Never use destructive shortcuts to work around a problem, such as skipping hooks or safety checks; fix the root cause. When repository state is unexpected — unfamiliar files, branches, locks or processes — investigate before acting, and resolve merge conflicts rather than discarding work.

# Using your tools
- Prefer dedicated tools over the shell for file work: use read instead of cat, head, tail or sed; edit instead of sed or awk; write instead of heredocs and echo redirection; glob instead of find; grep instead of rg or shell grep. Reserve bash for commands that genuinely need a shell, such as builds, tests and git operations.
- Use real tools instead of guessing: executing a command or reading a file beats assuming an outcome.
- Make independent tool calls in parallel in the same message; make dependent calls sequentially.
- The workspace root is the only file system scope; stay inside it and use absolute paths.

# Tone and style
- Do not use emojis unless the user explicitly asks for them.
- Be concise; reply in the user's language, defaulting to terse Markdown.
- Reference code as path:line.
- Do not end a sentence with a colon immediately before a tool call.

# Output efficiency
- Lead with the result or action; skip filler, restatement and narration of steps the user can see.
- Limit prose to decisions that need user input, milestone status, and errors or blockers that change the plan.
- Report what changed, how it was verified, and anything the user must do next; show file paths and commands rather than pasting large outputs.`

// toolConfig is one entry of the built-in coding toolset.
type toolConfig struct {
	Name             string      `json:"name"`
	Enabled          bool        `json:"enabled"`
	PermissionPolicy interface{} `json:"permission_policy"`
}

func alwaysAllowTool(name string) toolConfig {
	return toolConfig{Name: name, Enabled: true, PermissionPolicy: map[string]string{"type": "always_allow"}}
}

func codingTools() []toolConfig {
	names := []string{"bash", "read", "write", "edit", "glob", "grep"}
	out := make([]toolConfig, 0, len(names))
	for _, n := range names {
		out = append(out, alwaysAllowTool(n))
	}
	return out
}

// environmentName is the fixed self-hosted environment nomad reuses.
const environmentName = "nomad-self-hosted"

// EnsureEnvironment creates a self_hosted environment if id is empty,
// reusing an existing one with the standard name on a 409 conflict.
func (c *Client) EnsureEnvironment(ctx context.Context, id string) (string, error) {
	if id != "" {
		if _, err := c.getEnvironment(ctx, id); err == nil {
			return id, nil
		}
	}
	body := map[string]interface{}{
		"name":        environmentName,
		"description": "Auto-provisioned by nomad CLI",
		"config":      map[string]string{"type": "self_hosted"},
	}
	var out struct {
		ID string `json:"id"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/environments", body, &out)
	if err != nil {
		if !isConflictError(err) {
			return "", fmt.Errorf("ma: create environment: %w", err)
		}
		existing, ferr := c.findEnvironmentByName(ctx, environmentName)
		if ferr != nil || existing == "" {
			return "", fmt.Errorf("ma: create environment: %w (and lookup of existing %q failed: %v)",
				err, environmentName, ferr)
		}
		return existing, nil
	}
	if out.ID == "" {
		return "", fmt.Errorf("ma: create environment returned no id")
	}
	return out.ID, nil
}

// findEnvironmentByName lists environments and returns the id of the
// first one matching name.
func (c *Client) findEnvironmentByName(ctx context.Context, name string) (string, error) {
	var list struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/environments", nil, &list); err != nil {
		return "", err
	}
	for _, e := range list.Data {
		if e.Name == name && e.ID != "" {
			return e.ID, nil
		}
	}
	return "", nil
}

func isConflictError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "HTTP 409")
}

func (c *Client) getEnvironment(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, "/environments/"+id, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// EnsureAgent creates the coding agent if id is empty; otherwise it is
// validated. The selected model is applied at session creation via
// overrides, so changing the model does not require recreating the agent.
func (c *Client) EnsureAgent(ctx context.Context, id, model string, extraSystem string) (string, error) {
	systemPrompt := codingSystemPrompt
	if strings.TrimSpace(extraSystem) != "" {
		systemPrompt += "\n\n" + strings.TrimSpace(extraSystem)
	}
	if id != "" {
		if _, err := c.getAgent(ctx, id); err == nil {
			return id, nil
		}
	}
	body := map[string]interface{}{
		"name":   "nomad-coding-agent",
		"model":  map[string]string{"id": model},
		"system": systemPrompt,
		"tools": []map[string]interface{}{{
			"type":    "agent_toolset_20260701",
			"configs": codingTools(),
		}},
	}
	var out struct {
		ID string `json:"id"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/agents", body, &out)
	if err != nil {
		if !isConflictError(err) {
			return "", fmt.Errorf("ma: create agent: %w", err)
		}
		existing, ferr := c.findAgentByName(ctx, "nomad-coding-agent")
		if ferr != nil || existing == "" {
			return "", fmt.Errorf("ma: create agent: %w (lookup of existing agent failed: %v)", err, ferr)
		}
		return existing, nil
	}
	if out.ID == "" {
		return "", fmt.Errorf("ma: create agent returned no id")
	}
	return out.ID, nil
}

// findAgentByName lists agents and returns the id of the first match.
func (c *Client) findAgentByName(ctx context.Context, name string) (string, error) {
	var list struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/agents", nil, &list); err != nil {
		return "", err
	}
	for _, a := range list.Data {
		if a.Name == name && a.ID != "" {
			return a.ID, nil
		}
	}
	return "", nil
}

func (c *Client) getAgent(ctx context.Context, id string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, "/agents/"+id, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Provision ensures both resources exist and returns the cached profile.
func (c *Client) Provision(ctx context.Context, p Profile) (Profile, error) {
	envID, err := c.EnsureEnvironment(ctx, p.EnvironmentID)
	if err != nil {
		return p, err
	}
	p.EnvironmentID = envID
	if p.Model == "" {
		if models, err := c.ListModels(ctx); err == nil {
			if m := preferredModel(models); m != "" {
				p.Model = m
			}
		}
	}
	agentID, err := c.EnsureAgent(ctx, p.AgentID, p.Model, "")
	if err != nil {
		return p, err
	}
	p.AgentID = agentID
	return p, nil
}
