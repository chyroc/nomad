package cli

import (
	"strings"
	"testing"
)

func TestScrollWindow(t *testing.T) {
	cases := []struct {
		name                         string
		total, shown, sel, top, want int
	}{
		{"all visible resets to top", 12, 12, 0, 5, 0},
		{"all visible any sel", 12, 12, 11, 5, 0},
		{"short window selected at top stays", 12, 7, 0, 0, 0},
		{"short window selected within window keeps top", 12, 7, 4, 2, 2},
		{"short window select past bottom scrolls down", 12, 7, 9, 0, 3},
		{"short window select last item scrolls to end", 12, 7, 11, 0, 5},
		{"scroll back up when selection moves up", 12, 7, 3, 8, 3},
		{"stays clamped at end", 12, 7, 10, 5, 5},
		{"single visible window", 12, 1, 11, 0, 11},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scrollWindow(c.total, c.shown, c.sel, c.top); got != c.want {
				t.Fatalf("scrollWindow(%d,%d,%d,top=%d)=%d want %d",
					c.total, c.shown, c.sel, c.top, got, c.want)
			}
		})
	}
}

func TestScrollWindowSelectedAlwaysVisible(t *testing.T) {
	for total := 1; total <= 20; total++ {
		for shown := 1; shown <= total; shown++ {
			top := 0
			for sel := 0; sel < total; sel++ {
				top = scrollWindow(total, shown, sel, top)
				if sel < top || sel >= top+shown {
					t.Fatalf("total=%d shown=%d sel=%d top=%d: selection not visible", total, shown, sel, top)
				}
				if top < 0 || top+shown > total {
					t.Fatalf("total=%d shown=%d sel=%d top=%d: window out of range", total, shown, sel, top)
				}
			}
		}
	}
}

func TestFilterPickItems(t *testing.T) {
	items := []pickItem{
		{id: "deepseek-v4-pro-260425", label: "deepseek-v4-pro", tag: "fmv-a"},
		{id: "glm-5-2-260617", label: "glm-5-2", tag: "fmv-b", desc: "chat model"},
		{id: "doubao-seed-2-1-pro-260915", label: "doubao-seed-2-1-pro", tag: "fmv-c"},
	}
	if got := filterPickItems(items, ""); len(got) != 3 {
		t.Fatalf("empty filter = %d, want all 3", len(got))
	}
	got := filterPickItems(items, "glm")
	if len(got) != 1 || got[0].label != "glm-5-2" {
		t.Fatalf("glm filter = %+v", got)
	}
	if got := filterPickItems(items, "fmv-b"); len(got) != 1 {
		t.Fatalf("id/tag substring filter = %d, want 1", len(got))
	}
	if got := filterPickItems(items, "CHAT"); len(got) != 1 {
		t.Fatalf("case-insensitive description match = %d, want 1", len(got))
	}
	if got := filterPickItems(items, "zzzz"); len(got) != 0 {
		t.Fatalf("no-match should be 0, got %d", len(got))
	}
}

func TestSlashHintsFilterAndDedup(t *testing.T) {
	complete := func(line string) []string {
		return []string{"/model", "/memory", "/mode"}
	}
	if got := slashHints(complete, "/"); len(got) != 3 {
		t.Fatalf("/ should hint 3, got %d: %v", len(got), got)
	}
	got := slashHints(complete, "/mod")
	if len(got) != 2 {
		t.Fatalf("/mod should match /model and /mode, got %d", len(got))
	}
	if got := slashHints(complete, "/model"); len(got) != 0 {
		t.Fatalf("exact match should not hint itself, got %v", got)
	}
	if got := slashHints(complete, "/model x"); got != nil {
		t.Fatalf("command with args must not show hints")
	}
	if got := slashHints(complete, "hello"); got != nil {
		t.Fatalf("non-slash input must not show hints")
	}
}

func TestSlashUnique(t *testing.T) {
	complete := func(line string) []string {
		switch strings.TrimSpace(line) {
		case "/mod":
			return []string{"/model"}
		case "/mo":
			return []string{"/model", "/mode"}
		}
		return nil
	}
	if got := slashUnique(complete, "/mod"); got != "/model" {
		t.Fatalf("unique /mod -> %q, want /model", got)
	}
	if got := slashUnique(complete, "/mo"); got != "" {
		t.Fatalf("ambiguous /mo must not auto-complete, got %q", got)
	}
}

func TestFuzzyScore(t *testing.T) {
	cases := []struct {
		pattern, target string
		wantMatch       bool
	}{
		{"dsp", "deepseek-v4-pro", true},
		{"pro", "deepseek-v4-pro", true},
		{"zzz", "deepseek-v4-pro", false},
		{"dp", "doubao-seed-2-1-pro", true},
	}
	for _, c := range cases {
		_, ok := fuzzyScore(c.pattern, c.target)
		if ok != c.wantMatch {
			t.Errorf("fuzzyScore(%q,%q) match=%v want %v", c.pattern, c.target, ok, c.wantMatch)
		}
	}
}

func TestFilterPickItemsRanks(t *testing.T) {
	items := []pickItem{
		{id: "x", label: "doubao-seed-evolving"},
		{id: "y", label: "doubao-seed-2-1-pro"},
		{id: "z", label: "deepseek-v4-pro"},
	}
	got := filterPickItems(items, "dvp")
	if len(got) != 1 || got[0].label != "deepseek-v4-pro" {
		t.Fatalf("dvp should match only deepseek-v4-pro, got %+v", got)
	}
	got = filterPickItems(items, "deepseek")
	if len(got) != 1 || got[0].label != "deepseek-v4-pro" {
		t.Fatalf("deepseek prefix should match one, got %+v", got)
	}
}
