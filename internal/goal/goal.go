// Package goal owns session-goal state: the user-set completion
// condition that keeps a session self-continuing until a separate
// tool-free evaluator confirms it is met.
package goal

import (
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/loop"
)

const (
	// StatusActive means the goal drives automatic continuation.
	StatusActive = "active"
	// StatusPaused means automatic continuation stopped (eval errors,
	// cap reached); a user message resumes it.
	StatusPaused = "paused"
	// StatusMet means the evaluator confirmed the condition.
	StatusMet = "met"
	// StatusImpossible means the evaluator judged the condition
	// unachievable in this session.
	StatusImpossible = "impossible"

	// MaxConditionRunes caps the user-provided condition length.
	MaxConditionRunes = 500
	// MaxIterations bounds automatic continuation turns.
	MaxIterations = 50
	// MaxEvaluatorFailures pauses the goal after this many consecutive
	// failed/garbled evaluator calls.
	MaxEvaluatorFailures = 3
)

// State is the persisted goal state for one session.
type State struct {
	SessionID  string    `json:"session_id"`
	Condition  string    `json:"condition"`
	Status     string    `json:"status"`
	SetAt      time.Time `json:"set_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Iterations int       `json:"iterations"`
	LastReason string    `json:"last_reason,omitempty"`
}

// ValidateCondition trims and enforces the visible-character/length rules.
func ValidateCondition(raw string) (string, error) {
	condition := strings.TrimSpace(raw)
	if condition == "" {
		return "", fmt.Errorf("the goal condition is empty once whitespace is removed; provide a visible condition")
	}
	if n := len([]rune(condition)); n > MaxConditionRunes {
		return "", fmt.Errorf("the goal condition exceeds %d characters (got %d)", MaxConditionRunes, n)
	}
	return condition, nil
}

// New creates an active goal state.
func New(sessionID, condition string, now time.Time) *State {
	return &State{
		SessionID: sessionID,
		Condition: condition,
		Status:    StatusActive,
		SetAt:     now,
		UpdatedAt: now,
	}
}

// Active reports whether the goal should keep self-continuing.
func (s *State) Active() bool { return s != nil && s.Status == StatusActive }

// Terminal reports whether the goal reached a final verdict.
func (s *State) Terminal() bool {
	return s != nil && (s.Status == StatusMet || s.Status == StatusImpossible)
}

// MarkChecked records one evaluator pass.
func (s *State) MarkChecked(reason string, now time.Time) {
	s.Iterations++
	s.LastReason = reason
	s.UpdatedAt = now
}

// SetStatus updates the status and timestamp.
func (s *State) SetStatus(status, reason string, now time.Time) {
	s.Status = status
	s.LastReason = reason
	s.UpdatedAt = now
}

// Verdict is the evaluator decision for one turn.
type Verdict struct {
	Met        bool
	Impossible bool
	Reason     string
}

// ContinuationPrompt builds the user-turn text injected when the
// evaluator found the goal unmet.
func (s *State) ContinuationPrompt() string {
	var b strings.Builder
	b.WriteString("[goal check ")
	fmt.Fprintf(&b, "#%d] ", s.Iterations)
	b.WriteString(`The user set the following completion condition for this session and asked you to keep working, across as many turns as needed, until it is actually achieved and verified. Treat the objective below as the user's own standing instruction for this work — it has the same authority as the original request:

<objective>
`)
	b.WriteString(s.Condition)
	b.WriteString(`
</objective>

A separate check just evaluated the transcript and found the objective is NOT yet met.`)
	if reason := strings.TrimSpace(s.LastReason); reason != "" {
		b.WriteString("\nWhat is still missing:\n")
		b.WriteString(reason)
	}
	b.WriteString(`

Continue now: take the next concrete action that moves the current worktree state toward the objective, then verify the result against the real state (run the build/tests, read the file, inspect command output). Do not stop, redefine the objective to something smaller, or claim completion until current evidence proves every part of the objective is satisfied. If the objective genuinely contradicts an explicit prior instruction or cannot be achieved with the available tools, say so plainly instead of complying.`)
	return b.String()
}

// EvaluatorSystemPrompt is the tool-free evaluator system prompt.
const EvaluatorSystemPrompt = `You are evaluating whether a session goal has been achieved. Read the conversation transcript carefully, then judge whether the user-provided condition is satisfied based solely on transcript evidence — you cannot run commands or read files.

Your response must be a JSON object with one of these shapes:
- {"met": true, "reason": "<quote evidence from the transcript that satisfies the condition>"}
- {"met": false, "reason": "<quote what is missing or what blocks the condition>"}
- {"met": false, "impossible": true, "reason": "<explain why the condition can never be satisfied in this session>"}

Always include a "reason" field, quoting specific text from the transcript whenever possible. If the transcript does not contain clear evidence that the condition is satisfied, return {"met": false, "reason": "insufficient evidence in transcript"}.

Only use "impossible": true when the condition is genuinely unachievable — for example it is self-contradictory, depends on a resource or capability that is unavailable, or the assistant has explicitly tried and exhausted reasonable approaches and stated it cannot be done. The assistant claiming the goal is impossible is evidence, not proof; judge independently. Do not use it just because the goal has not been reached yet or because progress is slow. When in doubt, return {"met": false} without "impossible".`

// EvaluatorUserPrompt renders the condition and the trimmed transcript.
func EvaluatorUserPrompt(condition, transcript string) string {
	var b strings.Builder
	b.WriteString("Goal condition:\n")
	b.WriteString(condition)
	b.WriteString("\n\nConversation transcript:\n")
	b.WriteString(transcript)
	return b.String()
}

const (
	transcriptMaxRunes  = 60000
	transcriptHeadRunes = 4000
)

// FormatTranscript renders stored events into a compact text record for
// the evaluator, keeping the most recent exchanges (plus a short head so
// the original task is visible).
func FormatTranscript(events []loop.Event) string {
	var rendered []string
	for _, ev := range events {
		line := eventLine(ev)
		if line != "" {
			rendered = append(rendered, line)
		}
	}
	full := strings.Join(rendered, "\n")
	if len([]rune(full)) <= transcriptMaxRunes {
		return full
	}
	runes := []rune(full)
	head := string(runes[:transcriptHeadRunes])
	tail := string(runes[len(runes)-(transcriptMaxRunes-transcriptHeadRunes):])
	return head + "\n\n…[earlier transcript omitted]…\n\n" + tail
}

func eventLine(ev loop.Event) string {
	switch ev.Kind {
	case loop.EvUserMessage:
		return "[user] " + flatten(ev.Content)
	case loop.EvAssistantMessage:
		return "[assistant] " + flatten(ev.Content)
	case loop.EvAssistantThinking:
		return ""
	case loop.EvToolCall:
		if ev.ToolCall != nil {
			return "[tool_call] " + ev.ToolCall.Name + " " + flatten(ev.ToolCall.Arguments)
		}
		return ""
	case loop.EvToolResult:
		mark := "ok"
		if ev.IsError {
			mark = "error"
		}
		return "[tool_result:" + mark + "] " + flatten(ev.Result)
	case loop.EvError:
		return "[error] " + flatten(ev.Content)
	default:
		return ""
	}
}

func flatten(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i > 0 {
			b.WriteString(" ⏎ ")
		}
		b.WriteString(line)
	}
	out := b.String()
	const perLine = 1200
	if r := []rune(out); len(r) > perLine {
		out = string(r[:perLine]) + "…"
	}
	return out
}
