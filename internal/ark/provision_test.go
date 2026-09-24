package ark

import (
	"strings"
	"testing"
)

func TestCodingSystemPromptSections(t *testing.T) {
	sections := []string{
		"# Doing tasks",
		"# Executing actions with care",
		"# Using your tools",
		"# Tone and style",
		"# Output efficiency",
	}
	prev := -1
	for _, s := range sections {
		idx := strings.Index(codingSystemPrompt, s)
		if idx < 0 {
			t.Errorf("missing section %q", s)
			continue
		}
		if idx <= prev {
			t.Errorf("section %q out of order (idx=%d prev=%d)", s, idx, prev)
		}
		prev = idx
	}
}

func TestCodingSystemPromptCoversTools(t *testing.T) {
	for _, name := range []string{"bash", "read", "write", "edit", "glob", "grep"} {
		if !strings.Contains(codingSystemPrompt, name) {
			t.Errorf("system prompt does not document tool %q", name)
		}
	}
}
