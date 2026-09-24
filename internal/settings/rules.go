package settings

import (
	"encoding/json"
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
// characters (* ? [) use wildcard matching over the full command; a
// pattern without meta does prefix matching.
func commandMatches(pattern, command string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return strings.HasPrefix(command, pattern)
	}
	return globMatch(pattern, command)
}

// globMatch implements shell-style wildcards where * matches any
// sequence (including spaces), ? one rune and [abc] one of a set.
func globMatch(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	starP, starT := -1, 0
	i, j := 0, 0
	for j < len(t) {
		switch {
		case i < len(p) && (p[i] == t[j] || p[i] == '?'):
			i++
			j++
		case i < len(p) && p[i] == '[':
			if end, ok := matchClass(p, i, t[j]); ok {
				i = end
				j++
			} else if starP >= 0 {
				i = starP + 1
				starT++
				j = starT
			} else {
				return false
			}
		case i < len(p) && p[i] == '*':
			starP = i
			starT = j
			i++
		case starP >= 0:
			i = starP + 1
			starT++
			j = starT
		default:
			return false
		}
	}
	for i < len(p) && p[i] == '*' {
		i++
	}
	return i == len(p)
}

func matchClass(p []rune, start int, c rune) (int, bool) {
	k := start + 1
	negated := false
	if k < len(p) && (p[k] == '!' || p[k] == '^') {
		negated = true
		k++
	}
	matched := false
	for k < len(p) && p[k] != ']' {
		if k+2 < len(p) && p[k+1] == '-' && p[k+2] != ']' {
			if c >= p[k] && c <= p[k+2] {
				matched = true
			}
			k += 3
			continue
		}
		if p[k] == c {
			matched = true
		}
		k++
	}
	if k >= len(p) {
		return start, false
	}
	return k + 1, matched != negated
}
