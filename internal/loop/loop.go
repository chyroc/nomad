// Package loop defines the backend-neutral conversation event model.
//
// A Runner drives one user turn against a managed-agents session and
// emits Events; the CLI renders them. Concrete tool/model execution lives
// in the ma package (server-side agent loop, local self-hosted worker).
package loop

import (
	"context"
	"errors"
	"time"
)

// ErrMaxTurns is returned when a turn is cut short by the --max-turns
// model-round cap, mirroring the reference CLI's error_max_turns result.
var ErrMaxTurns = errors.New("reached --max-turns limit")

// Event kinds emitted during a turn.
const (
	EvUserMessage       = "user_message"
	EvAssistantChunk    = "assistant_chunk"
	EvAssistantMessage  = "assistant_message"
	EvAssistantThinking = "assistant_thinking"
	EvToolCall          = "tool_call"
	EvToolResult        = "tool_result"
	EvTurnEnd           = "turn_end"
	EvError             = "error"
)

// ToolCall references one tool invocation by the model.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// Usage reports token accounting for a turn.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Event is one immutable entry in a session timeline.
type Event struct {
	Kind     string    `json:"kind"`
	Time     time.Time `json:"time"`
	Content  string    `json:"content,omitempty"`
	ToolCall *ToolCall `json:"tool_call,omitempty"`
	ToolName string    `json:"tool_name,omitempty"`
	Result   string    `json:"result,omitempty"`
	IsError  bool      `json:"is_error,omitempty"`
	Usage    *Usage    `json:"usage,omitempty"`
}

// Observer receives loop events.
type Observer interface {
	OnEvent(ev Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(ev Event)

func (f ObserverFunc) OnEvent(ev Event) { f(ev) }

// Runner drives one conversation hosted by a backend.
type Runner interface {
	Run(ctx context.Context, userInput string, attachments []Attachment) error
	Subscribe(o Observer)
	SessionID() string
	Close() error
}

// Interruptor cancels a running turn server-side as well as locally.
type Interruptor interface {
	Interrupt(ctx context.Context) error
}

// Attachment is a multimodal input (image/document) attached to a turn.
type Attachment struct {
	Kind   string
	Source string
	Path   string
	URL    string
	FileID string
}
