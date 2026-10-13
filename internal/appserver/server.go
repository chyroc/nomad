// Package appserver exposes nomad sessions to GUI clients (the macOS
// app) as newline-delimited JSON-RPC 2.0 over stdio.
//
// Client requests: initialize, models/list, defaults/set, thread/list,
// thread/read, workspace/info, turn/start, turn/interrupt, auth/login,
// auth/submitCode, auth/logout. Server notifications: thread/started,
// event, turn/completed, auth/url, auth/completed. Server requests:
// approval/request, answered with {"decision":"allow|session|deny"}.
package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

const (
	codeParse    = -32700
	codeMethod   = -32601
	codeParams   = -32602
	codeInternal = -32000
)

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type inbound struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type paramsError struct{ err error }

func (e paramsError) Error() string { return e.err.Error() }

// Server multiplexes threads over one stdio connection.
type Server struct {
	backend Backend
	store   *store.SessionStore
	version string

	// ChatDir is the workspace of threads started without a project;
	// when empty, turn/start requires a workspace.
	ChatDir string

	wmu sync.Mutex
	out io.Writer

	mu      sync.Mutex
	threads map[string]*thread
	pending map[string]chan inbound
	nextReq int
	login   chan string
}

type thread struct {
	id        string
	workspace string
	model     string
	effort    string
	perm      string
	runner    loop.Runner
	running   bool
	turnCtx   context.Context
	cancel    context.CancelFunc
}

// New constructs a server backed by b, persisting transcripts in st.
func New(b Backend, st *store.SessionStore, version string, out io.Writer) *Server {
	return &Server{
		backend: b,
		store:   st,
		version: version,
		out:     out,
		threads: map[string]*thread{},
		pending: map[string]chan inbound{},
	}
}

// Serve reads requests from in until EOF or ctx is done.
func (s *Server) Serve(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer s.shutdown()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	var wg sync.WaitGroup
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg inbound
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			s.send(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcError{Code: codeParse, Message: err.Error()}})
			continue
		}
		if msg.Method == "" {
			s.deliver(msg)
			continue
		}
		if len(msg.ID) == 0 {
			continue
		}
		wg.Add(1)
		go func(msg inbound) {
			defer wg.Done()
			s.reply(msg.ID, s.handle(ctx, msg.Method, msg.Params))
		}(msg)
	}
	wg.Wait()
	return sc.Err()
}

type outcome struct {
	result any
	err    error
}

func (s *Server) handle(ctx context.Context, method string, params json.RawMessage) outcome {
	var (
		res any
		err error
	)
	switch method {
	case "initialize":
		res = s.initialize()
	case "models/list":
		res, err = s.modelsList(ctx)
	case "defaults/set":
		res, err = s.defaultsSet(params)
	case "thread/list":
		res, err = s.threadList()
	case "thread/read":
		res, err = s.threadRead(params)
	case "workspace/info":
		res, err = s.workspaceInfo(ctx, params)
	case "turn/start":
		res, err = s.turnStart(ctx, params)
	case "turn/interrupt":
		res, err = s.turnInterrupt(params)
	case "auth/login":
		res, err = s.authLogin()
	case "auth/submitCode":
		res, err = s.authSubmitCode(params)
	case "auth/logout":
		res, err = s.authLogout()
	default:
		return outcome{err: &rpcError{Code: codeMethod, Message: "unknown method " + method}}
	}
	return outcome{result: res, err: err}
}

func (s *Server) reply(id json.RawMessage, o outcome) {
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if o.err != nil {
		var re *rpcError
		var pe paramsError
		switch {
		case errors.As(o.err, &re):
			msg["error"] = re
		case errors.As(o.err, &pe):
			msg["error"] = rpcError{Code: codeParams, Message: pe.Error()}
		default:
			msg["error"] = rpcError{Code: codeInternal, Message: o.err.Error()}
		}
	} else {
		if o.result == nil {
			o.result = struct{}{}
		}
		msg["result"] = o.result
	}
	s.send(msg)
}

func (e *rpcError) Error() string { return e.Message }

func (s *Server) notify(method string, params any) {
	s.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *Server) send(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, _ = s.out.Write(append(data, '\n'))
}

func (s *Server) request(ctx context.Context, method string, params any) (inbound, error) {
	s.mu.Lock()
	s.nextReq++
	id := fmt.Sprintf("s%d", s.nextReq)
	ch := make(chan inbound, 1)
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()
	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	select {
	case msg := <-ch:
		return msg, nil
	case <-ctx.Done():
		return inbound{}, ctx.Err()
	}
}

func (s *Server) deliver(msg inbound) {
	var id string
	if err := json.Unmarshal(msg.ID, &id); err != nil {
		return
	}
	s.mu.Lock()
	ch := s.pending[id]
	s.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func decode(params json.RawMessage, v any) error {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, v); err != nil {
		return paramsError{err}
	}
	return nil
}

func (s *Server) initialize() any {
	st := s.backend.Status()
	return map[string]any{
		"version":       s.version,
		"chatWorkspace": s.ChatDir,
		"loggedIn":      st.LoggedIn,
		"model":         st.Model,
		"effort":        st.Effort,
		"efforts":       ark.EffortLadder,
	}
}

