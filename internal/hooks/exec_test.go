package hooks

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("NOMAD_HOOK_HELPER") == "1" {
		runHelper()
		return
	}
	os.Exit(m.Run())
}

func runHelper() {
	mode := os.Getenv("NOMAD_HOOK_MODE")
	var env Envelope
	data, _ := io.ReadAll(os.Stdin)
	_ = json.Unmarshal(data, &env)
	switch mode {
	case "allow":
		os.Stdout.WriteString("plain stdout context")
	case "deny-exit":
		os.Stderr.WriteString("exit two reason")
		os.Exit(2)
	case "deny-json":
		os.Stdout.WriteString(`{"decision":"deny","reason":"json says no"}`)
	case "allow-json":
		os.Stdout.WriteString(`{"decision":"allow","additionalContext":"json context"}`)
	case "ctx-json":
		os.Stdout.WriteString(`{"additionalContext":"extra notes"}`)
	case "fail":
		os.Exit(3)
	case "echo-tool":
		os.Stdout.WriteString(env.ToolName)
	}
}

type fakeRunner struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	delay    time.Duration
	got      []byte
}

func (f *fakeRunner) Run(ctx context.Context, cwd, command string, stdin []byte) ([]byte, []byte, int, error) {
	f.got = append(f.got, stdin...)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, nil, -1, ctx.Err()
		}
	}
	return f.stdout, f.stderr, f.exitCode, nil
}

func newFakeExecutor(r CommandRunner) *Executor {
	return &Executor{
		Runner:     r,
		Timeout:    time.Second,
		MinTimeout: time.Millisecond,
		MaxTimeout: time.Minute,
	}
}

func helperCommand(mode string) string {
	exe, _ := os.Executable()
	return "NOMAD_HOOK_HELPER=1 NOMAD_HOOK_MODE=" + mode + " " + exe
}

func shellExecutor() *Executor {
	return &Executor{
		Runner:     shellRunner{},
		Timeout:    5 * time.Second,
		MinTimeout: 100 * time.Millisecond,
		MaxTimeout: time.Minute,
	}
}

func TestHookAllowPlainStdout(t *testing.T) {
	r := &fakeRunner{stdout: []byte("plain stdout context")}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventPostToolUse, CWD: t.TempDir()})
	if out.Blocked {
		t.Fatal("should not block")
	}
	if out.AdditionalContext != "plain stdout context" {
		t.Fatalf("context = %q", out.AdditionalContext)
	}
}

func TestHookDenyExitTwo(t *testing.T) {
	r := &fakeRunner{exitCode: 2, stderr: []byte("exit two reason")}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventPreToolUse, CWD: t.TempDir()})
	if !out.Blocked || out.Reason != "exit two reason" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestHookDenyJSON(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"decision":"deny","reason":"json says no"}`)}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventPreToolUse, CWD: t.TempDir()})
	if !out.Blocked || out.Reason != "json says no" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestHookAllowJSON(t *testing.T) {
	r := &fakeRunner{stdout: []byte(`{"decision":"allow","additionalContext":"json context"}`)}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventUserPromptSubmit, CWD: t.TempDir()})
	if out.Blocked || out.AdditionalContext != "json context" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestHookTimeout(t *testing.T) {
	r := &fakeRunner{delay: 2 * time.Second}
	e := newFakeExecutor(r)
	e.Timeout = 100 * time.Millisecond
	done := make(chan Outcome, 1)
	go func() {
		done <- e.Run(context.Background(), []Hook{{Command: "x"}},
			Envelope{Event: EventPreToolUse, CWD: t.TempDir()})
	}()
	select {
	case out := <-done:
		if !out.Blocked {
			t.Fatal("timeout should block a pre-tool hook")
		}
	case <-time.After(time.Second):
		t.Fatal("hook timeout did not fire")
	}
}

func TestHookNonzeroFailClosedForPreTool(t *testing.T) {
	r := &fakeRunner{exitCode: 3, stderr: []byte("boom")}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventPreToolUse, CWD: t.TempDir()})
	if !out.Blocked || out.Reason != "boom" {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestHookNonzeroContinuesForPostTool(t *testing.T) {
	r := &fakeRunner{exitCode: 3, stderr: []byte("boom")}
	out := newFakeExecutor(r).Run(context.Background(), []Hook{{Command: "x"}},
		Envelope{Event: EventPostToolUse, CWD: t.TempDir()})
	if out.Blocked {
		t.Fatal("nonzero post-tool hook should not block")
	}
}

func TestHookMatcher(t *testing.T) {
	r := &fakeRunner{stdout: []byte("matched")}
	e := newFakeExecutor(r)
	env := Envelope{Event: EventPreToolUse, CWD: t.TempDir(), ToolName: "write"}
	out := e.Run(context.Background(), []Hook{{Matcher: "bash", Command: "x"}}, env)
	if out.AdditionalContext != "" {
		t.Fatalf("non-matching hook should be skipped, got %q", out.AdditionalContext)
	}
	out = e.Run(context.Background(), []Hook{{Matcher: "read,write", Command: "x"}}, env)
	if out.AdditionalContext != "matched" {
		t.Fatalf("comma matcher should match, got %q", out.AdditionalContext)
	}
	out = e.Run(context.Background(), []Hook{{Matcher: "wr*", Command: "x"}}, env)
	if out.AdditionalContext != "matched" {
		t.Fatalf("glob matcher should match, got %q", out.AdditionalContext)
	}
}

func TestHookEnvelopeOnStdin(t *testing.T) {
	r := shellRunner{}
	out, _, code, err := r.Run(context.Background(), t.TempDir(),
		helperCommand("echo-tool"),
		mustJSON(Envelope{ToolName: "grep"}))
	if err != nil || code != 0 || string(out) != "grep" {
		t.Fatalf("envelope delivery failed: code=%d out=%q err=%v", code, out, err)
	}
}

func mustJSON(v Envelope) []byte {
	b, _ := json.Marshal(v)
	return b
}

func TestHookMatchesUnit(t *testing.T) {
	cases := []struct {
		matcher, target string
		want            bool
	}{
		{"", "bash", true},
		{"*", "read", true},
		{"bash", "bash", true},
		{"bash", "read", false},
		{"bash,read", "read", true},
		{"ba*", "bash", true},
	}
	for _, c := range cases {
		if got := (Hook{Matcher: c.matcher}).Matches(c.target); got != c.want {
			t.Errorf("Matches(%q,%q)=%v want %v", c.matcher, c.target, got, c.want)
		}
	}
}
