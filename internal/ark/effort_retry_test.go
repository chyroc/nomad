package ark

import (
	"errors"
	"testing"
)

func TestIsReasoningEffortError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("Error code: 400 - {\"message\":\"model.reasoning_effort is not supported\"}"), true},
		{errors.New("Error code: 400 - InvalidParameter: model.id bad"), false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isReasoningEffortError(c.err); got != c.want {
			t.Errorf("isReasoningEffortError(%v)=%v want %v", c.err, got, c.want)
		}
	}
}
