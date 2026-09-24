package cli

import "testing"

func TestAlwaysAllowRuleFileTool(t *testing.T) {
	a := &App{}
	r, ok := a.alwaysAllowRule("write", `{}`)
	if !ok || r.String() != "write" {
		t.Fatalf("want bare write rule, got %q ok=%v", r.String(), ok)
	}
}

func TestAlwaysAllowRuleBash(t *testing.T) {
	a := &App{}
	cases := []struct {
		args string
		want string
	}{
		{`{"command":"git status --short"}`, "bash(git status*)"},
		{`{"command":"ls"}`, "bash(ls*)"},
		{`{"cmd":"npm run build"}`, "bash(npm run*)"},
	}
	for _, c := range cases {
		r, ok := a.alwaysAllowRule("bash", c.args)
		if !ok || r.String() != c.want {
			t.Errorf("alwaysAllowRule bash(%s) = %q ok=%v, want %q", c.args, r.String(), ok, c.want)
		}
	}
}

func TestAlwaysAllowRuleBashMissing(t *testing.T) {
	a := &App{}
	if _, ok := a.alwaysAllowRule("bash", `{"command":"   "}`); ok {
		t.Fatal("empty command should not produce a rule")
	}
	if _, ok := a.alwaysAllowRule("bash", `{bad`); ok {
		t.Fatal("malformed args should not produce a rule")
	}
}

func TestSplit2(t *testing.T) {
	cases := []struct{ in, sub, rest string }{
		{"list", "list", ""},
		{"allow bash(git*)", "allow", "bash(git*)"},
		{"  deny  rm  ", "deny", "rm"},
		{"", "", ""},
	}
	for _, c := range cases {
		sub, rest := split2(c.in)
		if sub != c.sub || rest != c.rest {
			t.Errorf("split2(%q) = %q,%q want %q,%q", c.in, sub, rest, c.sub, c.rest)
		}
	}
}
