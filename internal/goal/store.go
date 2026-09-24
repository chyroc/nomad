package goal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store persists one State JSON file per session id.
type Store struct {
	dir string
}

// NewStore creates the goal store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("goal store: create dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Load returns nil state (and no error) when the session has no goal.
func (s *Store) Load(sessionID string) (*State, error) {
	data, err := os.ReadFile(s.path(sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("goal store: read: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("goal store: decode: %w", err)
	}
	return &st, nil
}

// Save writes the state atomically.
func (s *Store) Save(st *State) error {
	if st == nil || st.SessionID == "" {
		return fmt.Errorf("goal store: missing session id")
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(st.SessionID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("goal store: write: %w", err)
	}
	if err := os.Rename(tmp, s.path(st.SessionID)); err != nil {
		return fmt.Errorf("goal store: rename: %w", err)
	}
	return nil
}

// Clear removes the goal file; absent file is not an error.
func (s *Store) Clear(sessionID string) error {
	err := os.Remove(s.path(sessionID))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("goal store: remove: %w", err)
	}
	return nil
}

func (s *Store) path(sessionID string) string {
	return filepath.Join(s.dir, strings.ReplaceAll(filepath.Base(sessionID), "/", "_")+".json")
}
