package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

func (a *App) runHeadless(ctx context.Context) error {
	prompt := strings.Join(a.opts.PromptArgs, " ")
	if strings.TrimSpace(prompt) == "" {
		return errors.New("no prompt provided")
	}

	transcript, err := store.NewSessionStore(a.paths.SessionsDir())
	if err != nil {
		return err
	}

	resumeID := a.resumeSessionID(transcript)
	runner, err := a.newRunner(ctx, resumeID, nil)
	if err != nil {
		return err
	}
	a.sessionID = runner.SessionID()
	defer runner.Close()

	var renderer loop.Observer
	var collect *CollectRenderer
	var stream *StreamRenderer

	switch a.opts.OutputFormat {
	case FormatStreamJSON:
		stream = NewStreamRenderer(a.out, a.model, a.sessionID, a.opts.PermissionMode)
		stream.Init(a.paths.Workspace)
		renderer = stream
	case FormatJSON:
		collect = &CollectRenderer{}
		renderer = collect
	default:
		renderer = &textRenderer{out: a.out}
	}
	runner.Subscribe(loop.ObserverFunc(func(ev loop.Event) {
		_ = transcript.Append(a.sessionID, ev)
		renderer.OnEvent(ev)
	}))

	var atts []loop.Attachment
	for _, img := range a.opts.Images {
		att := loop.Attachment{Kind: "image", Source: "file", Path: img}
		if strings.HasPrefix(img, "http://") || strings.HasPrefix(img, "https://") {
			att = loop.Attachment{Kind: "image", Source: "url", URL: img}
		} else {
			uploaded, uerr := runner.UploadAttachment(ctx, att)
			if uerr != nil {
				return fmt.Errorf("upload image %s: %w", img, uerr)
			}
			att = uploaded
		}
		atts = append(atts, att)
	}

	err = runner.Run(ctx, prompt, atts)

	switch a.opts.OutputFormat {
	case FormatStreamJSON:
		usage := lastUsage(transcript, a.sessionID)
		stream.Result(lastText(transcript, a.sessionID), a.sessionID, usage, err != nil)
	case FormatJSON:
		out := map[string]interface{}{
			"type":       "result",
			"subtype":    "success",
			"session_id": a.sessionID,
			"result":     collect.Text.String(),
			"num_turns":  1,
		}
		if err != nil {
			out["subtype"] = "error_during_execution"
			out["is_error"] = true
			out["result"] = err.Error()
		}
		if collect.Usage != nil {
			out["usage"] = collect.Usage
		}
		_ = json.NewEncoder(a.out).Encode(out)
	default:
		if err == nil {
			fmt.Fprintln(a.out)
		}
	}
	return err
}

func (a *App) newRunner(ctx context.Context, remoteSessionID string, ask func(string, string) string) (*ark.Runner, error) {
	allowed, disallowed := a.toolSets()
	opts := ark.RunnerOptions{
		Profile:         a.ctrl.Profile,
		SessionID:       remoteSessionID,
		Workspace:       a.paths.Workspace,
		SystemPrompt:    a.sessionSystem(ctx),
		Model:           a.model,
		Permission:      a.permMode(),
		AllowedTools:    allowed,
		Disallowed:      disallowed,
		Ask:             ask,
		MaxToolTurns:    a.opts.MaxTurns,
		ReasoningEffort: a.opts.ReasoningEffort,
	}
	if a.appSettings != nil {
		opts.AllowRules = a.appSettings.AllowRules
		opts.DenyRules = a.appSettings.DenyRules
	}
	return a.ctrl.NewRunner(ctx, opts)
}

// resumeSessionID resolves --resume/--continue to a remote session id.
func (a *App) resumeSessionID(transcript *store.SessionStore) string {
	if a.opts.Resume != "" {
		return a.opts.Resume
	}
	if a.opts.SessionID != "" {
		return a.opts.SessionID
	}
	if a.opts.Continue {
		if recs, err := transcript.List(); err == nil && len(recs) > 0 {
			return recs[0].ID
		}
	}
	return ""
}

func lastText(s *store.SessionStore, id string) string {
	evs, err := s.Load(id)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, ev := range evs {
		if ev.Kind == loop.EvAssistantChunk {
			b.WriteString(ev.Content)
		}
	}
	return b.String()
}

func lastUsage(s *store.SessionStore, id string) *loop.Usage {
	evs, err := s.Load(id)
	if err != nil {
		return nil
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == loop.EvTurnEnd && evs[i].Usage != nil {
			return evs[i].Usage
		}
	}
	return nil
}

// textRenderer prints plain streaming text for --output-format text.
type textRenderer struct {
	out interface{ Write(p []byte) (int, error) }
}

func (t *textRenderer) OnEvent(ev loop.Event) {
	switch ev.Kind {
	case loop.EvAssistantChunk:
		t.out.Write([]byte(ev.Content))
	case loop.EvToolCall:
		if ev.ToolCall != nil {
			fmt.Fprintf(t.out, "\n[tool %s] %s\n", ev.ToolCall.Name, oneLine(ev.ToolCall.Arguments, 120))
		}
	case loop.EvToolResult:
		fmt.Fprintf(t.out, "[result] %s\n", oneLine(ev.Result, 200))
	case loop.EvError:
		fmt.Fprintf(t.out, "[error] %s\n", ev.Content)
	}
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
