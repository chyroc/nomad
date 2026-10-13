package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

type fakeRunner struct {
	id     string
	ask    func(string, string) string
	obs    []loop.Observer
	closed bool
}

func (r *fakeRunner) Subscribe(o loop.Observer) { r.obs = append(r.obs, o) }
func (r *fakeRunner) SessionID() string         { return r.id }
func (r *fakeRunner) Close() error              { r.closed = true; return nil }

func (r *fakeRunner) emit(ev loop.Event) {
	for _, o := range r.obs {
		o.OnEvent(ev)
	}
}

func (r *fakeRunner) Run(ctx context.Context, text string, _ []loop.Attachment) error {
	r.emit(loop.Event{Kind: loop.EvUserMessage, Content: text})
	if text == "block" {
		<-ctx.Done()
		return ctx.Err()
	}
	if text == "write" {
		args := `{"path":"a.txt"}`
		r.emit(loop.Event{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{ID: "t1", Name: "write", Arguments: args}})
		decision := r.ask("write", args)
		r.emit(loop.Event{Kind: loop.EvToolResult, ToolName: "write", Result: decision, ToolCall: &loop.ToolCall{ID: "t1"}})
	}
	r.emit(loop.Event{Kind: loop.EvAssistantMessage, Content: "done: " + text})
	r.emit(loop.Event{Kind: loop.EvTurnEnd, Usage: &loop.Usage{InputTokens: 3, OutputTokens: 2}})
	return nil
}

type fakeBackend struct {
	mu      sync.Mutex
	n       int
	specs   []RunnerSpec
	runners []*fakeRunner
}

func (b *fakeBackend) Status() Status { return Status{LoggedIn: true, Model: "m1", Effort: "high"} }
func (b *fakeBackend) Models(context.Context) ([]Model, error) {
	return []Model{{ID: "m1", Name: "Model One"}}, nil
}
func (b *fakeBackend) SetDefaults(string, string) error { return nil }
func (b *fakeBackend) Login(context.Context, func(string), func() (string, error)) error {
	return nil
}
func (b *fakeBackend) Logout() error { return nil }

func (b *fakeBackend) NewRunner(_ context.Context, spec RunnerSpec) (loop.Runner, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.n++
	id := spec.SessionID
	if id == "" {
		id = fmt.Sprintf("sess-%d", b.n)
	}
	r := &fakeRunner{id: id, ask: spec.Ask}
	b.specs = append(b.specs, spec)
	b.runners = append(b.runners, r)
	return r, nil
}

type client struct {
	t   *testing.T
	w   io.Writer
	r   *bufio.Scanner
	seq int
}

func startServer(t *testing.T, b Backend) (*client, *store.SessionStore) {
	t.Helper()
	st, err := store.NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := New(b, st, "test", outW)
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(context.Background(), inR)
		close(done)
	}()
	t.Cleanup(func() {
		_ = inW.Close()
		go func() {
			for {
				buf := make([]byte, 4096)
				if _, err := outR.Read(buf); err != nil {
					return
				}
			}
		}()
		<-done
		_ = outW.Close()
	})
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return &client{t: t, w: inW, r: sc}, st
}

func (c *client) send(v any) {
	c.t.Helper()
	data, _ := json.Marshal(v)
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) call(method string, params any) string {
	c.seq++
	id := fmt.Sprintf("c%d", c.seq)
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return id
}

type msg struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *client) next() msg {
	c.t.Helper()
	got := make(chan msg, 1)
	go func() {
		if !c.r.Scan() {
			close(got)
			return
		}
		var m msg
		_ = json.Unmarshal(c.r.Bytes(), &m)
		got <- m
	}()
	select {
	case m, ok := <-got:
		if !ok {
			c.t.Fatal("server closed output")
		}
		return m
	case <-time.After(5 * time.Second):
		c.t.Fatal("timed out waiting for server message")
	}
	return msg{}
}

func (c *client) until(pred func(msg) bool) []msg {
	c.t.Helper()
	var seen []msg
	for {
		m := c.next()
		seen = append(seen, m)
		if pred(m) {
			return seen
		}
	}
}

func isResponse(id string) func(msg) bool {
	return func(m msg) bool { return m.Method == "" && string(m.ID) == `"`+id+`"` }
}

func isMethod(method string) func(msg) bool {
	return func(m msg) bool { return m.Method == method }
}

func TestInitializeAndModels(t *testing.T) {
	c, _ := startServer(t, &fakeBackend{})
	id := c.call("initialize", nil)
	seen := c.until(isResponse(id))
	var res struct {
		LoggedIn bool     `json:"loggedIn"`
		Model    string   `json:"model"`
		Efforts  []string `json:"efforts"`
	}
	_ = json.Unmarshal(seen[len(seen)-1].Result, &res)
	if !res.LoggedIn || res.Model != "m1" || len(res.Efforts) == 0 {
		t.Fatalf("initialize = %+v", res)
	}
	id = c.call("models/list", nil)
	seen = c.until(isResponse(id))
	if string(seen[len(seen)-1].Result) == "" {
		t.Fatal("models/list returned no result")
	}
	id = c.call("nope", nil)
	seen = c.until(isResponse(id))
	if e := seen[len(seen)-1].Error; e == nil || e.Code != codeMethod {
		t.Fatalf("unknown method error = %+v", e)
	}
}

