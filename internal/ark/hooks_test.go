package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/hooks"
	"github.com/chyroc/nomad/internal/loop"
)

// hookFakeServer streams one write tool_use and records the raw bodies
// posted to /events so tool-result delivery and user.message text can be
// asserted.
func hookFakeServer(t *testing.T) (string, func() string) {
	var mu sync.Mutex
	var bodies []string
	gotResult := make(chan struct{})
	var once sync.Once

	sessionJSON, _ := json.Marshal(map[string]any{
		"id": "sesn_h", "type": "session", "status": "idle",
		"environment_id": "env_1",
		"agent":          map[string]any{"type": "agent", "id": "agent_1"},
		"created_at":     "2026-09-23T00:00:00Z",
		"updated_at":     "2026-09-23T00:00:00Z",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			w.Write(sessionJSON)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			mu.Lock()
			bodies = append(bodies, body)
			if strings.Contains(body, "user.tool_result") {
				once.Do(func() { close(gotResult) })
			}
			mu.Unlock()
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			fmt.Fprint(w, `{"events":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			fmt.Fprint(w, `{"data":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events/stream"):
			fl := w.(http.Flusher)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			send := func(v any) {
				b, _ := json.Marshal(v)
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
			send(map[string]any{"id": "t1", "type": "agent.tool_use",
				"tool_use_id": "tu1", "name": "write",
				"input": map[string]any{"file_path": "hook-fake.txt", "content": "x"}})
			select {
			case <-gotResult:
			case <-time.After(5 * time.Second):
				return
			}
			send(map[string]any{"id": "i1", "type": "session.status_idle",
				"stop_reason": map[string]any{"type": "end_turn"}})
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(bodies, "\n")
	}
}

func newHookRunner(t *testing.T, url, ws string, pre func(context.Context, string, json.RawMessage) hooks.Outcome, post func(context.Context, string, json.RawMessage, bool) hooks.Outcome) *Runner {
	r, err := NewRunner(context.Background(), RunnerOptions{
		Client:     NewClient(url, "k", "", WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false })),
		Profile:    Profile{AgentID: "agent_1", EnvironmentID: "env_1", Model: "model-x"},
		Workspace:  ws,
		Permission: PermBypass,
		PreToolUse: pre,
		PostToolUse: func(ctx context.Context, name string, input json.RawMessage, isErr bool) hooks.Outcome {
			if post == nil {
				return hooks.Outcome{}
			}
			return post(ctx, name, input, isErr)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHook_PreToolUseBlock(t *testing.T) {
	ws := t.TempDir()
	url, bodies := hookFakeServer(t)

	var sawBlocked bool
	runner := newHookRunner(t, url, ws,
		func(ctx context.Context, name string, input json.RawMessage) hooks.Outcome {
			if name == "write" {
				return hooks.Outcome{Blocked: true, Reason: "writes forbidden by test hook"}
			}
			return hooks.Outcome{}
		}, nil)
	defer runner.Close()
	runner.Subscribe(loop.ObserverFunc(func(ev loop.Event) {
		if ev.Kind == loop.EvToolResult && ev.IsError && strings.Contains(ev.Result, "writes forbidden by test hook") {
			sawBlocked = true
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.Run(ctx, "try a write", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sawBlocked {
		t.Fatal("pre-tool block did not surface as an errored tool result")
	}
	if _, err := os.ReadFile(ws + "/hook-fake.txt"); err == nil {
		t.Fatal("blocked tool must not create the file")
	}
	if !strings.Contains(bodies(), "user.tool_result") {
		t.Fatal("blocked tool result was not posted to the server")
	}
}

func TestHook_PostContextInjectedNextTurn(t *testing.T) {
	ws := t.TempDir()
	url, bodies := hookFakeServer(t)

	runner := newHookRunner(t, url, ws, nil,
		func(ctx context.Context, name string, input json.RawMessage, isErr bool) hooks.Outcome {
			return hooks.Outcome{AdditionalContext: "lint passed for " + name}
		})
	defer runner.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.Run(ctx, "first write", nil); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := runner.Run(ctx, "second turn", nil); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	all := bodies()
	if !strings.Contains(all, "Hook-provided context") || !strings.Contains(all, "lint passed for write") {
		t.Fatalf("post-hook context not injected into a later user message:\n%s", all)
	}
	if strings.Index(all, "first write") > strings.Index(all, "lint passed for write") {
		t.Fatal("hook context should appear after the first turn, in a later message")
	}
}
