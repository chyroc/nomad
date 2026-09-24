package ark

import "testing"

func TestEffortBaseConfigLookup(t *testing.T) {
	cases := []struct {
		model  string
		wantOK bool
		def    string
	}{
		{"deepseek-v4-1-flash-260910", true, "high"},
		{"deepseek-v4-pro-ga-260813", true, "high"},
		{"doubao-seed-2-1-pro-260915", true, "high"},
		{"doubao-seed-2-0-mini-260428", true, "medium"},
		{"glm-5-3-flash-260828", true, "max"},
		{"glm-5-2-260617", true, "high"},
		{"some-unknown-model-x", false, ""},
	}
	for _, c := range cases {
		cfg := lookupEffortBaseConfig(c.model)
		if (cfg != nil) != c.wantOK {
			t.Errorf("%s: lookup ok=%v want %v", c.model, cfg != nil, c.wantOK)
			continue
		}
		if cfg != nil && cfg.defaultEffort != c.def {
			t.Errorf("%s: default=%s want %s", c.model, cfg.defaultEffort, c.def)
		}
	}
}

func TestEffortMapping(t *testing.T) {
	cases := []struct {
		model, requested, wire string
	}{
		// deepseek-v4-1-flash: no 'max' level; UI max stays max here because
		// the model does accept max per its own table, but ultra maps to it.
		{"deepseek-v4-1-flash-260910", "max", "high"},
		{"deepseek-v4-1-flash-260910", "xhigh", "high"},
		{"deepseek-v4-1-flash-260910", "minimal", "low"},
		{"deepseek-v4-1-flash-260910", "none", "none"},
		{"deepseek-v4-1-flash-260910", "weird", "high"},
		// doubao 2-1: max/xhigh fold to high; none -> minimal.
		{"doubao-seed-2-1-pro-260915", "max", "high"},
		{"doubao-seed-2-1-pro-260915", "none", "minimal"},
		// doubao 2-0 defaults medium, max -> high.
		{"doubao-seed-2-0-pro-260215", "max", "high"},
		{"doubao-seed-2-0-pro-260215", "", "medium"},
		// glm-5-3-flash always on.
		{"glm-5-3-flash-260828", "none", "low"},
		{"glm-5-3-flash-260828", "xhigh", "max"},
		// pro-ga: medium -> low, max -> xhigh.
		{"deepseek-v4-pro-ga-260813", "medium", "low"},
		{"deepseek-v4-pro-ga-260813", "max", "xhigh"},
	}
	for _, c := range cases {
		cfg := lookupEffortBaseConfig(c.model)
		if cfg == nil {
			t.Fatalf("no config for %s", c.model)
		}
		if got := cfg.mapEffort(c.requested); got != c.wire {
			t.Errorf("%s mapEffort(%q)=%q want %q", c.model, c.requested, got, c.wire)
		}
	}
}

func TestLongestPatternWins(t *testing.T) {
	// -ga variant must not be matched by a generic shorter entry.
	cfg := lookupEffortBaseConfig("deepseek-v4-pro-260425")
	if cfg == nil {
		t.Fatal("expected config")
	}
	if got := cfg.mapEffort("medium"); got != "high" {
		t.Fatalf("pro-260425 medium should fall back to default high, got %q", got)
	}
}

func TestEffortMenuSupportedFlags(t *testing.T) {
	for _, m := range []struct{ model, level string }{
		{"deepseek-v4-1-flash-260910", "low"},
		{"deepseek-v4-1-flash-260910", "high"},
		{"doubao-seed-2-1-pro-260915", "high"},
		{"glm-5-3-flash-260828", "max"},
	} {
		menu := EffortMenu(m.model)
		found := false
		for _, info := range menu {
			if info.Level == m.level {
				found = true
				if !info.Supported {
					t.Errorf("%s/%s should be directly supported", m.model, m.level)
				}
			}
		}
		if !found {
			t.Errorf("%s menu missing %s", m.model, m.level)
		}
	}
}
