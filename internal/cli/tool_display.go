package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// toolInvocation renders a tool call argument JSON the way Claude Code
// does: the salient positional value as a short parenthetical, e.g.
//
//	bash("echo hi")
//	read(path/to/file)
//	write(path/to/file)
//	edit(path/to/file)
//	grep(pattern in dir)
//
// Returns an already-styled string including the enclosing parens, or
// "" when there are no meaningful arguments.
func toolInvocation(name, argumentsJSON string, maxWidth int) string {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(argumentsJSON)), &m); err != nil || len(m) == 0 {
		return ""
	}
	var s string
	switch name {
	case "bash":
		s = fmt.Sprintf("(%s)", strArg(m, "command"))
	case "read":
		s = fmt.Sprintf("(%s)", firstStr(m, "file_path", "path"))
	case "write", "edit":
		s = fmt.Sprintf("(%s)", firstStr(m, "file_path", "path"))
	case "glob":
		s = fmt.Sprintf("(%s)", firstStr(m, "pattern", "path"))
	case "grep":
		p := firstStr(m, "pattern", "query")
		dir := firstStr(m, "path", "glob")
		if dir != "" {
			s = fmt.Sprintf("(%s in %s)", p, dir)
		} else {
			s = fmt.Sprintf("(%s)", p)
		}
	default:
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		if len(keys) == 0 {
			return ""
		}
		s = "(" + oneLine(argumentsJSON, maxWidth-4) + ")"
	}
	s = strings.TrimSpace(s)
	if s == "()" {
		return ""
	}
	r := []rune(s)
	if len(r) > maxWidth {
		s = string(r[:maxWidth-1]) + "…" + ")"
	}
	return cDim + s + cReset
}

func strArg(m map[string]interface{}, key string) string {
	return oneLine(fmt.Sprintf("%v", m[key]), 120)
}

func firstStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}
