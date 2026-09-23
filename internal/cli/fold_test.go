package cli

import "testing"

func TestFoldLines(t *testing.T) {
	var short []string
	for i := 0; i < 5; i++ {
		short = append(short, "l")
	}
	if _, _, _, ok := foldLines("bash", joinLines(short)); ok {
		t.Fatal("short output must not fold")
	}
	var long []string
	for i := 0; i < 30; i++ {
		long = append(long, "line")
	}
	head, tail, more, ok := foldLines("bash", joinLines(long))
	if !ok {
		t.Fatal("long output must fold")
	}
	if len(head) != foldHead || len(tail) != foldTail || more != 30-foldHead-foldTail {
		t.Fatalf("head=%d tail=%d more=%d", len(head), len(tail), more)
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
