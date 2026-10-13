package store

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/loop"
)

func TestExportMarkdown(t *testing.T) {
	s, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := "sess-1"
	events := []loop.Event{
		{Kind: loop.EvUserMessage, Time: time.Now(), Content: "hello"},
		{Kind: loop.EvAssistantChunk, Time: time.Now(), Content: "partial"},
		{Kind: loop.EvAssistantMessage, Time: time.Now(), Content: "hi there"},
		{Kind: loop.EvToolCall, Time: time.Now(), ToolCall: &loop.ToolCall{Name: "bash", Arguments: `{"command":"ls"}`}},
		{Kind: loop.EvToolResult, Time: time.Now(), ToolName: "bash", Result: "a.go"},
		{Kind: loop.EvTurnEnd, Time: time.Now(), Usage: &loop.Usage{InputTokens: 10, OutputTokens: 5}},
	}
	for _, ev := range events {
		if err := s.Append(id, ev); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if err := s.ExportMarkdown(id, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"# Session sess-1", "## User", "hello", "## Assistant", "hi there", "## Tool: bash", `"command":"ls"`, "a.go", "15 tokens"} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown export missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "partial") {
		t.Error("assistant chunk should not be exported")
	}
}

func TestExportJSONL(t *testing.T) {
	s, _ := NewSessionStore(t.TempDir())
	id := "s2"
	if err := s.Append(id, loop.Event{Kind: loop.EvUserMessage, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.Export(id, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"kind":"user_message"`) {
		t.Fatalf("jsonl export wrong: %s", buf.String())
	}
}

func TestMetaRoundTripAndList(t *testing.T) {
	s, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append("sess-a", loop.Event{Kind: loop.EvUserMessage, Content: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMeta("sess-a", Meta{Workspace: "/ws/a", Model: "m1"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.LoadMeta("sess-a")
	if err != nil || first.Workspace != "/ws/a" || first.CreatedAt.IsZero() {
		t.Fatalf("meta = %+v, %v", first, err)
	}
	if err := s.SaveMeta("sess-a", Meta{Workspace: "/ws/a", Model: "m2"}); err != nil {
		t.Fatal(err)
	}
	second, _ := s.LoadMeta("sess-a")
	if !second.CreatedAt.Equal(first.CreatedAt) || second.Model != "m2" {
		t.Fatalf("meta not merged: %+v", second)
	}
	recs, err := s.List()
	if err != nil || len(recs) != 1 {
		t.Fatalf("list = %+v, %v", recs, err)
	}
	if recs[0].Workspace != "/ws/a" || recs[0].Title != "first" {
		t.Fatalf("record = %+v", recs[0])
	}
}
