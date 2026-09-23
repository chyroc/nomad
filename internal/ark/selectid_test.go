package ark

import "testing"

func TestModelInfoSelectID(t *testing.T) {
	m := ModelInfo{
		ID:        "fmv-20260515172947-mgnmn",
		RuntimeID: "deepseek-v4-pro-260425",
		Name:      "deepseek-v4-pro",
		Version:   "260425",
	}
	if got := m.SelectID(); got != "deepseek-v4-pro-260425" {
		t.Fatalf("SelectID = %q, want runtime id (override accepts Name-Version, not fmv)", got)
	}

	fallback := ModelInfo{ID: "plain-id"}
	if got := fallback.SelectID(); got != "plain-id" {
		t.Fatalf("SelectID fallback = %q, want id", got)
	}

	derived := ModelInfo{ID: "fmv-x", Name: "glm-5-3-flash", Version: "260828"}
	if got := derived.SelectID(); got != "glm-5-3-flash-260828" {
		t.Fatalf("SelectID derived = %q, want name-version", got)
	}
}
