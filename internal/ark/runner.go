package ark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/volcengine/ark-runtime-go/arkruntime/model/session"
	"github.com/volcengine/ark-runtime-go/arkruntime/selfhosted"
	"github.com/volcengine/ark-runtime-go/arkruntime/toolset"

	"github.com/chyroc/nomad/internal/loop"
)

// PermissionMode controls which tool calls run without asking.
type PermissionMode string

const (
	PermDefault PermissionMode = "default"     // ask for non-read tools
	PermEdit    PermissionMode = "acceptEdits" // auto-allow file edits, ask for bash
	PermBypass  PermissionMode = "bypassPermissions"
	PermPlan    PermissionMode = "plan"
)

// RunConfig holds per-turn options.
type RunConfig struct {
	Model           string
	SystemPrompt    string
	PermissionMode  PermissionMode
	AllowedTools    map[string]bool
	DisallowedTools map[string]bool
	ToolTimeout     time.Duration
	Ask             func(toolName, arguments string) string
}

// Runner drives one managed-agents session as a local worker.
type Runner struct {
	client          *Client
	api             *selfhosted.ClientAPI
	tools           *toolset.Set
	reasoningEffort string

	sessionID string
	cfg       RunConfig

	obs []loop.Observer

	usage loop.Usage
}

// RunnerOptions constructs a runner for an existing or new session.
type RunnerOptions struct {
	Client          *Client
	Profile         Profile
	SessionID       string
	Workspace       string
	SystemPrompt    string
	Model           string
	ReasoningEffort string
	Permission      PermissionMode
	AllowedTools    map[string]bool
	Disallowed      map[string]bool
	Ask             func(toolName, arguments string) string
	ToolTimeout     time.Duration
	MaxToolTurns    int
}

// NewRunner creates (or attaches to) a session and wires the worker.
func NewRunner(ctx context.Context, o RunnerOptions) (*Runner, error) {
	if o.Client == nil {
		return nil, errors.New("ma: client is required")
	}
	if o.Workspace == "" {
		return nil, errors.New("ma: workspace is required")
	}
	toolTimeout := o.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = 120 * time.Second
	}
	perm := o.Permission
	if perm == "" {
		perm = PermBypass
	}
	allowed, disallowed := o.AllowedTools, o.Disallowed
	var sessionMu sync.Mutex
	sessionAllowed := map[string]bool{}
	var ask func(string, string) string
	if o.Ask != nil {
		ask = func(name, args string) string {
			answer := o.Ask(name, args)
			if strings.EqualFold(strings.TrimSpace(answer), "session") {
				sessionMu.Lock()
				sessionAllowed[name] = true
				sessionMu.Unlock()
				return "allow"
			}
			return answer
		}
	}
	decide := func(name string, input json.RawMessage) (bool, string) {
		sessionMu.Lock()
		sess := sessionAllowed
		sessionMu.Unlock()
		return decidePermission(perm, allowed, disallowed, sess, ask, name, input)
	}
	tools, err := newGatedToolSet(o.Workspace, toolTimeout, decide, o.MaxToolTurns)
	if err != nil {
		return nil, fmt.Errorf("ma: init toolset: %w", err)
	}
	api := selfhosted.NewClientAPI(o.Client.Runtime())

	r := &Runner{
		client:          o.Client,
		api:             api,
		tools:           tools,
		reasoningEffort: o.ReasoningEffort,
		cfg: RunConfig{
			Model:           o.Model,
			SystemPrompt:    o.SystemPrompt,
			PermissionMode:  perm,
			AllowedTools:    allowed,
			DisallowedTools: disallowed,
			ToolTimeout:     toolTimeout,
			Ask:             o.Ask,
		},
	}

	if o.SessionID != "" {
		r.sessionID = o.SessionID
		if _, err := api.GetSession(ctx, selfhosted.GetSessionRequest{SessionID: o.SessionID}); err != nil {
			return nil, fmt.Errorf("ma: get session: %w", err)
		}
	} else {
		if err := r.createSession(ctx, o.Profile); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Runner) createSession(ctx context.Context, p Profile) error {
	modelID := r.cfg.Model
	if modelID == "" {
		modelID = p.Model
	}
	agentRef := session.AgentRef{Type: "agent", ID: session.NewOptString(p.AgentID)}
	overrides := map[string]interface{}{}
	if modelID != "" {
		modelOverride := map[string]interface{}{"id": modelID}
		if effort := r.resolveEffort(ctx, modelID); effort != "" {
			modelOverride["reasoning_effort"] = effort
		}
		overrides["model"] = modelOverride
	}
	if strings.TrimSpace(r.cfg.SystemPrompt) != "" {
		overrides["system"] = codingSystemPrompt + "\n\n" + strings.TrimSpace(r.cfg.SystemPrompt)
	}

	ref := session.NewAgentRefAgentIdentifier(agentRef)
	req := &session.CreateSessionRequest{
		EnvironmentID: session.NewOptString(p.EnvironmentID),
	}
	if len(overrides) > 0 {
		raw, _ := json.Marshal(map[string]interface{}{
			"type": "agent_with_overrides",
			"id":   p.AgentID,
		})
		raw, _ = injectOverrides(raw, overrides)
		if err := json.Unmarshal(raw, &agentRef); err != nil {
			return fmt.Errorf("ma: build agent override: %w", err)
		}
		ref = session.NewAgentRefAgentIdentifier(agentRef)
	}
	req.Agent = session.NewOptAgentIdentifier(ref)

	sess, err := r.client.Runtime().CreateSession(ctx, req)
	if err != nil {
		return fmt.Errorf("ma: create session: %w", err)
	}
	r.sessionID = sess.ID
	return nil
}

