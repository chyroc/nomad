package cli

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/hooks"
)

// applyPromptHooks runs UserPromptSubmit hooks, returning the possibly
// augmented prompt and ok=false when a hook blocks the turn.
func (a *App) applyPromptHooks(ctx context.Context, sessionID, text string) (string, bool) {
	out := a.runHooks(ctx, hooks.EventUserPromptSubmit, hooks.Envelope{
		SessionID: sessionID,
		CWD:       a.paths.Workspace,
		Event:     hooks.EventUserPromptSubmit,
		Prompt:    text,
	}, nil)
	if out.Blocked {
		return text, false
	}
	if strings.TrimSpace(out.AdditionalContext) != "" {
		text = text + "\n\n# Hook-provided context\n" + strings.TrimSpace(out.AdditionalContext)
	}
	return text, true
}

// runSessionStartHook runs SessionStart hooks once for a new session.
func (a *App) runSessionStartHook(ctx context.Context, sessionID string, firstPrompt string) (string, bool) {
	out := a.runHooks(ctx, hooks.EventSessionStart, hooks.Envelope{
		SessionID: sessionID,
		CWD:       a.paths.Workspace,
		Event:     hooks.EventSessionStart,
		Prompt:    firstPrompt,
	}, nil)
	if out.Blocked {
		return firstPrompt, false
	}
	if strings.TrimSpace(out.AdditionalContext) != "" {
		firstPrompt = firstPrompt + "\n\n# Hook-provided context\n" + strings.TrimSpace(out.AdditionalContext)
	}
	return firstPrompt, true
}

func (a *App) runHooks(ctx context.Context, event string, env hooks.Envelope, list []hooks.Hook) hooks.Outcome {
	if a.hookRunner == nil {
		return hooks.Outcome{}
	}
	if list == nil && a.hookConfig != nil {
		list = a.hookConfig.ForEvent(event)
	}
	if len(list) == 0 {
		return hooks.Outcome{}
	}
	return a.hookRunner.Run(ctx, list, env)
}

// preToolHook adapts tool pre-hooks for the runner.
// installToolHooks attaches the pre/post tool hook adapters to a runner.
func (a *App) installToolHooks(r *ark.Runner) {
	r.SetToolHooks(a.preToolHook(r.SessionID()), a.postToolHook(r.SessionID()))
}

// runStopHooks fires Stop hooks after a turn ends (best effort).
func (a *App) runStopHooks(ctx context.Context, sessionID string) {
	out := a.runHooks(ctx, hooks.EventStop, hooks.Envelope{
		SessionID: sessionID,
		CWD:       a.paths.Workspace,
		Event:     hooks.EventStop,
	}, nil)
	if strings.TrimSpace(out.AdditionalContext) != "" {
		a.printf("%s%s%s\n", cDim, strings.TrimSpace(out.AdditionalContext), cReset)
	}
}

func (a *App) preToolHook(sessionID string) func(context.Context, string, json.RawMessage) hooks.Outcome {
	return func(ctx context.Context, name string, input json.RawMessage) hooks.Outcome {
		return a.runHooks(ctx, hooks.EventPreToolUse, hooks.Envelope{
			SessionID: sessionID,
			CWD:       a.paths.Workspace,
			Event:     hooks.EventPreToolUse,
			ToolName:  name,
			ToolInput: input,
		}, nil)
	}
}

// postToolHook adapts tool post-hooks for the runner.
func (a *App) postToolHook(sessionID string) func(context.Context, string, json.RawMessage, bool) hooks.Outcome {
	return func(ctx context.Context, name string, input json.RawMessage, isError bool) hooks.Outcome {
		out := a.runHooks(ctx, hooks.EventPostToolUse, hooks.Envelope{
			SessionID: sessionID,
			CWD:       a.paths.Workspace,
			Event:     hooks.EventPostToolUse,
			ToolName:  name,
			ToolInput: input,
		}, nil)
		if isError && strings.TrimSpace(out.Reason) == "" && out.Blocked {
			out.Blocked = false
		}
		return out
	}
}