func (s *Server) modelsList(ctx context.Context) (any, error) {
	models, err := s.backend.Models(ctx)
	if err != nil {
		return nil, err
	}
	st := s.backend.Status()
	return map[string]any{"models": models, "model": st.Model, "effort": st.Effort}, nil
}

func (s *Server) defaultsSet(params json.RawMessage) (any, error) {
	var p struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	return nil, s.backend.SetDefaults(p.Model, p.Effort)
}

// ThreadSummary is one sidebar entry.
type ThreadSummary struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Workspace string    `json:"workspace"`
	UpdatedAt time.Time `json:"updatedAt"`
	Running   bool      `json:"running"`
}

func (s *Server) threadList() (any, error) {
	recs, err := s.store.List()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ThreadSummary, 0, len(recs))
	for _, r := range recs {
		sum := ThreadSummary{ID: r.ID, Title: r.Title, Workspace: r.Workspace, UpdatedAt: r.UpdatedAt}
		if th := s.threads[r.ID]; th != nil {
			sum.Running = th.running
		}
		out = append(out, sum)
	}
	return map[string]any{"threads": out}, nil
}

func (s *Server) threadRead(params json.RawMessage) (any, error) {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	events, err := s.store.Load(p.ThreadID)
	if err != nil {
		return nil, err
	}
	meta, _ := s.store.LoadMeta(p.ThreadID)
	s.mu.Lock()
	running := false
	if th := s.threads[p.ThreadID]; th != nil {
		running = th.running
	}
	s.mu.Unlock()
	if events == nil {
		events = []loop.Event{}
	}
	return map[string]any{
		"threadId":  p.ThreadID,
		"workspace": meta.Workspace,
		"model":     meta.Model,
		"running":   running,
		"events":    events,
	}, nil
}

func (s *Server) workspaceInfo(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if p.Path == "" {
		return nil, paramsError{errors.New("path is required")}
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	branch := ""
	if out, err := exec.CommandContext(cctx, "git", "-C", p.Path, "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	return map[string]any{"path": p.Path, "name": filepath.Base(p.Path), "branch": branch}, nil
}

type turnStartParams struct {
	ThreadID       string   `json:"threadId"`
	Workspace      string   `json:"workspace"`
	Text           string   `json:"text"`
	Model          string   `json:"model"`
	Effort         string   `json:"effort"`
	PermissionMode string   `json:"permissionMode"`
	Images         []string `json:"images"`
}

type uploader interface {
	UploadAttachment(ctx context.Context, a loop.Attachment) (loop.Attachment, error)
}

func (s *Server) turnStart(ctx context.Context, params json.RawMessage) (any, error) {
	var p turnStartParams
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Text) == "" {
		return nil, paramsError{errors.New("text is required")}
	}
	th, err := s.claimThread(p)
	if err != nil {
		return nil, err
	}
	if err := s.prepareRunner(ctx, th, p); err != nil {
		s.mu.Lock()
		th.running = false
		s.mu.Unlock()
		return nil, err
	}
	var atts []loop.Attachment
	if up, ok := th.runner.(uploader); ok {
		for _, img := range p.Images {
			att, err := up.UploadAttachment(ctx, loop.Attachment{Kind: "image", Source: "file", Path: img})
			if err != nil {
				s.mu.Lock()
				th.running = false
				s.mu.Unlock()
				return nil, fmt.Errorf("upload image %s: %w", img, err)
			}
			atts = append(atts, att)
		}
	}
	turnCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	th.turnCtx, th.cancel = turnCtx, cancel
	runner := th.runner
	s.mu.Unlock()
	go s.runTurn(turnCtx, th, runner, p.Text, atts)
	return map[string]any{"threadId": th.id}, nil
}

func (s *Server) claimThread(p turnStartParams) (*thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var th *thread
	if p.ThreadID != "" {
		th = s.threads[p.ThreadID]
		if th == nil {
			th = &thread{id: p.ThreadID, workspace: p.Workspace}
			if meta, err := s.store.LoadMeta(p.ThreadID); err == nil && meta.Workspace != "" {
				th.workspace = meta.Workspace
			}
			s.threads[p.ThreadID] = th
		}
	} else {
		th = &thread{workspace: p.Workspace}
	}
	if th.running {
		return nil, errors.New("a turn is already running in this thread")
	}
	if th.workspace == "" && s.ChatDir != "" {
		if err := os.MkdirAll(s.ChatDir, 0o700); err != nil {
			return nil, err
		}
		th.workspace = s.ChatDir
	}
	if th.workspace == "" {
		return nil, paramsError{errors.New("workspace is required")}
	}
	th.running = true
	return th, nil
}

func (s *Server) prepareRunner(ctx context.Context, th *thread, p turnStartParams) error {
	s.mu.Lock()
	reuse := th.runner != nil && th.perm == p.PermissionMode
	old := th.runner
	if !reuse {
		th.runner = nil
	}
	s.mu.Unlock()
	if reuse {
		return nil
	}
	if old != nil {
		_ = old.Close()
	}
	runner, err := s.backend.NewRunner(ctx, RunnerSpec{
		SessionID:      th.id,
		Workspace:      th.workspace,
		Model:          p.Model,
		Effort:         p.Effort,
		PermissionMode: p.PermissionMode,
		Ask:            s.askFunc(th),
	})
	if err != nil {
		return err
	}
	isNew := th.id == ""
	s.mu.Lock()
	th.runner = runner
	th.perm = p.PermissionMode
	th.model, th.effort = p.Model, p.Effort
	if isNew {
		th.id = runner.SessionID()
		s.threads[th.id] = th
	}
	id := th.id
	s.mu.Unlock()
	runner.Subscribe(loop.ObserverFunc(func(ev loop.Event) {
		if ev.Time.IsZero() {
			ev.Time = time.Now()
		}
		_ = s.store.Append(id, ev)
		s.notify("event", map[string]any{"threadId": id, "event": ev})
	}))
	if isNew {
		_ = s.store.SaveMeta(id, store.Meta{Workspace: th.workspace, Model: p.Model})
		s.notify("thread/started", ThreadSummary{
			ID: id, Title: firstLine(p.Text), Workspace: th.workspace, UpdatedAt: time.Now(), Running: true,
		})
	}
	return nil
}

func (s *Server) runTurn(ctx context.Context, th *thread, runner loop.Runner, text string, atts []loop.Attachment) {
	err := runner.Run(ctx, text, atts)
	s.mu.Lock()
	th.running = false
	if th.cancel != nil {
		th.cancel()
	}
	th.cancel = nil
	id := th.id
	s.mu.Unlock()
	params := map[string]any{"threadId": id}
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled), errors.Is(err, ark.ErrInterrupted):
		params["interrupted"] = true
	default:
		params["error"] = err.Error()
	}
	s.notify("turn/completed", params)
}

