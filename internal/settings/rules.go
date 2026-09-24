package settings

import (
	"encoding/json"
	"path"
	"strings"
)

// Rule is a bare tool rule (read) or a bash pattern rule
// (bash(git commit*)).
type Rule struct {
	Tool    string
	Pattern string
	Bare    bool
	Raw     string
}

// String returns the canonical rule text.
func (r Rule) String() string {
	if r.Bare {
		return r.Tool
	}
	return r.Tool + "(" + r.Pattern + ")"
}

// ParseRule parses "read" or "bash(git commit*)".
func ParseRule(s string) (Rule, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, false
	}
	open := strings.IndexByte(s, '(')
	if open < 0 {
		if !validToolName(s) {
			return Rule{}, false
		}
		return Rule{Tool: s, Bare: true, Raw: s}, true
	}
	if !strings.HasSuffix(s, ")") || open == 0 {
		return Rule{}, false
	}
	tool := s[:open]
	pattern := s[open+1 : len(s)-1]
	if !validToolName(tool) || strings.TrimSpace(pattern) == "" {
		return Rule{}, false
	}
	return Rule{Tool: tool, Pattern: pattern, Raw: s}, true
}

func validToolName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// MatchAny reports whether any rule matches the given tool call.
func MatchAny(rules []Rule, name string, input json.RawMessage) (Rule, bool) {
	for _, r := range rules {
		if r.Match(name, input) {
			return r, true
		}
	}
	return Rule{}, false
}

// Match reports whether this rule matches the tool call.
func (r Rule) Match(name string, input json.RawMessage) bool {
	if r.Tool != name {
		return false
	}
	if r.Bare {
		return true
	}
	var args struct {
		Command string `json:"command"`
		Cmd     string `json:"cmd"`
	}
	_ = json.Unmarshal(input, &args)
	command := strings.TrimSpace(firstNonEmpty(args.Command, args.Cmd))
	return commandMatches(r.Pattern, command)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// commandMatches matches commands against the rule pattern: glob meta
// characters use path.Match over the full command; a pattern without
// meta does prefix matching.
func commandMatches(pattern, command string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return strings.HasPrefix(command, pattern)
	}
	ok, _ := path.Match(pattern, command)
	return ok
}
