// Package config resolves local file locations and runtime options.
//
// Nomad has a single backend: the remote Volcengine Ark managed-agents
// service. Credentials and provisioned agent/environment ids are created
// on first run and cached under the data directory; normal usage needs no
// environment variables. Provider domains are intentionally absent here —
// they live in the ark package.
package config

import (
	"os"
	"path/filepath"
)

// Paths groups on-disk locations.
type Paths struct {
	Home      string
	DataDir   string
	Workspace string
}

// DefaultPaths resolves ~/.nomad and the current working directory.
func DefaultPaths() Paths {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return Paths{
		Home:      home,
		DataDir:   filepath.Join(home, ".nomad"),
		Workspace: cwd,
	}
}

// AuthFile is the OAuth/API-key credential cache.
func (p Paths) AuthFile() string { return filepath.Join(p.DataDir, "auth.json") }

// ProfileFile stores the auto-provisioned agent/environment ids.
func (p Paths) ProfileFile() string { return filepath.Join(p.DataDir, "profile.json") }

// SessionsDir holds local JSONL transcripts.
func (p Paths) SessionsDir() string { return filepath.Join(p.DataDir, "sessions") }

// SkillsDir holds user-defined skills.
func (p Paths) SkillsDir() string { return filepath.Join(p.DataDir, "skills") }

// MemoryFile is the global user instruction file.
func (p Paths) MemoryFile() string { return filepath.Join(p.DataDir, "NOMAD.md") }

func (p Paths) ModelsCache() string { return filepath.Join(p.DataDir, "models.json") }

func (p Paths) Ensure() error {
	for _, d := range []string{p.DataDir, p.SessionsDir(), p.SkillsDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}
