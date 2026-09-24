package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/config"
	"github.com/chyroc/nomad/internal/goal"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

func newGoalTestApp(t *testing.T, verdicts []ark.GoalVerdict) (*App, *store.SessionStore, *int32) {
	t.Helper()
	dir := t.TempDir()
	transcript, err := store.NewSessionStore(dir + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		paths: config.Paths{Home: dir, DataDir: dir, Workspace: dir},
		model: "model-x",
	}
	var b strings.Builder
	a.out = &b
	if err := a.initGoalStore(); err != nil {
		t.Fatal(err)
	}
	a.goalRetryDelay = func(int) time.Duration { return 0 }
	a.sessionID = "sesn_test"
	var calls int32
	a.goalEvalFunc = func(ctx context.Context, modelID, sys, user string) (ark.GoalVerdict, error) {
		n := atomic.AddInt32(&calls, 1) - 1
		if int(n) >= len(verdicts) {
			t.Fatalf("evaluator called %d times, only %d verdicts queued", n+1, len(verdicts))
		}
		if !strings.Contains(user, "GOAL-COND") {
			t.Errorf("evaluator user prompt missing condition: %q", user[:min(80, len(user))])
		}
		return verdicts[n], nil
	}
	return a, transcript, &calls
}

func seedGoal(t *testing.T, a *App, condition string) {
	t.Helper()
	st := goal.New(a.sessionID, condition, time.Now())
	if err := a.goalStore.Save(st); err != nil {
		t.Fatal(err)
	}
	a.goal = st
}

func seedTranscript(t *testing.T, transcript *store.SessionStore, id string) {
	t.Helper()
	for _, ev := range []loop.Event{
		{Kind: loop.EvUserMessage, Content: "GOAL-COND work"},
		{Kind: loop.EvAssistantMessage, Content: "started"},
	} {
		if err := transcript.Append(id, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func TestContinueGoalMetAfterOneContinuation(t *testing.T) {
	a, transcript, calls := newGoalTestApp(t, []ark.GoalVerdict{
		{Met: false, Reason: "tests still red"},
		{Met: true, Reason: "go test exits 0"},
	})
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	var turns int32
	runTurn := func(text string, _ []loop.Attachment) error {
		atomic.AddInt32(&turns, 1)
		if !strings.Contains(text, "tests still red") || !strings.Contains(text, "GOAL-COND") {
			t.Errorf("continuation prompt missing reason/condition: %q", text)
		}
		return transcript.Append(a.sessionID, loop.Event{Kind: loop.EvAssistantMessage, Content: "fixed"})
	}
	if err := a.continueGoal(context.Background(), transcript, runTurn, nil); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&turns) != 1 {
		t.Fatalf("want exactly 1 continuation turn, got %d", turns)
	}
	if atomic.LoadInt32(calls) != 2 {
		t.Fatalf("want 2 evaluations, got %d", *calls)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusMet || st.Iterations != 1 {
		t.Fatalf("final state = %+v", st)
	}
	if a.goal != nil {
		t.Fatal("in-memory goal must clear after terminal verdict")
	}
}

func TestContinueGoalImpossibleStopsImmediately(t *testing.T) {
	a, transcript, _ := newGoalTestApp(t, []ark.GoalVerdict{
		{Met: false, Impossible: true, Reason: "needs unavailable API"},
	})
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	var turns int32
	err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { atomic.AddInt32(&turns, 1); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if turns != 0 {
		t.Fatalf("impossible must not continue, ran %d turns", turns)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusImpossible {
		t.Fatalf("state = %+v", st)
	}
}

func TestContinueGoalEvaluatorErrorPauses(t *testing.T) {
	a, transcript, calls := newGoalTestApp(t, nil)
	a.goalEvalFunc = func(context.Context, string, string, string) (ark.GoalVerdict, error) {
		atomic.AddInt32(calls, 1)
		return ark.GoalVerdict{}, errors.New("HTTP 500")
	}
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	var turns int32
	if err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { atomic.AddInt32(&turns, 1); return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if turns != 0 {
		t.Fatal("evaluator failure must not run a continuation")
	}
	if got := atomic.LoadInt32(calls); got != int32(goal.MaxEvaluatorFailures) {
		t.Fatalf("want %d evaluator attempts, got %d", goal.MaxEvaluatorFailures, got)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusPaused || !strings.Contains(st.LastReason, "evaluator unavailable") {
		t.Fatalf("state = %+v", st)
	}
}

func TestContinueGoalRetriesTransientEvaluatorError(t *testing.T) {
	a, transcript, calls := newGoalTestApp(t, nil)
	a.goalEvalFunc = func(context.Context, string, string, string) (ark.GoalVerdict, error) {
		if atomic.AddInt32(calls, 1) == 1 {
			return ark.GoalVerdict{}, errors.New("temporary")
		}
		return ark.GoalVerdict{Met: true, Reason: "ok after retry"}, nil
	}
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	if err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { return nil }, nil); err != nil {
		t.Fatal(err)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusMet {
		t.Fatalf("state = %+v", st)
	}
	if a.goalEvalFailures != 0 {
		t.Fatalf("failure streak must reset after success, got %d", a.goalEvalFailures)
	}
}

func TestContinueGoalCapPauses(t *testing.T) {
	verdicts := make([]ark.GoalVerdict, goal.MaxIterations)
	for i := range verdicts {
		verdicts[i] = ark.GoalVerdict{Met: false, Reason: fmt.Sprintf("gap %d", i)}
	}
	a, transcript, _ := newGoalTestApp(t, verdicts)
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	var turns int32
	if err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { atomic.AddInt32(&turns, 1); return nil }, nil); err != nil {
		t.Fatal(err)
	}
	if turns != int32(goal.MaxIterations-1) {
		t.Fatalf("want %d continuations, got %d", goal.MaxIterations-1, turns)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusPaused || st.Iterations != goal.MaxIterations {
		t.Fatalf("state = %+v", st)
	}
}

func TestContinueGoalInterruptedContinuationStops(t *testing.T) {
	a, transcript, _ := newGoalTestApp(t, []ark.GoalVerdict{{Met: false, Reason: "not yet"}})
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)

	err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { return ark.ErrInterrupted }, nil)
	if !errors.Is(err, ark.ErrInterrupted) {
		t.Fatalf("want interrupted, got %v", err)
	}
	st, _ := a.goalStore.Load(a.sessionID)
	if st == nil || st.Status != goal.StatusActive {
		t.Fatalf("interruption must leave the goal active for resume, got %+v", st)
	}
}

func TestGoalCheckEventCallback(t *testing.T) {
	var seen []goalCheck
	a, transcript, _ := newGoalTestApp(t, []ark.GoalVerdict{{Met: true, Reason: "ok"}})
	seedGoal(t, a, "GOAL-COND")
	seedTranscript(t, transcript, a.sessionID)
	if err := a.continueGoal(context.Background(), transcript,
		func(string, []loop.Attachment) error { return nil },
		func(c goalCheck) { seen = append(seen, c) }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Verdict != "met" {
		t.Fatalf("checks = %+v", seen)
	}
}
