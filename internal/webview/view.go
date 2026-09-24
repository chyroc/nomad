package webview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/diff"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/yuin/goldmark"
	gmextension "github.com/yuin/goldmark/extension"
)

var markdownRenderer = goldmark.New(goldmark.WithExtensions(gmextension.GFM))

const (
	blockMessage  = "message"
	blockActivity = "activity"
	blockError    = "error"

	stepThinking = "thinking"
	stepTool     = "tool"
)

// MessageBlock is one user or assistant message.
type MessageBlock struct {
	Type string    `json:"type"`
	Role string    `json:"role"`
	Time time.Time `json:"time"`
	HTML string    `json:"html,omitempty"`
}

// DiffLine is one row of a tool-call diff.
type DiffLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// Step is one reasoning or tool-call entry inside an activity.
type Step struct {
	Kind       string     `json:"kind"`
	Time       time.Time  `json:"time"`
	Text       string     `json:"text,omitempty"`
	Name       string     `json:"name,omitempty"`
	Args       string     `json:"args,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	Result     string     `json:"result,omitempty"`
	IsError    bool       `json:"is_error,omitempty"`
	Pending    bool       `json:"pending,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	Diff       []DiffLine `json:"diff,omitempty"`
}

// ToolCount summarizes how many times one tool ran in an activity.
type ToolCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ActivityBlock groups the reasoning and tool steps of one turn. While
// a turn is running Settled is false and the UI renders steps live;
// after the final answer it collapses to a one-line summary.
type ActivityBlock struct {
	Type          string      `json:"type"`
	StartTime     time.Time   `json:"start_time"`
	EndTime       time.Time   `json:"end_time,omitempty"`
	Settled       bool        `json:"settled"`
	ThinkingLines int         `json:"thinking_lines"`
	Steps         []*Step     `json:"steps"`
	Tools         []ToolCount `json:"tools"`
	ToolCount     int         `json:"tool_count"`
	InTokens      int         `json:"in_tokens,omitempty"`
	OutTokens     int         `json:"out_tokens,omitempty"`
}

// ErrorBlock is a session-level error.
type ErrorBlock struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

// Block is a timeline entry; exactly one of the concrete block kinds.
type Block map[string]any

// SessionData is the JSON payload served at /api/session.
type SessionData struct {
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	Workspace string    `json:"workspace"`
	UpdatedAt time.Time `json:"updated_at"`
	Blocks    []Block   `json:"blocks"`
}

type pendingCall struct {
	index int
	start time.Time
}

type activityBuilder struct {
	block   *ActivityBlock
	pending map[string]pendingCall
}

func buildSessionData(sessionID, workspace string, events []loop.Event) SessionData {
	data := SessionData{
		SessionID: sessionID,
		Workspace: workspace,
		Blocks:    []Block{},
	}
	act := &activityBuilder{}

	flush := func(settled bool) {
		if act.block == nil {
			return
		}
		finalizeActivity(act.block, settled)
		data.Blocks = append(data.Blocks, toBlock(act.block))
		act.block = nil
		act.pending = nil
	}
	ensure := func(evTime time.Time) *activityBuilder {
		if act.block == nil {
			act.block = &ActivityBlock{Type: blockActivity, StartTime: evTime, Steps: []*Step{}}
			act.pending = map[string]pendingCall{}
		}
		return act
	}

	for _, ev := range events {
		switch ev.Kind {
		case loop.EvUserMessage:
			flush(true)
			data.Blocks = append(data.Blocks, toBlock(&MessageBlock{
				Type: blockMessage, Role: "user", Time: ev.Time, HTML: renderMarkdown(ev.Content),
			}))
			if data.Title == "" {
				data.Title = firstLine(ev.Content, 80)
			}
		case loop.EvAssistantThinking:
			appendThinking(ensure(ev.Time).block, ev)
		case loop.EvAssistantChunk:
		case loop.EvAssistantMessage:
			if strings.TrimSpace(ev.Content) == "" {
				break
			}
			flush(true)
			data.Blocks = append(data.Blocks, toBlock(&MessageBlock{
				Type: blockMessage, Role: "assistant", Time: ev.Time, HTML: renderMarkdown(ev.Content),
			}))
		case loop.EvToolCall:
			if ev.ToolCall == nil {
				break
			}
			b := ensure(ev.Time).block
			act.pending[ev.ToolCall.ID] = pendingCall{index: len(b.Steps), start: ev.Time}
			b.Steps = append(b.Steps, &Step{
				Kind:    stepTool,
				Time:    ev.Time,
				Name:    ev.ToolCall.Name,
				Args:    prettyJSON(ev.ToolCall.Arguments),
				Summary: toolSummary(ev.ToolCall.Name, ev.ToolCall.Arguments, ""),
				Pending: true,
			})
		case loop.EvToolResult:
			attachToolResult(ensure(ev.Time), ev, workspace)
		case loop.EvTurnEnd:
			if act.block != nil {
				if ev.Usage != nil {
					act.block.InTokens = ev.Usage.InputTokens
					act.block.OutTokens = ev.Usage.OutputTokens
				}
				if act.block.EndTime.IsZero() {
					act.block.EndTime = ev.Time
				}
				flush(true)
			} else if last := lastActivityBlock(data.Blocks); last != nil {
				if ev.Usage != nil {
					setActTokens(last, ev.Usage.InputTokens, ev.Usage.OutputTokens)
				}
				if !ev.Time.IsZero() {
					setActEnd(last, ev.Time)
				}
			}
		case loop.EvError:
			flush(true)
			data.Blocks = append(data.Blocks, toBlock(&ErrorBlock{Type: blockError, Time: ev.Time, Text: ev.Content}))
		}
		if !ev.Time.IsZero() && ev.Time.After(data.UpdatedAt) {
			data.UpdatedAt = ev.Time
		}
	}
	flush(false)
	return data
}

