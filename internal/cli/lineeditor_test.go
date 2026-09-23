package cli

import "testing"

func TestVisiblePromptWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"> ", 2},
		{"\x1b[1m> \x1b[0m", 2},
		{"\x1b[36mhello\x1b[0m", 5},
		{"/model", 6},
		{"你好", 4},
		{"\x1b[1m你好\x1b[0m", 4},
		{"\x1b[1m", 0},
	}
	for _, c := range cases {
		if got := visiblePromptWidth(c.s); got != c.want {
			t.Errorf("width(%q)=%d want %d", c.s, got, c.want)
		}
	}
}

func TestIndexAtWidth(t *testing.T) {
	if idx, w := indexAtWidth("你好x", 2); idx != 1 || w != 2 {
		t.Fatalf("idx=%d w=%d, want 1,2", idx, w)
	}
	if idx, w := indexAtWidth("abc", 2); idx != 2 || w != 2 {
		t.Fatalf("idx=%d w=%d", idx, w)
	}
}