func injectOverrides(raw []byte, overrides map[string]interface{}) ([]byte, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	for k, v := range overrides {
		m[k] = v
	}
	return json.Marshal(m)
}

// SessionID implements loop.Runner.
func (r *Runner) SessionID() string { return r.sessionID }

// Subscribe implements loop.Runner.
func (r *Runner) Subscribe(o loop.Observer) { r.obs = append(r.obs, o) }

// Close releases tool resources.
func (r *Runner) Close() error {
	if r.tools != nil {
		return r.tools.Close()
	}
	return nil
}

func (r *Runner) emit(ev loop.Event) {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	for _, o := range r.obs {
		o.OnEvent(ev)
	}
}

// Interrupt aborts the in-flight turn server-side.
func (r *Runner) Interrupt(ctx context.Context) error {
	return r.client.Runtime().SendSessionEventRaw(ctx, r.sessionID, map[string]interface{}{
		"type":   "user.interrupt",
		"reason": "user_cancelled",
	})
}

// Run drives one user turn with optional image attachments.
func (r *Runner) Run(ctx context.Context, text string, attachments []loop.Attachment) error {
	r.emit(loop.Event{Kind: loop.EvUserMessage, Content: text})
	r.usage = loop.Usage{}

	toolResults, toolRunner, err := r.startToolWorker(ctx)
	if err != nil {
		return err
	}
	defer toolRunner.Close()

	stream, err := r.api.StreamEvents(ctx, selfhosted.StreamEventsRequest{SessionID: r.sessionID})
	if err != nil {
		return fmt.Errorf("ma: open event stream: %w", err)
	}
	defer stream.Close()

	if err := r.sendUserMessage(ctx, text, attachments); err != nil {
		return err
	}

	return r.pump(ctx, stream, toolResults)
}

func (r *Runner) sendUserMessage(ctx context.Context, text string, attachments []loop.Attachment) error {
	content := []map[string]interface{}{{"type": "text", "text": text}}
	for _, a := range attachments {
		if a.Kind == "image" {
			source := map[string]interface{}{"type": "file", "file_id": a.FileID}
			content = append(content, map[string]interface{}{"type": "image", "source": source})
		}
	}
	return r.client.Runtime().SendSessionEventRaw(ctx, r.sessionID, map[string]interface{}{
		"type":    "user.message",
		"content": content,
	})
}

// UploadAttachment uploads a local image file and returns a populated
// attachment carrying the backend file id.
func (r *Runner) UploadAttachment(ctx context.Context, a loop.Attachment) (loop.Attachment, error) {
	if a.FileID != "" {
		return a, nil
	}
	if a.Source == "url" {
		return a, nil
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return a, fmt.Errorf("open attachment: %w", err)
	}
	defer f.Close()
	id, err := r.client.UploadFile(ctx, "user_data", f)
	if err != nil {
		return a, err
	}
	a.FileID = id
	a.Source = "file"
	return a, nil
}

// resolveEffort maps the requested effort onto what the model accepts.
// It first consults the documented base table; if the model is not
// listed there, it falls back to the live ArkModels metadata. It returns
// "" when no reasoning_effort parameter should be sent.
func (r *Runner) resolveEffort(ctx context.Context, modelID string) string {
	requested := r.reasoningEffort
	if requested == "" {
		requested = "max"
	}
	if cfg := lookupEffortBaseConfig(modelID); cfg != nil {
		return cfg.mapEffort(requested)
	}
	models, err := r.client.ListModels(ctx)
	if err != nil {
		return ""
	}
	for _, m := range models {
		if m.ID == modelID || m.RuntimeID == modelID || m.SelectID() == modelID {
			if len(m.Efforts) == 0 {
				return ""
			}
			return ResolveEffort(requested, m.Efforts)
		}
	}
	return ""
}