func TestSimpleTurnPersistsAndCompletes(t *testing.T) {
	b := &fakeBackend{}
	c, st := startServer(t, b)
	id := c.call("turn/start", map[string]any{"workspace": "/ws/proj", "text": "hello", "permissionMode": "default"})
	seen := c.until(isMethod("turn/completed"))

	var threadID string
	var kinds []string
	startedSeen := false
	for _, m := range seen {
		switch {
		case isResponse(id)(m):
			var r struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(m.Result, &r)
			threadID = r.ThreadID
		case m.Method == "thread/started":
			startedSeen = true
		case m.Method == "event":
			var p struct {
				Event loop.Event `json:"event"`
			}
			_ = json.Unmarshal(m.Params, &p)
			kinds = append(kinds, p.Event.Kind)
		}
	}
	if threadID != "sess-1" || !startedSeen {
		t.Fatalf("threadID=%q started=%v", threadID, startedSeen)
	}
	want := []string{loop.EvUserMessage, loop.EvAssistantMessage, loop.EvTurnEnd}
	if fmt.Sprint(kinds) != fmt.Sprint(want) {
		t.Fatalf("event kinds = %v, want %v", kinds, want)
	}
	meta, err := st.LoadMeta("sess-1")
	if err != nil || meta.Workspace != "/ws/proj" {
		t.Fatalf("meta = %+v, %v", meta, err)
	}

	lid := c.call("thread/list", nil)
	seen = c.until(isResponse(lid))
	var list struct {
		Threads []ThreadSummary `json:"threads"`
	}
	_ = json.Unmarshal(seen[len(seen)-1].Result, &list)
	if len(list.Threads) != 1 || list.Threads[0].Workspace != "/ws/proj" || list.Threads[0].Title != "hello" {
		t.Fatalf("threads = %+v", list.Threads)
	}

	rid := c.call("thread/read", map[string]any{"threadId": "sess-1"})
	seen = c.until(isResponse(rid))
	var read struct {
		Events []loop.Event `json:"events"`
	}
	_ = json.Unmarshal(seen[len(seen)-1].Result, &read)
	if len(read.Events) != 3 {
		t.Fatalf("persisted events = %d", len(read.Events))
	}

	c.call("turn/start", map[string]any{"threadId": "sess-1", "text": "again", "permissionMode": "default"})
	c.until(isMethod("turn/completed"))
	if len(b.runners) != 1 {
		t.Fatalf("runner not reused: %d runners", len(b.runners))
	}
	c.call("turn/start", map[string]any{"threadId": "sess-1", "text": "bypass", "permissionMode": "bypassPermissions"})
	c.until(isMethod("turn/completed"))
	if len(b.runners) != 2 || !b.runners[0].closed || b.specs[1].SessionID != "sess-1" {
		t.Fatalf("permission change should reattach the session: %+v", b.specs)
	}
}

func TestApprovalRoundTrip(t *testing.T) {
	c, _ := startServer(t, &fakeBackend{})
	c.call("turn/start", map[string]any{"workspace": "/ws", "text": "write", "permissionMode": "default"})
	seen := c.until(isMethod("approval/request"))
	req := seen[len(seen)-1]
	var p struct {
		ThreadID  string          `json:"threadId"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	_ = json.Unmarshal(req.Params, &p)
	if p.Tool != "write" || p.ThreadID != "sess-1" || string(p.Arguments) != `{"path":"a.txt"}` {
		t.Fatalf("approval params = %+v", p)
	}
	c.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": map[string]string{"decision": "session"}})
	seen = c.until(isMethod("turn/completed"))
	found := false
	for _, m := range seen {
		if m.Method != "event" {
			continue
		}
		var ep struct {
			Event loop.Event `json:"event"`
		}
		_ = json.Unmarshal(m.Params, &ep)
		if ep.Event.Kind == loop.EvToolResult && ep.Event.Result == "session" {
			found = true
		}
	}
	if !found {
		t.Fatal("tool did not observe the approval decision")
	}
}

func TestInterruptCancelsTurn(t *testing.T) {
	c, _ := startServer(t, &fakeBackend{})
	id := c.call("turn/start", map[string]any{"workspace": "/ws", "text": "block"})
	c.until(isResponse(id))
	busy := c.call("turn/start", map[string]any{"threadId": "sess-1", "text": "second"})
	seen := c.until(isResponse(busy))
	if seen[len(seen)-1].Error == nil {
		t.Fatal("expected busy error for concurrent turn")
	}
	c.call("turn/interrupt", map[string]any{"threadId": "sess-1"})
	seen = c.until(isMethod("turn/completed"))
	var done struct {
		Interrupted bool `json:"interrupted"`
	}
	_ = json.Unmarshal(seen[len(seen)-1].Params, &done)
	if !done.Interrupted {
		t.Fatalf("turn/completed = %s", seen[len(seen)-1].Params)
	}
}
