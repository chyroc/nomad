package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	ggoal "github.com/chyroc/nomad/internal/goal"
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
	a.installToolHooks(runner)
	if resumeID == "" {
		var ok bool
		prompt, ok = a.runSessionStartHook(ctx, a.sessionID, prompt)
		if !ok {
			return fmt.Errorf("SessionStart hook blocked the run")
		}
	}
	prompt, allowed := a.applyPromptHooks(ctx, a.sessionID, prompt)
	if !allowed {
		return fmt.Errorf("UserPromptSubmit hook blocked the prompt")
	}
	defer runner.Close()
	a.loadGoal()
	if cond := strings.TrimSpace(a.opts.Goal); cond != "" {
		cond, err := ggoal.ValidateCondition(cond)
		if err != nil {
			return err
		}
		st := ggoal.New(a.sessionID, cond, time.Now())
		if err := a.goalStore.Save(st); err != nil {
			return err
		}
		a.goal = st
	}

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

	numTurns := 1
	runTurn := func(text string, _ []loop.Attachment) error {
		numTurns++
		return runner.Run(ctx, text, nil)
	}
	emitCheck := func(c goalCheck) {
		if a.opts.OutputFormat == FormatStreamJSON {
			stream.GoalCheck(c.Iterations, c.Verdict, c.Reason)
		}
	}

	err = runner.Run(ctx, prompt, atts)
	if err == nil && a.goal != nil && a.goal.Active() {
		err = a.continueGoal(ctx, transcript, runTurn, emitCheck)
	}

	subtype := SubtypeSuccess
	if errors.Is(err, loop.ErrMaxTurns) {
		subtype = SubtypeMaxTurns
	} else if err != nil {
		subtype = SubtypeErrorRun
	}

	finalGoal := a.goal
	if st, loadErr := a.goalStore.Load(a.sessionID); loadErr == nil && st != nil {
		finalGoal = st
	}

	switch a.opts.OutputFormat {
	case FormatStreamJSON:
		usage := lastUsage(transcript, a.sessionID)
		stream.Result(lastText(transcript, a.sessionID), a.sessionID, usage, subtype, numTurns)
	case FormatJSON:
		out := map[string]interface{}{
			"type":       "result",
			"subtype":    subtype,
			"session_id": a.sessionID,
			"result":     collect.Text.String(),
			"num_turns":  numTurns,
		}
		if subtype != SubtypeSuccess {
			out["is_error"] = true
			out["result"] = err.Error()
		}
		if subtype == SubtypeMaxTurns {
			out["errors"] = []string{"Reached maximum number of turns"}
			out["terminal_reason"] = "max_turns"
		} else {
			out["terminal_reason"] = "completed"
		}
		if collect.Usage != nil {
			out["usage"] = collect.Usage
		}
		if finalGoal != nil {
			out["goal"] = map[string]interface{}{
				"condition":  finalGoal.Condition,
				"status":     finalGoal.Status,
				"iterations": finalGoal.Iterations,
				"reason":     finalGoal.LastReason,
			}
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
	if f, err := os.OpenFile(filepath.Join(a.paths.DataDir, "worker.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		opts.WorkerLogger = log.New(f, "", log.LstdFlags)
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
