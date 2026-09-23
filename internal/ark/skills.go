package ark

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// SkillBinding records one locally-owned skill registered with MA.
type SkillBinding struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SkillID     string `json:"skill_id"`
	Version     string `json:"version,omitempty"`
	LocalDir    string `json:"local_dir"`
}

// stubSkillZip builds a minimal skill package containing ONLY the
// name/description frontmatter. The instruction body is deliberately not
// uploaded: the platform only needs the metadata so the agent can discover
// and trigger the skill; execution reads the real SKILL.md locally.
func stubSkillZip(name, description string) ([]byte, string, error) {
	name = sanitizeSkillName(name)
	var fm bytes.Buffer
	fm.WriteString("---\n")
	fmt.Fprintf(&fm, "name: %s\n", name)
	if description != "" {
		fmt.Fprintf(&fm, "description: %s\n", strings.ReplaceAll(description, "\n", " "))
	}
	fm.WriteString("---\n\n")
	fmt.Fprintf(&fm, "Skill content is provided locally by nomad.\n")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name + "/SKILL.md")
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(w, &fm); err != nil {
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), name + ".zip", nil
}

// RegisterSkill uploads the name/description stub and returns the binding
// (with the platform skill id). No instruction body leaves the machine.
// If a custom skill with the same name already exists it is reused.
func (c *Client) RegisterSkill(ctx context.Context, name, description string) (SkillBinding, error) {
	if id, version, found := c.findSkillByName(ctx, name); found {
		return SkillBinding{Name: name, Description: description, SkillID: id, Version: firstNonEmpty(version, "1")}, nil
	}
	zipBytes, fileName, err := stubSkillZip(name, description)
	if err != nil {
		return SkillBinding{}, err
	}
	created, err := c.rt.CreateSkill(ctx, bytes.NewReader(zipBytes), fileName, name)
	if err != nil {
		return SkillBinding{}, fmt.Errorf("register skill %s: %w", name, err)
	}
	b := SkillBinding{Name: name, Description: description, SkillID: created.ID, Version: "1"}
	if v := strings.TrimSpace(created.LatestVersion); v != "" {
		b.Version = v
	} else if got, err := c.rt.GetSkill(ctx, created.ID); err == nil && strings.TrimSpace(got.LatestVersion) != "" {
		b.Version = got.LatestVersion
	}
	return b, nil
}

// LinkLocalSkill symlinks the local skill bundle into the workspace under
// .nomad/skills/<name> so that when MA triggers the skill and the agent
// reads its files, they resolve to the real local content (no copy).
func LinkLocalSkill(workspace, name, localDir string) error {
	linkRoot := filepath.Join(workspace, ".nomad", "skills")
	if err := os.MkdirAll(linkRoot, 0o755); err != nil {
		return err
	}
	link := filepath.Join(linkRoot, sanitizeSkillName(name))
	_ = os.Remove(link)
	absLocal, err := filepath.Abs(localDir)
	if err != nil {
		absLocal = localDir
	}
	return os.Symlink(absLocal, link)
}

// SkillLinkDir returns the workspace directory skills are linked into.
func SkillLinkDir(workspace string) string {
	return filepath.Join(workspace, ".nomad", "skills")
}

// BindSkillsToAgent attaches the registered skill ids to the coding agent.
//
// The array-replacement semantics of the MA agent API require sending the
// full current skill set, so callers pass the complete desired list. Each
// entry is a bare custom skill id ("skill-..."), which the control plane
// treats as a custom TOP skill bound to its latest version.
func (c *Client) BindSkillsToAgent(ctx context.Context, agentID string, bindings []SkillBinding) error {
	var cur struct {
		Version int `json:"version"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/agents/"+agentID, nil, &cur); err != nil {
		return fmt.Errorf("get agent version: %w", err)
	}
	skills := make([]map[string]string, 0, len(bindings))
	for _, b := range bindings {
		skills = append(skills, map[string]string{
			"skill_id": b.SkillID,
			"type":     "custom",
		})
	}
	body := map[string]interface{}{
		"version": cur.Version,
		"skills":  skills,
	}
	var raw json.RawMessage
	return c.doJSON(ctx, http.MethodPost, "/agents/"+agentID, body, &raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// findSkillByName returns the id/version of an existing custom skill.
func (c *Client) findSkillByName(ctx context.Context, name string) (id, version string, found bool) {
	var list struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Source        string `json:"source"`
			LatestVersion string `json:"latest_version"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/skills?limit=100", nil, &list); err != nil {
		return "", "", false
	}
	for _, s := range list.Data {
		if s.Name == name {
			return s.ID, s.LatestVersion, true
		}
	}
	return "", "", false
}

// sanitizeSkillName normalizes a directory/skill name for upload paths.
func sanitizeSkillName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.' || r == '/':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "skill"
	}
	return out
}
