package ark_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// fakeMA is a two-phase managed-agents control plane that:
// creates session, accepts user.message, streams agent.tool_use then
// (after user.tool_result) the final agent.message + end_turn idle,
// and lists models/files.
func fakeMA(t *testing.T, workspace string) (*httptest.Server, *sync.Mutex, *[]string, chan struct{}) {
	var mu sync.Mutex
	var posted []string
	resultPosted := make(chan struct{})
	var once sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
			  "id":"sesn_fake_1","type":"session","status":"idle",
			  "environment_id":"env_1","agent":{"type":"agent","id":"agent_1"},
			  "created_at":"2026-09-23T00:00:00Z","updated_at":"2026-09-23T00:00:00Z"}`)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			var body struct{ Events []struct{ Type string } }
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			for _, e := range body.Events {
				posted = append(posted, e.Type)
				if e.Type == "user.tool_result" {
					once.Do(func() { close(resultPosted) })
				}
			}
			mu.Unlock()
			fmt.Fprint(w, `{"data":[]}`)

		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events"):
			fmt.Fprint(w, `{"events":[]}`)

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
				"input": map[string]any{"file_path": "fake.txt", "content": "ma works"}})
			select {
			case <-resultPosted:
			case <-time.After(10 * time.Second):
				t.Errorf("timeout waiting tool result")
				return
			}
			send(map[string]any{"id": "m1", "type": "span.model_request_end",
				"model_usage": map[string]any{"input_tokens": 100, "output_tokens": 20}})
			send(map[string]any{"id": "a1", "type": "agent.message",
				"content": []map[string]any{{"type": "text", "text": "ALL DONE"}}})
			send(map[string]any{"id": "i1", "type": "session.status_idle",
				"stop_reason": map[string]any{"type": "end_turn"}})
			<-r.Context().Done()

		case r.Method == http.MethodGet && r.URL.Path == "/models":
			fmt.Fprint(w, `{"data":[{"id":"model-x","name":"Model X","status":"Available"}]}`)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &mu, &posted, resultPosted
}

func TestRunner_FullTurnWithLocalTool(t *testing.T) {
	ws := t.TempDir()
	srv, mu, posted, _ := fakeMA(t, ws)

	var kinds []string
	var text strings.Builder
	client := ark.NewClient(srv.URL, "k", "", ark.WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false }))
	runner, err := ark.NewRunner(context.Background(), ark.RunnerOptions{
		Client:     client,
		Profile:    ark.Profile{AgentID: "agent_1", EnvironmentID: "env_1", Model: "model-x"},
		Workspace:  ws,
		Permission: ark.PermBypass,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer runner.Close()
	runner.Subscribe(loop.ObserverFunc(func(ev loop.Event) {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == loop.EvAssistantChunk {
			text.WriteString(ev.Content)
		}
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runner.Run(ctx, "write the file", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if data, err := os.ReadFile(filepath.Join(ws, "fake.txt")); err != nil || string(data) != "ma works" {
		t.Fatalf("tool did not write correctly: %q %v", data, err)
	}
	if text.String() != "ALL DONE" {
		t.Fatalf("final answer = %q", text.String())
	}
	mu.Lock()
	joined := strings.Join(*posted, ",")
	mu.Unlock()
	if !strings.Contains(joined, "user.message") || !strings.Contains(joined, "user.tool_result") {
		t.Fatalf("posted events = %s", joined)
	}
	if !contains(kinds, "turn_end") {
		t.Fatalf("missing turn_end in %v", kinds)
	}
}

func TestClient_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[
		  {"id":"m1","name":"M1","status":"Available","task_type":["TextGeneration"],
		   "modalities":{"output_modalities":["text"]},
		   "features":{"tools":{"function_calling":true}},"token_limits":{"context_window":32768}},
		  {"id":"m2","name":"M2","status":"Shutdown","task_type":["TextGeneration"],"modalities":{"output_modalities":["text"]}},
		  {"id":"e1","name":"Emb","status":"Available","task_type":["Embedding"],"modalities":{"output_modalities":["text"]}}
		]}`)
	}))
	defer srv.Close()
	models, err := ark.NewClient(srv.URL, "k", "", ark.WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false })).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "m1" || !models[0].ToolCalling || models[0].ContextWindow != 32768 {
		t.Fatalf("models = %+v (shutdown/embedding must be filtered)", models)
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
