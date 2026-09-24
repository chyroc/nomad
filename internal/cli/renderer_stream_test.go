package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chyroc/nomad/internal/loop"
)

func TestStreamRenderer(t *testing.T) {
	var buf bytes.Buffer
	r := NewStreamRenderer(&buf, "m1", "sesn-1", "bypassPermissions")
	r.Init("/work")
	r.OnEvent(loop.Event{Kind: loop.EvAssistantChunk, Content: "hi"})
	r.OnEvent(loop.Event{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{ID: "t1", Name: "bash", Arguments: `{"command":"ls"}`}})
	r.OnEvent(loop.Event{Kind: loop.EvToolResult, ToolCall: &loop.ToolCall{ID: "t1", Name: "bash"}, Result: "ok"})
	r.Result("hi", "sesn-1", &loop.Usage{InputTokens: 5, OutputTokens: 2}, SubtypeSuccess, 1)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 json lines, got %d:\n%s", len(lines), buf.String())
	}
	var init map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &init)
	if init["type"] != "system" || init["subtype"] != "init" || init["session_id"] != "sesn-1" {
		t.Fatalf("bad init: %v", init)
	}
	var result map[string]interface{}
	json.Unmarshal([]byte(lines[4]), &result)
	if result["type"] != "result" || result["result"] != "hi" || result["subtype"] != "success" {
		t.Fatalf("bad result: %v", result)
	}
}
