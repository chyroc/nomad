package ark

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/volcengine/ark-runtime-go/arkruntime/toolset"
)

type fakeTool struct {
	calls int
}

func (f *fakeTool) Name() string { return "write" }
func (f *fakeTool) Execute(_ context.Context, _ json.RawMessage) toolset.Result {
	f.calls++
	return toolset.TextResult("ok")
}

func TestGatedTool_MaxTurns(t *testing.T) {
	allowAll := func(string, json.RawMessage) (bool, string) { return true, "" }
	ft := &fakeTool{}
	counter := &turnCounter{}
	g := &gatedTool{inner: ft, decide: allowAll, counter: counter, max: 2}

	r1 := g.Execute(context.Background(), json.RawMessage(`{}`))
	r2 := g.Execute(context.Background(), json.RawMessage(`{}`))
	r3 := g.Execute(context.Background(), json.RawMessage(`{}`))

	if r1.IsError || r2.IsError {
		t.Fatalf("first two calls must succeed: %+v %+v", r1, r2)
	}
	if !r3.IsError {
		t.Fatalf("third call must be rejected by max-turns=2")
	}
	if ft.calls != 2 {
		t.Fatalf("inner tool ran %d times, want 2", ft.calls)
	}
}

func TestGatedTool_Deny(t *testing.T) {
	deny := func(string, json.RawMessage) (bool, string) { return false, "no" }
	ft := &fakeTool{}
	g := &gatedTool{inner: ft, decide: deny, counter: &turnCounter{}}
	r := g.Execute(context.Background(), nil)
	if !r.IsError || ft.calls != 0 {
		t.Fatalf("denied tool must not execute: %+v calls=%d", r, ft.calls)
	}
}

func TestDecidePermissionSessionAllow(t *testing.T) {
	ask := func(string, string) string { return "session" }
	allowed, denied := decidePermission(PermDefault, nil, nil, nil, ask, "write", json.RawMessage(`{}`))
	if !allowed || denied != "" {
		t.Fatalf("first session answer should allow: %v %q", allowed, denied)
	}
	sessionMap := map[string]bool{"write": true}
	allowed, denied = decidePermission(PermDefault, nil, nil, sessionMap, ask, "write", json.RawMessage(`{}`))
	if !allowed || denied != "" {
		t.Fatalf("session-remembered tool should allow without asking")
	}
	allowed, _ = decidePermission(PermDefault, nil, nil, sessionMap, nil, "bash", json.RawMessage(`{}`))
	if allowed {
		t.Fatalf("other tools should not be implicitly allowed")
	}
}
