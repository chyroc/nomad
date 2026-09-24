package ark

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/chyroc/nomad/internal/settings"
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
	capped := &atomic.Bool{}
	g := &gatedTool{inner: ft, decide: allowAll, capped: capped}

	r1 := g.Execute(context.Background(), json.RawMessage(`{}`))
	capped.Store(true)
	r2 := g.Execute(context.Background(), json.RawMessage(`{}`))

	if r1.IsError {
		t.Fatalf("uncapped call must succeed: %+v", r1)
	}
	if !r2.IsError {
		t.Fatalf("capped call must be rejected")
	}
	if ft.calls != 1 {
		t.Fatalf("inner tool ran %d times, want 1", ft.calls)
	}
}

func TestGatedTool_Deny(t *testing.T) {
	deny := func(string, json.RawMessage) (bool, string) { return false, "no" }
	ft := &fakeTool{}
	g := &gatedTool{inner: ft, decide: deny, capped: &atomic.Bool{}}
	r := g.Execute(context.Background(), nil)
	if !r.IsError || ft.calls != 0 {
		t.Fatalf("denied tool must not execute: %+v calls=%d", r, ft.calls)
	}
}

func TestDecidePermissionSessionAllow(t *testing.T) {
	ask := func(string, string) string { return "session" }
	g := gateOptions{mode: PermDefault, ask: ask}
	allowed, denied := decidePermission(g, "write", json.RawMessage(`{}`))
	if !allowed || denied != "" {
		t.Fatalf("first session answer should allow: %v %q", allowed, denied)
	}
	sessionMap := map[string]bool{"write": true}
	g.sessionAllowed = sessionMap
	allowed, denied = decidePermission(g, "write", json.RawMessage(`{}`))
	if !allowed || denied != "" {
		t.Fatalf("session-remembered tool should allow without asking")
	}
	gNoAsk := gateOptions{mode: PermDefault, sessionAllowed: sessionMap}
	allowed, _ = decidePermission(gNoAsk, "bash", json.RawMessage(`{}`))
	if allowed {
		t.Fatalf("other tools should not be implicitly allowed")
	}
}

func TestDecidePermissionSettingsRules(t *testing.T) {
	rule := func(s string) settings.Rule {
		r, ok := settings.ParseRule(s)
		if !ok {
			t.Fatalf("bad rule %q", s)
		}
		return r
	}
	bashInput := json.RawMessage(`{"command":"rm -rf /tmp/x"}`)
	g := gateOptions{
		mode:       PermBypass,
		denyRules:  []settings.Rule{rule("bash(rm*)")},
		allowRules: []settings.Rule{rule("read")},
	}
	if allowed, reason := decidePermission(g, "bash", bashInput); allowed {
		t.Fatalf("deny rule must win even in bypass mode (got allow, reason %q)", reason)
	}
	if allowed, _ := decidePermission(g, "read", json.RawMessage(`{}`)); !allowed {
		t.Fatal("allow rule should permit read")
	}
}

func TestDecidePermissionFlagDisallowBeatsSettings(t *testing.T) {
	rule, _ := settings.ParseRule("write")
	g := gateOptions{
		mode:       PermBypass,
		disallowed: map[string]bool{"write": true},
		allowRules: []settings.Rule{rule},
	}
	if allowed, _ := decidePermission(g, "write", json.RawMessage(`{}`)); allowed {
		t.Fatal("flag-disallowed tool must never be rescued by settings")
	}
}

func TestDecidePermissionHeadlessDefaultDeny(t *testing.T) {
	g := gateOptions{mode: PermDefault}
	allowed, reason := decidePermission(g, "bash", json.RawMessage(`{"command":"ls"}`))
	if allowed || reason == "" {
		t.Fatalf("headless default mode must deny bash: %v %q", allowed, reason)
	}
}
