package ark

import "testing"

func TestCollapseModelVersions(t *testing.T) {
	in := []ModelInfo{
		{Name: "doubao-seed-2-1-pro", Version: "260628", ID: "fmv-old", Primary: false, RuntimeID: "doubao-seed-2-1-pro-260628"},
		{Name: "glm-5-2", Version: "260617", ID: "fmv-glm", Primary: true, RuntimeID: "glm-5-2-260617"},
		{Name: "doubao-seed-2-1-pro", Version: "260915", ID: "fmv-new", Primary: true, RuntimeID: "doubao-seed-2-1-pro-260915"},
	}
	out := collapseModelVersions(in)
	want := []struct {
		name string
		rid  string
	}{
		{"doubao-seed-2-1-pro", "doubao-seed-2-1-pro-260915"},
		{"glm-5-2", "glm-5-2-260617"},
	}
	if len(out) != len(want) {
		t.Fatalf("got %d models: %+v", len(out), out)
	}
	for i, w := range want {
		if out[i].Name != w.name || out[i].RuntimeID != w.rid {
			t.Errorf("position %d: got %s %s, want %s %s", i, out[i].Name, out[i].RuntimeID, w.name, w.rid)
		}
	}
}

func TestCollapseModelVersionsHidesHardcodedNames(t *testing.T) {
	in := []ModelInfo{
		{Name: "deepseek-v4-pro", Version: "260425", ID: "a", Primary: true},
		{Name: "deepseek-v4-flash-ga", Version: "260731", ID: "b", Primary: true},
		{Name: "deepseek-v4-pro-ga", Version: "260813", ID: "c", Primary: true},
	}
	out := collapseModelVersions(in)
	if len(out) != 1 || out[0].Name != "deepseek-v4-pro-ga" {
		t.Fatalf("expected only deepseek-v4-pro-ga, got %+v", out)
	}
}

func TestCollapseModelVersionsNoPrimaryPicksNewest(t *testing.T) {
	in := []ModelInfo{
		{Name: "m", Version: "260101", ID: "a"},
		{Name: "m", Version: "261231", ID: "b"},
		{Name: "m", Version: "260606", ID: "c"},
	}
	out := collapseModelVersions(in)
	if len(out) != 1 || out[0].ID != "b" {
		t.Fatalf("expected newest version b, got %+v", out)
	}
}
