package ark

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/volcengine/ark-runtime-go/arkruntime/selfhosted"

	"github.com/chyroc/nomad/internal/loop"
)

// ErrInterrupted is returned when a turn is cancelled locally or via the
// server-side user.interrupt event.
var ErrInterrupted = errors.New("interrupted")

// terminalGrace lets trailing frames already buffered after end_turn
// (token usage span, repeated idle echoes) flush before returning. It is
// short and is NOT reset by tool-result chatter from the SDK's list/event
// compensation loop, which can otherwise postpone shutdown indefinitely.
const terminalGrace = 1500 * time.Millisecond

func (r *Runner) pump(ctx context.Context, stream *selfhosted.EventStream, toolResults <-chan selfhosted.ToolCallResult) error {
	events := stream.Events()
	var graceC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return ErrInterrupted

		case res, ok := <-toolResults:
			if !ok {
				toolResults = nil
				continue
			}
			if graceC == nil {
				r.emitToolResult(res)
			}

		case <-graceC:
			return nil

		case ev, ok := <-events:
			if !ok {
				if err := stream.Err(); err != nil {
					return err
				}
				return errors.New("ma: event stream closed before session went idle")
			}
			idle, terminated, err := r.handleStreamEvent(ev)
			if err != nil {
				return err
			}
			if terminated {
				r.emit(loop.Event{Kind: loop.EvTurnEnd})
				return nil
			}
			if idle && graceC == nil {
				usage := r.usage
				r.emit(loop.Event{Kind: loop.EvTurnEnd, Usage: &usage})
				graceC = time.After(terminalGrace)
			}
		}
	}
}

func (r *Runner) handleStreamEvent(ev selfhosted.Event) (idle, terminated bool, err error) {
	switch ev.Type {
	case "agent.message":
		text := textFromBlocks(ev.Content)
		if text != "" {
			r.emit(loop.Event{Kind: loop.EvAssistantChunk, Content: text})
			r.emit(loop.Event{Kind: loop.EvAssistantMessage, Content: text})
		}

	case "agent.thinking":
		if text := textFromBlocks(ev.Content); text != "" {
			r.emit(loop.Event{Kind: loop.EvAssistantThinking, Content: text})
		}

	case "agent.tool_use":
		r.emit(loop.Event{Kind: loop.EvToolCall, ToolCall: &loop.ToolCall{
			ID: ev.ToolUseID, Name: ev.Name, Arguments: string(ev.Input),
		}})

	case "span.model_request_end":
		if raw, ok := ev.Extra["model_usage"]; ok {
			var u struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}
			if json.Unmarshal(raw, &u) == nil {
				r.usage.InputTokens += u.InputTokens
				r.usage.OutputTokens += u.OutputTokens
			}
		}

	case selfhosted.EventTypeSessionStatusIdle:
		switch ev.StopReasonType() {
		case selfhosted.SessionStopReasonEndTurn:
			return true, false, nil
		case "user_interrupt":
			return false, false, ErrInterrupted
		default:
			return false, false, nil
		}

	case selfhosted.EventTypeSessionStatusTerminated, "session.deleted":
		return false, true, nil

	case "session.error":
		msg := ev.Name
		if msg == "" {
			msg = "session error"
		}
		r.emit(loop.Event{Kind: loop.EvError, Content: msg, IsError: true})
		return false, false, errors.New("ma: " + msg)
	}
	return false, false, nil
}

func (r *Runner) emitToolResult(res selfhosted.ToolCallResult) {
	var parts []string
	isErr := false
	for _, b := range res.Result.Content {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	if res.Result.IsError != nil {
		isErr = *res.Result.IsError
	}
	r.emit(loop.Event{
		Kind:     loop.EvToolResult,
		ToolName: res.Name,
		Result:   strings.Join(parts, "\n"),
		IsError:  isErr,
		ToolCall: &loop.ToolCall{ID: res.ToolUseID, Name: res.Name},
	})
}

func textFromBlocks(blocks []selfhosted.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}
