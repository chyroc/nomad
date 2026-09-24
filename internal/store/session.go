// Package store persists local session transcripts as JSONL.
//
// Conversation state for the ma backend lives on the server; the local
// file is a rendering/audit record and backs the /sessions listing.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/loop"
)

// SessionStore appends events to one JSONL file per local session.
type SessionStore struct {
	dir string
}

// NewSessionStore creates the store rooted at dir.
func NewSessionStore(dir string) (*SessionStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: create sessions dir: %w", err)
	}
	return &SessionStore{dir: dir}, nil
}

// Append writes one event to a session file, creating it if needed.
func (s *SessionStore) Append(id string, ev loop.Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	f, err := os.OpenFile(filepath.Join(s.dir, id+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("store: open session: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(ev); err != nil {
		return fmt.Errorf("store: write event: %w", err)
	}
	return nil
}

// Load reads all events of a session.
func (s *SessionStore) Load(id string) ([]loop.Event, error) {
	f, err := os.Open(filepath.Join(s.dir, s.safe(id)))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []loop.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev loop.Event
		if json.Unmarshal([]byte(line), &ev) == nil {
			events = append(events, ev)
		}
	}
	return events, sc.Err()
}

// SessionRecord is a list entry.
type SessionRecord struct {
	ID        string
	Title     string
	UpdatedAt time.Time
}

// List returns existing transcripts, newest first.
func (s *SessionStore) List() ([]SessionRecord, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var out []SessionRecord
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		info, err := e.Info()
		if err != nil {
			continue
		}
		title := ""
		if evs, err := s.Load(id); err == nil {
			for _, ev := range evs {
				if ev.Kind == loop.EvUserMessage {
					title = truncateTitle(ev.Content)
					break
				}
			}
		}
		out = append(out, SessionRecord{ID: id, Title: title, UpdatedAt: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Resolve resolves an id prefix against stored sessions.
func (s *SessionStore) Resolve(prefix string) (string, error) {
	records, err := s.List()
	if err != nil {
		return "", err
	}
	var match string
	for _, r := range records {
		if strings.HasPrefix(r.ID, prefix) {
			if match != "" {
				return "", fmt.Errorf("ambiguous session prefix: %s", prefix)
			}
			match = r.ID
		}
	}
	if match == "" {
		return "", fmt.Errorf("no session found with prefix %s", prefix)
	}
	return match, nil
}

// Export writes the full JSONL transcript of a session to w.
func (s *SessionStore) Export(id string, w io.Writer) error {
	events, err := s.Load(id)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

// ExportMarkdown renders the transcript of a session as readable
// Markdown: user/assistant turns, tool calls and results, usage.
func (s *SessionStore) ExportMarkdown(id string, w io.Writer) error {
	events, err := s.Load(id)
	if err != nil {
		return err
	}
	mw := io.Writer(w)
	fmt.Fprintf(mw, "# Session %s\n\n", id)
	for _, ev := range events {
		switch ev.Kind {
		case loop.EvUserMessage:
			writeMarkdownSection(mw, "User", ev.Content)
		case loop.EvAssistantMessage:
			writeMarkdownSection(mw, "Assistant", ev.Content)
		case loop.EvAssistantThinking:
			fmt.Fprintf(mw, "## Thinking\n\n%s\n\n", strings.TrimSpace(ev.Content))
		case loop.EvToolCall:
			if ev.ToolCall != nil {
				fmt.Fprintf(mw, "## Tool: %s\n\n", ev.ToolCall.Name)
				if args := strings.TrimSpace(ev.ToolCall.Arguments); args != "" && args != "{}" {
					fmt.Fprintf(mw, "```json\n%s\n```\n\n", args)
				}
			}
		case loop.EvToolResult:
			status := "ok"
			if ev.IsError {
				status = "error"
			}
			fmt.Fprintf(mw, "Result (%s):\n\n```\n%s\n```\n\n", status, strings.TrimSpace(ev.Result))
		case loop.EvTurnEnd:
			if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
				fmt.Fprintf(mw, "_%d tokens (in %d, out %d)_\n\n",
					ev.Usage.InputTokens+ev.Usage.OutputTokens,
					ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
		case loop.EvError:
			fmt.Fprintf(mw, "## Error\n\n%s\n\n", strings.TrimSpace(ev.Content))
		}
	}
	return nil
}

func writeMarkdownSection(w io.Writer, title, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	fmt.Fprintf(w, "## %s\n\n%s\n\n", title, content)
}

func (s *SessionStore) safe(id string) string {
	return filepath.Base(id) + ".jsonl"
}

func truncateTitle(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}

// ErrNotFound is returned for missing transcripts.
var ErrNotFound = errors.New("session transcript not found")
