package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/loop"
)

type fakeMA struct {
	mu      sync.Mutex
	history []string
	subs    map[chan string]bool
	results chan string
	n       int
}

func newFakeMA(t *testing.T) string {
	f := &fakeMA{subs: map[chan string]bool{}, results: make(chan string, 8)}
	sessionJSON, _ := json.Marshal(map[string]any{
		"id": "sesn_e2e", "type": "session", "status": "idle",
		"environment_id": "env_1",
		"agent":          map[string]any{"type": "agent", "id": "agent_1"},
		"created_at":     "2026-10-13T00:00:00Z",
		"updated_at":     "2026-10-13T00:00:00Z",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(sessionJSON)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			switch {
			case strings.Contains(body, "user.message"):
				go f.play(body)
			case strings.Contains(body, "user.tool_result"):
				f.results <- body
			}
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			fmt.Fprint(w, `{"events":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events/stream"):
			f.stream(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func (f *fakeMA) broadcast(v map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	v["id"] = fmt.Sprintf("ev%d", f.n)
	b, _ := json.Marshal(v)
	line := string(b)
	f.history = append(f.history, line)
	for ch := range f.subs {
		ch <- line
	}
}

func (f *fakeMA) play(msg string) {
	f.mu.Lock()
	f.history = nil
	f.mu.Unlock()
	message := func(text string) map[string]any {
		return map[string]any{"type": "agent.message", "content": []map[string]any{{"type": "text", "text": text}}}
	}
	idle := func(reason string) map[string]any {
		return map[string]any{"type": "session.status_idle", "stop_reason": map[string]any{"type": reason}}
	}
	if strings.Contains(msg, "write two files") {
		for i, name := range []string{"a.txt", "b.txt"} {
			f.broadcast(map[string]any{"type": "agent.tool_use", "tool_use_id": fmt.Sprintf("tu%d", i),
				"name": "write", "input": map[string]any{"file_path": name, "content": "hello " + name}})
		}
		f.broadcast(idle("requires_action"))
		for range 2 {
			select {
			case <-f.results:
			case <-time.After(10 * time.Second):
				return
			}
		}
		f.broadcast(map[string]any{"type": "session.status_running"})
		f.broadcast(message("wrote both files"))
	} else {
		f.broadcast(message("hello from the fake agent"))
	}
	f.broadcast(idle("end_turn"))
}

func (f *fakeMA) stream(w http.ResponseWriter, r *http.Request) {
	fl := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	ch := make(chan string, 64)
	f.mu.Lock()
	backlog := append([]string(nil), f.history...)
	f.subs[ch] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.subs, ch)
		f.mu.Unlock()
	}()
	for _, line := range backlog {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	fl.Flush()
	for {
		select {
		case line := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", line)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

type fakeArkBackend struct{ url string }

func (b *fakeArkBackend) Status() Status { return Status{LoggedIn: true, Model: "model-x"} }
func (b *fakeArkBackend) Models(context.Context) ([]Model, error) {
	return nil, nil
}
func (b *fakeArkBackend) SetDefaults(string, string) error { return nil }
func (b *fakeArkBackend) Login(context.Context, func(string), func() (string, error)) error {
	return nil
}
func (b *fakeArkBackend) Logout() error { return nil }

func (b *fakeArkBackend) NewRunner(ctx context.Context, spec RunnerSpec) (loop.Runner, error) {
	client := ark.NewClient(b.url, "k", "", ark.WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false }))
	return ark.NewRunner(ctx, ark.RunnerOptions{
		Client:     client,
		Profile:    ark.Profile{AgentID: "agent_1", EnvironmentID: "env_1", Model: "model-x"},
		SessionID:  spec.SessionID,
		Workspace:  spec.Workspace,
		Model:      spec.Model,
		Permission: permissionMode(spec.PermissionMode),
		Ask:        spec.Ask,
	})
}

func eventKinds(seen []msg) []string {
	var kinds []string
	for _, m := range seen {
		if m.Method != "event" {
			continue
		}
		var p struct {
			Event loop.Event `json:"event"`
		}
		_ = json.Unmarshal(m.Params, &p)
		kinds = append(kinds, p.Event.Kind)
	}
	return kinds
}

func TestE2ESimpleChatTurn(t *testing.T) {
	ws := t.TempDir()
	c, st := startServer(t, &fakeArkBackend{url: newFakeMA(t)})
	c.call("turn/start", map[string]any{"workspace": ws, "text": "say hi", "permissionMode": "default"})
	seen := c.until(isMethod("turn/completed"))
	last := seen[len(seen)-1]
	if strings.Contains(string(last.Params), "error") {
		t.Fatalf("turn failed: %s", last.Params)
	}
	kinds := strings.Join(eventKinds(seen), ",")
	if !strings.Contains(kinds, "user_message") || !strings.Contains(kinds, "assistant_message") || !strings.HasSuffix(kinds, "turn_end") {
		t.Fatalf("unexpected event kinds: %s", kinds)
	}
	meta, err := st.LoadMeta("sesn_e2e")
	if err != nil || meta.Workspace != ws {
		t.Fatalf("meta = %+v, %v", meta, err)
	}
}

func TestE2EParallelWritesWithApprovals(t *testing.T) {
	ws := t.TempDir()
	c, _ := startServer(t, &fakeArkBackend{url: newFakeMA(t)})
	c.call("turn/start", map[string]any{"workspace": ws, "text": "write two files", "permissionMode": "default"})
	approvals := 0
	var seen []msg
	for {
		m := c.next()
		seen = append(seen, m)
		if m.Method == "approval/request" {
			approvals++
			c.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m.ID), "result": map[string]string{"decision": "allow"}})
			continue
		}
		if m.Method == "turn/completed" {
			if strings.Contains(string(m.Params), "error") {
				t.Fatalf("turn failed: %s", m.Params)
			}
			break
		}
	}
	if approvals != 2 {
		t.Fatalf("approvals = %d, want 2", approvals)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		data, err := os.ReadFile(filepath.Join(ws, name))
		if err != nil || string(data) != "hello "+name {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
	kinds := eventKinds(seen)
	results := 0
	for _, k := range kinds {
		if k == loop.EvToolResult {
			results++
		}
	}
	if results != 2 || kinds[len(kinds)-1] != loop.EvTurnEnd {
		t.Fatalf("event kinds = %v", kinds)
	}
}
