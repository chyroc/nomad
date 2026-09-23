package ark

import "testing"

func TestResolveEffort(t *testing.T) {
	if got := ResolveEffort("max", []string{"low", "medium", "high"}); got != "high" {
		t.Fatalf("doubao max -> %s, want high", got)
	}
	if got := ResolveEffort("max", []string{"low", "high", "max"}); got != "max" {
		t.Fatalf("deepseek max -> %s, want max", got)
	}
	if got := ResolveEffort("low", []string{"low", "high"}); got != "low" {
		t.Fatalf("explicit low -> %s", got)
	}
	if got := ResolveEffort("medium", []string{"none", "high", "max"}); got != "max" {
		t.Fatalf("unsupported medium falls back to max -> %s", got)
	}
}
