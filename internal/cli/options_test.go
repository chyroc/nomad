package cli

import (
	"fmt"
	"testing"
)

func TestParseOptions(t *testing.T) {
	cases := []struct {
		name  string
		argv  []string
		check func(o Options) error
	}{
		{
			"print with prompt", []string{"-p", "hello"},
			func(o Options) error {
				if !o.Print || len(o.PromptArgs) == 0 || o.PromptArgs[0] != "hello" {
					return errf("print/prompt wrong: %+v", o)
				}
				return nil
			},
		},
		{
			"bare prompt implies print", []string{"do something"},
			func(o Options) error {
				if !o.Print {
					return errf("bare prompt should imply print")
				}
				return nil
			},
		},
		{
			"standard flags",
			[]string{"--output-format", "stream-json", "--model", "m1", "--resume", "sesn-1",
				"--permission-mode", "acceptEdits", "--max-turns", "3", "--dangerously-skip-permissions",
				"--allowed-tools", "bash,read", "--image", "a.png"},
			func(o Options) error {
				if o.OutputFormat != FormatStreamJSON || o.Model != "m1" || o.Resume != "sesn-1" ||
					o.PermissionMode != "bypassPermissions" || o.MaxTurns != 3 ||
					len(o.AllowedTools) != 2 || len(o.Images) != 1 {
					return errf("parsed wrong: %+v", o)
				}
				return nil
			},
		},
		{
			"equals form and continue", []string{"--session-id=x", "-c"},
			func(o Options) error {
				if o.SessionID != "x" || !o.Continue {
					return errf("equals/continue wrong: %+v", o)
				}
				return nil
			},
		},
		{
			"login subcommand", []string{"login"},
			func(o Options) error {
				if o.Subcommand != "login" {
					return errf("subcommand = %q", o.Subcommand)
				}
				return nil
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, err := ParseOptions(tc.argv)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.check(o); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestParseOptions_Invalid(t *testing.T) {
	for _, argv := range [][]string{
		{"--output-format", "yaml"},
		{"--permission-mode", "weird"},
		{"--resume", "x", "-c"},
		{"--unknown-flag", "v"},
		{"-p", "hi", "--output-format", "stream-json"},
	} {
		if _, err := ParseOptions(argv); err == nil {
			t.Fatalf("expected error for %v", argv)
		}
	}
}

func TestParseOptions_StreamJSONVerbose(t *testing.T) {
	if _, err := ParseOptions([]string{"-p", "hi", "--output-format", "stream-json", "--verbose"}); err != nil {
		t.Fatalf("stream-json with --verbose must parse: %v", err)
	}
}

func errf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}
