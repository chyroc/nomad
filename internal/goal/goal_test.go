package goal

import (
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
)

func TestValidateCondition(t *testing.T) {
	if _, err := ValidateCondition("   "); err == nil {
		t.Fatal("empty condition should be rejected")
	}
	if _, err := ValidateCondition(strings.Repeat("好", MaxConditionRunes)); err != nil {
		t.Fatalf("runes at cap rejected: %v", err)
	}
	if _, err := ValidateCondition(strings.Repeat("a", MaxConditionRunes+1)); err == nil {
		t.Fatal("over-long condition should be rejected")
	}
	got, err := ValidateCondition("  do thing\n ")
	if err != nil || got != "do thing" {
		t.Fatalf("trim = %q, %v", got, err)
	}
}

func TestStateLifecycle(t *testing.T) {
	now := time.Unix(1000, 0)
	st := New("s1", "ship it", now)
	if !st.Active() || st.Terminal() {
		t.Fatal("new goal must be active and non-terminal")
	}
	st.MarkChecked("missing tests", now.Add(time.Second))
	if st.Iterations != 1 || st.LastReason != "missing tests" {
		t.Fatalf("mark check = %d %q", st.Iterations, st.LastReason)
	}
	st.SetStatus(StatusMet, "done", now.Add(2*time.Second))
	if !st.Terminal() || st.Active() {
		t.Fatal("met goal must be terminal")
	}
}

func TestContinuationPromptCarriesConditionAndCheck(t *testing.T) {
	st := New("s1", "land the feature", time.Now())
	st.MarkChecked("ci red", time.Now())
	prompt := st.ContinuationPrompt()
	if !strings.Contains(prompt, "#1") || !strings.Contains(prompt, "land the feature") ||
		!strings.Contains(prompt, "ci red") {
		t.Fatalf("prompt missing check/condition/reason:\n%s", prompt)
	}
}

func TestFormatTranscript(t *testing.T) {
	events := []loop.Event{
		{Kind: loop.EvUserMessage, Content: "fix it"},
		{Kind: loop.EvAssistantThinking, Content: "secret reasoning"},
		{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{Name: "bash", Arguments: `{"command":"go test"}`}},
		{Kind: loop.EvToolResult, Result: "ok\n", IsError: false},
		{Kind: loop.EvAssistantMessage, Content: "done"},
	}
	out := FormatTranscript(events)
	for _, want := range []string{"[user] fix it", "[tool_call] bash", "[tool_result:ok]", "[assistant] done"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret reasoning") {
		t.Fatal("thinking must be omitted from evaluator transcript")
	}
}

func TestFormatTranscriptTruncationKeepsTail(t *testing.T) {
	events := []loop.Event{{Kind: loop.EvUserMessage, Content: "HEAD-MARKER " + strings.Repeat("a", 5000)}}
	for i := 0; i < 60; i++ {
		events = append(events, loop.Event{Kind: loop.EvAssistantMessage, Content: strings.Repeat("b", 4000)})
	}
	events = append(events, loop.Event{Kind: loop.EvAssistantMessage, Content: "TAIL-MARKER"})
	out := FormatTranscript(events)
	if !strings.Contains(out, "TAIL-MARKER") {
		t.Fatal("tail must survive truncation")
	}
	if !strings.Contains(out, "omitted") {
		t.Fatal("truncation marker missing")
	}
}
