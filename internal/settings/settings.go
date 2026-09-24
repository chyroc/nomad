// Package settings loads and merges local permission and hook
// configuration from user and project settings files.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// File is one settings document.
type File struct {
	Permissions Permissions `json:"permissions"`
	Hooks       Hooks       `json:"hooks,omitempty"`
}

// Permissions groups allow/deny rules and an optional default mode.
type Permissions struct {
	Allow       []string `json:"allow,omitempty"`
	Deny        []string `json:"deny,omitempty"`
	DefaultMode string   `json:"defaultMode,omitempty"`
}

// SourcedFile pairs a loaded document with its provenance.
type SourcedFile struct {
	Path   string
	File   File
	Source string
}

// AllowStrings returns the raw allow rules of this document.
func (s *SourcedFile) AllowStrings() []string {
	if s == nil {
		return nil
	}
	return s.File.Permissions.Allow
}

// DenyStrings returns the raw deny rules of this document.
func (s *SourcedFile) DenyStrings() []string {
	if s == nil {
		return nil
	}
	return s.File.Permissions.Deny
}

// Mode returns the default permission mode of this document.
func (s *SourcedFile) Mode() string {
	if s == nil {
		return ""
	}
	return s.File.Permissions.DefaultMode
}

// Settings is the merged view over user, project and compat files.
type Settings struct {
	User        *SourcedFile
	Project     *SourcedFile
	Compat      []SourcedFile
	AllowRules  []Rule
	DenyRules   []Rule
	DefaultMode string
}

// Load reads the user and project settings files plus any read-only
// compat paths. Missing files are ignored; malformed files error with
// their path.
func Load(userPath, projectPath string, compatPaths ...string) (*Settings, error) {
	s := &Settings{}
	parse := func(path, source string) (*SourcedFile, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		var f File
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("settings: parse %s: %w", path, err)
		}
		return &SourcedFile{Path: path, File: f, Source: source}, nil
	}
	var err error
	if s.User, err = parse(userPath, "user"); err != nil {
		return nil, err
	}
	if s.Project, err = parse(projectPath, "project"); err != nil {
		return nil, err
	}
	for _, p := range compatPaths {
		f, err := parse(p, "compat")
		if err != nil {
			return nil, err
		}
		if f != nil {
			s.Compat = append(s.Compat, *f)
		}
	}

	addRules := func(dst *[]Rule, raw []string) {
		for _, r := range raw {
			if rule, ok := ParseRule(r); ok {
				*dst = append(*dst, rule)
			}
		}
	}
	addRules(&s.AllowRules, s.User.AllowStrings())
	addRules(&s.DenyRules, s.User.DenyStrings())
	addRules(&s.AllowRules, s.Project.AllowStrings())
	addRules(&s.DenyRules, s.Project.DenyStrings())
	s.DefaultMode = s.Project.Mode()
	for _, f := range s.Compat {
		f := f
		addRules(&s.AllowRules, f.AllowStrings())
		addRules(&s.DenyRules, f.DenyStrings())
	}
	if s.DefaultMode == "" {
		s.DefaultMode = s.User.Mode()
	}
	return s, nil
}

// SaveUserRules rewrites the user settings file with merged allow and
// deny rule strings, atomically with 0600 permissions.
func SaveUserRules(path string, allow, deny []string) error {
	f := File{Permissions: Permissions{Allow: allow, Deny: deny}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &f)
		f.Permissions.Allow = allow
		f.Permissions.Deny = deny
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
