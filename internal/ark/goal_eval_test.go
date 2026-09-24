package ark

import (
	"testing"
)

func TestParseGoalVerdict(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		met, imposs bool
		err         bool
	}{
		{"plain", `{"met":true,"reason":"tests green"}`, true, false, false},
		{"fenced", "```json\n{\"met\":false,\"reason\":\"missing\"}\n```", false, false, false},
		{"surrounded", `noise {"met": false, "impossible": true, "reason": "x"} trailing`, false, true, false},
		{"ok-alias", `{"ok":true,"reason":"yes"}`, true, false, false},
		{"non-json", `I cannot judge`, false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parseGoalVerdict(tc.raw)
			if tc.err {
				if err == nil {
					t.Fatal("expected parse error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			v := GoalVerdict{Met: p.Met, Impossible: p.Impossible, Reason: p.Reason}
			if p.OK != nil {
				v.Met = *p.OK
			}
			if v.Impossible {
				v.Met = false
			}
			if v.Met != tc.met || v.Impossible != tc.imposs {
				t.Fatalf("got met=%v imposs=%v, want %v/%v", v.Met, v.Impossible, tc.met, tc.imposs)
			}
		})
	}
}

func TestParseGoalVerdictImpossibleForcesUnmet(t *testing.T) {
	p, err := parseGoalVerdict(`{"met":true,"impossible":true,"reason":"x"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Impossible {
		t.Fatal("impossible flag lost")
	}
}
