package cli

import (
	"strings"
	"testing"
)

func TestToolInvocation(t *testing.T) {
	cases := []struct {
		name, args, wantContains string
	}{
		{"bash", `{"command":"echo hi"}`, "echo hi"},
		{"read", `{"file_path":"/a/b.go"}`, "/a/b.go"},
		{"write", `{"file_path":"x.txt","content":"y"}`, "x.txt"},
		{"grep", `{"pattern":"foo","path":"."}`, "foo in ."},
		{"glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"unknown", `{}`, ""},
	}
	for _, c := range cases {
		got := toolInvocation(c.name, c.args, 90)
		if c.wantContains == "" {
			if got != "" {
				t.Errorf("%s empty args should be %q got %q", c.name, "", got)
			}
			continue
		}
		if !strings.Contains(got, c.wantContains) {
			t.Errorf("%s invocation=%q should contain %q", c.name, got, c.wantContains)
		}
	}
}