func (s *Server) askFunc(th *thread) func(tool, arguments string) string {
	return func(tool, arguments string) string {
		s.mu.Lock()
		ctx := th.turnCtx
		id := th.id
		s.mu.Unlock()
		if ctx == nil {
			ctx = context.Background()
		}
		var args any = arguments
		if json.Valid([]byte(arguments)) {
			args = json.RawMessage(arguments)
		}
		msg, err := s.request(ctx, "approval/request", map[string]any{
			"threadId": id, "tool": tool, "arguments": args,
		})
		if err != nil || msg.Error != nil {
			return "deny"
		}
		var r struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal(msg.Result, &r)
		switch r.Decision {
		case "allow", "session":
			return r.Decision
		default:
			return "deny"
		}
	}
}

func (s *Server) turnInterrupt(params json.RawMessage) (any, error) {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	s.mu.Lock()
	th := s.threads[p.ThreadID]
	var (
		runner loop.Runner
		cancel context.CancelFunc
	)
	if th != nil && th.running {
		runner, cancel = th.runner, th.cancel
	}
	s.mu.Unlock()
	if runner == nil {
		return map[string]any{"interrupted": false}, nil
	}
	if in, ok := runner.(loop.Interruptor); ok {
		ictx, icancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = in.Interrupt(ictx)
		icancel()
	}
	if cancel != nil {
		cancel()
	}
	return map[string]any{"interrupted": true}, nil
}

func (s *Server) authLogin() (any, error) {
	s.mu.Lock()
	if s.login != nil {
		s.mu.Unlock()
		return nil, errors.New("a login is already in progress")
	}
	codes := make(chan string, 1)
	s.login = codes
	s.mu.Unlock()
	go func() {
		lctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		err := s.backend.Login(lctx,
			func(url string) { s.notify("auth/url", map[string]string{"url": url}) },
			func() (string, error) {
				select {
				case c := <-codes:
					return c, nil
				case <-lctx.Done():
					return "", lctx.Err()
				}
			})
		s.mu.Lock()
		s.login = nil
		s.mu.Unlock()
		params := map[string]any{"ok": err == nil}
		if err != nil {
			params["error"] = err.Error()
		}
		st := s.backend.Status()
		params["model"] = st.Model
		params["effort"] = st.Effort
		s.notify("auth/completed", params)
	}()
	return nil, nil
}

func (s *Server) authSubmitCode(params json.RawMessage) (any, error) {
	var p struct {
		Code string `json:"code"`
	}
	if err := decode(params, &p); err != nil {
		return nil, err
	}
	s.mu.Lock()
	codes := s.login
	s.mu.Unlock()
	if codes == nil {
		return nil, errors.New("no login in progress")
	}
	select {
	case codes <- strings.TrimSpace(p.Code):
	default:
		return nil, errors.New("a code was already submitted")
	}
	return nil, nil
}

func (s *Server) authLogout() (any, error) {
	return nil, s.backend.Logout()
}

func (s *Server) shutdown() {
	s.mu.Lock()
	threads := make([]*thread, 0, len(s.threads))
	for _, th := range s.threads {
		threads = append(threads, th)
	}
	s.mu.Unlock()
	for _, th := range threads {
		s.mu.Lock()
		cancel, runner := th.cancel, th.runner
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if runner != nil {
			_ = runner.Close()
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}
