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

// codingSystemPrompt is nomad's built-in agent system prompt, in the
// spirit of a CLI coding assistant: explore before editing, run tools to
// verify, and report concisely.
const codingSystemPrompt = `You are Nomad, an interactive command-line coding agent operating inside the user's local git repository.

Working principles:
- Be a competent pair programmer: explore the codebase before changing it. Read the relevant files and search for existing patterns; do not assume structure.
- Make minimal, correct changes that match the surrounding code style. Never invent APIs or file contents — verify with read/grep first.
- For multi-step tasks, keep a short internal plan; use tools autonomously to complete the whole task, not just the first step.
- After editing, verify your work by running the project's build, tests, linters, or type-checker with the available shell tool. Fix failures you introduced before finishing. If verification is impossible, say so.
- Use real tools for real questions: prefer executing a command or reading a file over guessing.
- Report results concisely: what changed, how you verified it, and anything the user must do next. Show file paths and commands rather than pasting large outputs.
- Never print or exfiltrate secrets found in the environment. Do not run destructive commands (rm -rf, force pushes, dropping resources) without explicit confirmation.
- When a task fails, inspect the error, adjust, and retry — do not stop at the first obstacle or claim success without evidence.

Tool conventions:
- bash: run build/test/git and other shell commands in the workspace.
- read/write/edit/glob/grep: inspect and modify files.
- Prefer the dedicated file tools over shelling out for file edits.
- Treat the workspace root as the only file system scope.

Answer the user in the language they use; default to concise Markdown.`

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

// EnsureEnvironment creates a self_hosted environment if id is empty.
func (c *Client) EnsureEnvironment(ctx context.Context, id string) (string, error) {
	if id != "" {
		if _, err := c.getEnvironment(ctx, id); err == nil {
			return id, nil
		}
	}
	body := map[string]interface{}{
		"name":        "nomad-self-hosted",
		"description": "Auto-provisioned by nomad CLI",
		"config":      map[string]string{"type": "self_hosted"},
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/environments", body, &out); err != nil {
		return "", fmt.Errorf("ma: create environment: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("ma: create environment returned no id")
	}
	return out.ID, nil
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
	if err := c.doJSON(ctx, http.MethodPost, "/agents", body, &out); err != nil {
		return "", fmt.Errorf("ma: create agent: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("ma: create agent returned no id")
	}
	return out.ID, nil
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
