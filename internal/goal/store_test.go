package goal

import (
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("missing")
	if err != nil || got != nil {
		t.Fatalf("absent goal = %v, %v", got, err)
	}
	st := New("sesn_1", "do the thing", time.Now())
	st.MarkChecked("not yet", time.Now())
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	got, err = s.Load("sesn_1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Condition != "do the thing" || got.Iterations != 1 || got.Status != StatusActive {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if err := s.Clear("sesn_1"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Load("sesn_1")
	if got != nil {
		t.Fatal("goal should be cleared")
	}
	if err := s.Clear("sesn_1"); err != nil {
		t.Fatalf("clearing an absent goal must not error: %v", err)
	}
}

func TestStoreSaveRequiresSession(t *testing.T) {
	s, _ := NewStore(t.TempDir())
	if err := s.Save(&State{Condition: "x"}); err == nil {
		t.Fatal("save without session id must fail")
	}
}
