package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStripFirstLineMargin(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello", "hello"},
		{"\x1b[38;5;252m\x1b[0m  hi", "\x1b[38;5;252m\x1b[0mhi"},
		{"\n\x1b[0m  body", "\n\x1b[0mbody"},
		{"nolead", "nolead"},
	}
	for _, c := range cases {
		if got := stripFirstLineMargin(c.in); got != c.want {
			t.Fatalf("stripFirstLineMargin(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestAnswerFirstLineAlignsWithContinuation(t *testing.T) {
	a := &App{out: &strings.Builder{}, color: true, bar: newStatusBar(), model: "doubao-x"}
	a.paths.Workspace = t.TempDir()
	a.renderAnswer("你好。\n\n第二段，比较长用来触发自动换行后的续行对齐。")
	out := a.out.(*strings.Builder).String()
	plain := ansi.Strip(out)
	first := strings.Split(plain, "\n")[0]
	if !strings.HasPrefix(first, "● 你好") || strings.HasPrefix(first, "●  ") {
		t.Fatalf("first answer line should be '● text' with one gap: %q", first)
	}
	for _, l := range strings.Split(plain, "\n") {
		if l == "" || !strings.HasPrefix(l, "  ") {
			if l != "" && !strings.HasPrefix(l, "●") {
				t.Fatalf("continuation line should keep 2-col indent: %q", l)
			}
		}
	}
}
