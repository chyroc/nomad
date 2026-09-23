package cli

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/chyroc/nomad/internal/loop"
)

// OutputFormat selects a headless output mode.
type OutputFormat string

const (
	FormatText       OutputFormat = "text"
	FormatJSON       OutputFormat = "json"
	FormatStreamJSON OutputFormat = "stream-json"
)

// InitMessage is the stream-json system/init payload.
type InitMessage struct {
	Type           string   `json:"type"`
	Subtype        string   `json:"subtype"`
	Cwd            string   `json:"cwd"`
	Model          string   `json:"model"`
	SessionID      string   `json:"session_id"`
	PermissionMode string   `json:"permission_mode"`
	OutputStyle    string   `json:"output_style"`
	Tools          []string `json:"tools"`
}

// StreamRenderer emits newline-delimited JSON events for headless use.
type StreamRenderer struct {
	w        io.Writer
	enc      *json.Encoder
	model    string
	session  string
	permMode string
	sentInit bool
}

func NewStreamRenderer(w io.Writer, model, session, permMode string) *StreamRenderer {
	return &StreamRenderer{
		w:        w,
		enc:      json.NewEncoder(w),
		model:    model,
		session:  session,
		permMode: permMode,
	}
}

func (s *StreamRenderer) Init(cwd string) {
	s.enc.Encode(InitMessage{
		Type: "system", Subtype: "init", Cwd: cwd, Model: s.model,
		SessionID: s.session, PermissionMode: s.permMode,
		OutputStyle: "default",
		Tools:       []string{"bash", "read", "write", "edit", "glob", "grep"},
	})
}

func (s *StreamRenderer) OnEvent(ev loop.Event) {
	switch ev.Kind {
	case loop.EvAssistantChunk:
		s.assistant(ev.Content)
	case loop.EvAssistantThinking:
		s.assistantThinking(ev.Content)
	case loop.EvToolCall:
		s.toolUse(ev)
	case loop.EvToolResult:
		s.toolResult(ev)
	case loop.EvError:
		s.enc.Encode(map[string]interface{}{"type": "error", "error": ev.Content})
	}
}

type streamContentBlock map[string]interface{}

func (s *StreamRenderer) assistant(text string) {
	s.enc.Encode(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": []streamContentBlock{{"type": "text", "text": text}},
		},
	})
}

func (s *StreamRenderer) assistantThinking(text string) {
	s.enc.Encode(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role":    "assistant",
			"content": []streamContentBlock{{"type": "thinking", "thinking": text}},
		},
	})
}

func (s *StreamRenderer) toolUse(ev loop.Event) {
	var input interface{}
	if ev.ToolCall != nil && ev.ToolCall.Arguments != "" {
		_ = json.Unmarshal([]byte(ev.ToolCall.Arguments), &input)
	}
	name := ev.ToolName
	id := ""
	if ev.ToolCall != nil {
		name = ev.ToolCall.Name
		id = ev.ToolCall.ID
	}
	s.enc.Encode(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"role": "assistant",
			"content": []streamContentBlock{{
				"type": "tool_use", "id": id, "name": name, "input": input,
			}},
		},
	})
}

func (s *StreamRenderer) toolResult(ev loop.Event) {
	id := ""
	if ev.ToolCall != nil {
		id = ev.ToolCall.ID
	}
	s.enc.Encode(map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role": "user",
			"content": []streamContentBlock{{
				"type": "tool_result", "tool_use_id": id,
				"content": ev.Result, "is_error": ev.IsError,
			}},
		},
	})
}

// Result is the final headless result message (stream-json and json).
type Result struct {
	Type         string  `json:"type"`
	Subtype      string  `json:"subtype"`
	SessionID    string  `json:"session_id"`
	Result       string  `json:"result"`
	IsError      bool    `json:"is_error"`
	NumTurns     int     `json:"num_turns"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
}

func (s *StreamRenderer) Result(text, sessionID string, usage *loop.Usage, isErr bool) {
	r := Result{Type: "result", Subtype: "success", SessionID: sessionID, Result: text, NumTurns: 1}
	if isErr {
		r.Subtype = "error_during_execution"
		r.IsError = true
	}
	if usage != nil {
		r.InputTokens, r.OutputTokens = usage.InputTokens, usage.OutputTokens
	}
	s.enc.Encode(r)
}

// CollectRenderer accumulates the final assistant text and usage for
// --output-format json (single object, no streaming).
type CollectRenderer struct {
	Text  strings.Builder
	Usage *loop.Usage
}

func (c *CollectRenderer) OnEvent(ev loop.Event) {
	if ev.Kind == loop.EvAssistantChunk {
		c.Text.WriteString(ev.Content)
	}
	if ev.Kind == loop.EvTurnEnd && ev.Usage != nil {
		c.Usage = ev.Usage
	}
}
