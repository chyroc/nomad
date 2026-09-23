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

func TestSlashComplete(t *testing.T) {
	complete := func(line string) []string {
		cmds := []string{"/help", "/clear", "/model", "/memory", "/resume"}
		var out []string
		for _, c := range cmds {
			if len(line) > 0 && c[:len(line)] == line {
				out = append(out, c)
			}
		}
		return out
	}
	cases := []struct {
		line string
		want string
	}{
		{"/mod", "/model"},
		{"/mo", "/model"},
		{"/m", ""},
		{"/re", "/resume"},
		{"/x", ""},
		{"/model x", ""},
	}
	for _, c := range cases {
		if got := slashComplete(complete, c.line); got != c.want {
			t.Errorf("slashComplete(%q)=%q want %q", c.line, got, c.want)
		}
	}
}

func TestWordMotion(t *testing.T) {
	buf := []rune("hello world 测试")
	if got := wordLeft(buf, 5); got != 0 {
		t.Errorf("wordLeft(5)=%d want 0", got)
	}
	if got := wordLeft(buf, 11); got != 6 {
		t.Errorf("wordLeft(11)=%d want 6", got)
	}
	if got := wordRight(buf, 11); got != 13 {
		t.Errorf("wordRight(11)=%d want 13 (space before first CJK char)", got)
	}
	if got := wordRight(buf, 12); got != 13 {
		t.Errorf("wordRight(12)=%d want 13 (one CJK char)", got)
	}
	if got := wordRight(buf, 0); got != 5 {
		t.Errorf("wordRight(0)=%d want 5", got)
	}
}