func appendThinking(b *ActivityBlock, ev loop.Event) {
	text := strings.TrimSpace(ev.Content)
	if text == "" {
		return
	}
	if n := len(b.Steps); n > 0 && b.Steps[n-1].Kind == stepThinking {
		last := b.Steps[n-1]
		if last.Text != "" {
			last.Text += "\n"
		}
		last.Text += text
		return
	}
	b.Steps = append(b.Steps, &Step{Kind: stepThinking, Time: ev.Time, Text: text})
}

func attachToolResult(a *activityBuilder, ev loop.Event, workspace string) {
	b := a.block
	name := ev.ToolName
	callID := ""
	if ev.ToolCall != nil {
		callID = ev.ToolCall.ID
		if name == "" {
			name = ev.ToolCall.Name
		}
	}
	if name == "" {
		name = "tool"
	}
	var pc pendingCall
	idx := -1
	if hit, ok := a.pending[callID]; ok && hit.index >= 0 && hit.index < len(b.Steps) {
		pc, idx = hit, hit.index
		delete(a.pending, callID)
	} else if n, start := a.takePendingByName(name); n >= 0 {
		pc, idx = pendingCall{index: n, start: start}, n
	}
	var step *Step
	if idx >= 0 {
		step = b.Steps[idx]
	} else {
		step = &Step{Kind: stepTool, Time: ev.Time}
		b.Steps = append(b.Steps, step)
	}
	step.Name = name
	step.Summary = toolSummary(name, step.Args, ev.Result)
	step.Result = ev.Result
	step.IsError = ev.IsError
	step.Pending = false
	if !pc.start.IsZero() && !ev.Time.IsZero() {
		step.DurationMS = ev.Time.Sub(pc.start).Milliseconds()
	}
	if lines, ok := diff.ToolCall(name, step.Args, workspace); ok {
		step.Diff = make([]DiffLine, 0, len(lines))
		for _, l := range lines {
			step.Diff = append(step.Diff, DiffLine{Kind: string(l.Kind), Text: l.Text})
		}
	}
	if b.EndTime.IsZero() || ev.Time.After(b.EndTime) {
		b.EndTime = ev.Time
	}
}

// takePendingByName matches a result that lost its call id by tool
// name, consuming the most recent still-pending call of that name.
func (a *activityBuilder) takePendingByName(name string) (int, time.Time) {
	bestKey := ""
	best := pendingCall{index: -1}
	for key, pc := range a.pending {
		if pc.index < 0 || pc.index >= len(a.block.Steps) {
			continue
		}
		if s := a.block.Steps[pc.index]; s.Pending && s.Name == name {
			if pc.index >= best.index {
				best, bestKey = pc, key
			}
		}
	}
	if best.index < 0 {
		return -1, time.Time{}
	}
	delete(a.pending, bestKey)
	return best.index, best.start
}

func finalizeActivity(b *ActivityBlock, settled bool) {
	b.Settled = settled
	b.Tools = []ToolCount{}
	if settled {
		for _, s := range b.Steps {
			if s.Kind == stepTool && s.Pending && s.Result == "" {
				s.Pending = false
				if s.Summary == "" {
					s.Summary = "(no output)"
				}
			}
		}
	}
	counts := map[string]int{}
	for _, s := range b.Steps {
		switch s.Kind {
		case stepThinking:
			b.ThinkingLines += strings.Count(strings.TrimRight(s.Text, "\n"), "\n") + 1
		case stepTool:
			b.ToolCount++
			counts[s.Name]++
		}
	}
	for name, n := range counts {
		b.Tools = append(b.Tools, ToolCount{Name: name, Count: n})
	}
	if b.EndTime.IsZero() {
		b.EndTime = b.StartTime
	}
}

func toBlock(v any) Block {
	raw, _ := json.Marshal(v)
	var b Block
	_ = json.Unmarshal(raw, &b)
	return b
}

func lastActivityBlock(blocks []Block) Block {
	for i := len(blocks) - 1; i >= 0; i-- {
		if t, _ := blocks[i]["type"].(string); t == blockActivity {
			return blocks[i]
		}
	}
	return nil
}

func setActEnd(b Block, t time.Time) { b["end_time"] = t }

func setActTokens(b Block, in, out int) {
	b["in_tokens"] = in
	b["out_tokens"] = out
}

func renderMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(text), &buf); err != nil {
		return template.HTMLEscapeString(text)
	}
	return strings.TrimSpace(buf.String())
}

func prettyJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return raw
	}
	return strings.TrimRight(buf.String(), "\n")
}

func toolSummary(name, args, result string) string {
	var parsed map[string]any
	if args != "" {
		_ = json.Unmarshal([]byte(args), &parsed)
	}
	str := func(k string) string {
		v, _ := parsed[k].(string)
		return v
	}
	switch name {
	case "write":
		path := str("file_path")
		if path == "" {
			path = str("path")
		}
		if path != "" {
			return fmt.Sprintf("Wrote %d lines to %s", strings.Count(str("content"), "\n")+1, path)
		}
	case "edit":
		path := str("file_path")
		if path == "" {
			path = str("path")
		}
		if path != "" {
			return fmt.Sprintf("Added %d lines, removed %d lines",
				strings.Count(str("new_string"), "\n")+1,
				strings.Count(str("old_string"), "\n")+1)
		}
	}
	first := strings.TrimSpace(strings.Split(strings.TrimSpace(result), "\n")[0])
	if first == "" {
		return "(no output)"
	}
	return firstLine(first, 72)
}

func firstLine(s string, max int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
