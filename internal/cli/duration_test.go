package cli

import (
	"testing"
	"time"
)

func TestFormatTurnDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{400 * time.Millisecond, "400ms"},
		{900 * time.Millisecond, "900ms"},
		{1500 * time.Millisecond, "2s"},
		{12 * time.Second, "12s"},
		{65 * time.Second, "1m05s"},
		{3*time.Minute + 2*time.Second, "3m02s"},
	}
	for _, c := range cases {
		if got := formatTurnDuration(c.d); got != c.want {
			t.Errorf("formatTurnDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestElapsedTurnZero(t *testing.T) {
	a := &App{}
	if d := a.elapsedTurn(); d != 0 {
		t.Errorf("zero turnStart elapsed = %v, want 0", d)
	}
}
