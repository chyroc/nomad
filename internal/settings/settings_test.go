package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRule(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		bare bool
		tool string
		pat  string
	}{
		{"read", true, true, "read", ""},
		{"  bash ", true, true, "bash", ""},
		{"bash(git commit*)", true, false, "bash", "git commit*"},
		{"edit", true, true, "edit", ""},
		{"Bash", false, false, "", ""},
		{"bash(", false, false, "", ""},
		{"bash()", false, false, "", ""},
		{"(", false, false, "", ""},
		{"foo bar", false, false, "", ""},
	}
	for _, c := range cases {
		r, ok := ParseRule(c.in)
		if ok != c.ok {
			t.Errorf("ParseRule(%q) ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if r.Bare != c.bare || r.Tool != c.tool || r.Pattern != c.pat {
			t.Errorf("ParseRule(%q) = %+v", c.in, r)
		}
	}
}

func TestRuleMatchBare(t *testing.T) {
	r, _ := ParseRule("read")
	if !r.Match("read", json.RawMessage(`{}`)) {
		t.Error("bare read should match read tool")
	}
	if r.Match("write", json.RawMessage(`{}`)) {
		t.Error("bare read should not match write")
	}
}

func TestRuleMatchBashPrefix(t *testing.T) {
	r, _ := ParseRule("bash(git status)")
	cases := map[string]bool{
		"git status":         true,
		"git status --short": true,
		"git stash":          false,
		"echo git status":    false,
	}
	for cmd, want := range cases {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		if got := r.Match("bash", input); got != want {
			t.Errorf("bash(%q) match %q = %v want %v", r.Pattern, cmd, got, want)
		}
	}
}

func TestRuleMatchBashGlob(t *testing.T) {
	r, _ := ParseRule("bash(npm run *)")
	cases := map[string]bool{
		"npm run test":  true,
		"npm run build": true,
		"npm install":   false,
	}
	for cmd, want := range cases {
		input, _ := json.Marshal(map[string]string{"command": cmd})
		if got := r.Match("bash", input); got != want {
			t.Errorf("glob match %q = %v want %v", cmd, got, want)
		}
	}
}

func TestRuleMatchBashCmdAlias(t *testing.T) {
	r, _ := ParseRule("bash(ls)")
	input := json.RawMessage(`{"cmd":"ls -la"}`)
	if !r.Match("bash", input) {
		t.Error("cmd alias should be honored")
	}
}

func TestRuleMatchBashStar(t *testing.T) {
	r, _ := ParseRule("bash(*)")
	if !r.Match("bash", json.RawMessage(`{"command":"anything"}`)) {
		t.Error("bash(*) should match any command")
	}
}

func TestGlobMatchSpacesAndClasses(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"rm*", "rm -rf /tmp/x", true},
		{"npm run *", "npm run build", true},
		{"git *", "git push origin", true},
		{"git *", "hg push", false},
		{"test?", "tests", true},
		{"test?", "testing", false},
		{"[rg]m*", "rm -f", true},
		{"[rg]m*", "gm", true},
		{"[!rg]m*", "xm", true},
		{"[!rg]m*", "rm", false},
		{"exact", "exact", true},
		{"exact", "exactly", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.s); got != c.want {
			t.Errorf("globMatch(%q,%q)=%v want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestMatchAny(t *testing.T) {
	rules := []Rule{
		mustRule("read"),
		mustRule("bash(git*)"),
	}
	input, _ := json.Marshal(map[string]string{"command": "git push"})
	if _, ok := MatchAny(rules, "read", input); !ok {
		t.Error("read should match")
	}
	if _, ok := MatchAny(rules, "bash", input); !ok {
		t.Error("bash(git*) should match git push")
	}
	if _, ok := MatchAny(rules, "write", input); ok {
		t.Error("write should not match")
	}
}

func mustRule(s string) Rule {
	r, ok := ParseRule(s)
	if !ok {
		panic("bad rule " + s)
	}
	return r
}

func TestLoadMergeAndDefaultMode(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	project := filepath.Join(dir, "project.json")
	writeSettings(t, user, `{"permissions":{"allow":["read"],"deny":["bash(rm*)"],"defaultMode":"acceptEdits"}}`)
	writeSettings(t, project, `{"permissions":{"allow":["edit"],"defaultMode":"plan"}}`)

	s, err := Load(user, project)
	if err != nil {
		t.Fatal(err)
	}
	if s.DefaultMode != "plan" {
		t.Errorf("project defaultMode should win, got %q", s.DefaultMode)
	}
	if len(s.AllowRules) != 2 {
		t.Errorf("allow rules = %d, want 2", len(s.AllowRules))
	}
	if len(s.DenyRules) != 1 || s.DenyRules[0].Pattern != "rm*" {
		t.Errorf("deny rules wrong: %+v", s.DenyRules)
	}
}

func TestLoadUserDefaultModeFallback(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	writeSettings(t, user, `{"permissions":{"defaultMode":"default"}}`)
	s, err := Load(user, filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.DefaultMode != "default" {
		t.Errorf("got %q", s.DefaultMode)
	}
}

func TestLoadMalformed(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	writeSettings(t, bad, `{not json`)
	if _, err := Load(bad, filepath.Join(dir, "p.json")); err == nil {
		t.Fatal("malformed settings should error")
	}
}

func TestLoadIgnoresInvalidRules(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "u.json")
	writeSettings(t, user, `{"permissions":{"allow":["read","BAD NAME","bash(  )"]}}`)
	s, err := Load(user, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.AllowRules) != 1 {
		t.Fatalf("invalid rules should be skipped, got %+v", s.AllowRules)
	}
}

func TestSaveUserRulesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := SaveUserRules(path, []string{"read", "edit"}, []string{"bash(rm*)"}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.AllowRules) != 2 || len(s.DenyRules) != 1 {
		t.Fatalf("round-trip wrong: %+v", s)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "permissions") {
		t.Fatalf("missing permissions section:\n%s", data)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %o", info.Mode().Perm())
	}
}

func TestSaveUserRulesPreservesHooks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	writeSettings(t, path, `{"hooks":{"Stop":[{"command":"echo done"}]}}`)
	if err := SaveUserRules(path, []string{"read"}, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "echo done") {
		t.Fatalf("hooks not preserved:\n%s", data)
	}
}

func writeSettings(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
