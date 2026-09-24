package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/goal"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

// initGoalStore creates the per-session goal state store.
func (a *App) initGoalStore() error {
	if a.goalStore != nil {
		return nil
	}
	s, err := goal.NewStore(a.paths.GoalsDir())
	if err != nil {
		return err
	}
	a.goalStore = s
	return nil
}

// loadGoal loads the persisted active goal for the current session into
// memory (paused/terminal goals stay on disk for /goal status).
func (a *App) loadGoal() {
	a.goal = nil
	if a.goalStore == nil || a.sessionID == "" {
		return
	}
	st, err := a.goalStore.Load(a.sessionID)
	if err != nil {
		a.goalLog("%sgoal state: %v%s\n", cYellow, err, cReset)
		return
	}
	if st != nil && st.Active() {
		a.goal = st
	}
}

// materializePendingGoal persists a goal armed before the session
// existed; invoked after the first turn creates the session.
func (a *App) materializePendingGoal() {
	cond := strings.TrimSpace(a.pendingGoal)
	if cond == "" || a.sessionID == "" || a.goal != nil {
		a.pendingGoal = ""
		return
	}
	st := goal.New(a.sessionID, cond, time.Now())
	if err := a.goalStore.Save(st); err != nil {
		a.printf("%sgoal state: %v%s\n", cRed, err, cReset)
		a.pendingGoal = ""
		return
	}
	a.goal = st
	a.pendingGoal = ""
}

// cmdGoal handles "/goal [condition|clear|resume|status]".
func (a *App) cmdGoal(arg string) {
	arg = strings.TrimSpace(arg)
	switch arg {
	case "", "status":
		a.printGoalStatus()
	case "clear", "stop":
		a.clearGoal("Goal cleared")
	case "resume", "continue":
		a.resumeGoal()
	default:
		a.setGoal(arg)
	}
}

func (a *App) setGoal(raw string) {
	cond, err := goal.ValidateCondition(raw)
	if err != nil {
		a.printf("%s%v%s\n", cRed, err, cReset)
		return
	}
	if a.goal != nil && a.goal.Active() {
		a.printf("%sReplacing active goal:%s %s\n", cDim, cReset, truncateRunes(a.goal.Condition, 80))
	}
	a.pendingGoal = ""
	a.goalEvalFailures = 0
	if a.sessionID == "" {
		a.pendingGoal = cond
		a.goalKickoff = cond
		a.printGoalSet(cond, false)
		return
	}
	st := goal.New(a.sessionID, cond, time.Now())
	if err := a.goalStore.Save(st); err != nil {
		a.printf("%sgoal state: %v%s\n", cRed, err, cReset)
		return
	}
	a.goal = st
	a.goalKickoff = cond
	a.printGoalSet(cond, true)
}

func (a *App) printGoalSet(cond string, persisted bool) {
	a.printf("%s● Goal active%s\n", cGreen, cReset)
	if !persisted {
		a.printf("%s(armed; activates when the session starts)%s\n", cDim, cReset)
	}
	a.printf("%s%s%s\n", cBold, cond, cReset)
	a.printf("%s/goal clear to stop early · /goal <condition> to replace%s\n", cDim, cReset)
}

func (a *App) clearGoal(msg string) {
	if a.goalStore != nil && a.sessionID != "" {
		if err := a.goalStore.Clear(a.sessionID); err != nil {
			a.printf("%sgoal state: %v%s\n", cRed, err, cReset)
		}
	}
	a.goal = nil
	a.pendingGoal = ""
	a.printf("%s%s.%s\n", cDim, msg, cReset)
}

func (a *App) resumeGoal() {
	if a.goalStore == nil || a.sessionID == "" {
		a.printf("%sno goal to resume (set one with /goal <condition>)%s\n", cYellow, cReset)
		return
	}
	st, err := a.goalStore.Load(a.sessionID)
	if err != nil || st == nil {
		a.printf("%sno goal to resume (set one with /goal <condition>)%s\n", cYellow, cReset)
		return
	}
	if st.Active() {
		a.printf("%sgoal already active.%s\n", cDim, cReset)
		return
	}
	st.SetStatus(goal.StatusActive, "", time.Now())
	if err := a.goalStore.Save(st); err != nil {
		a.printf("%s%v%s\n", cRed, err, cReset)
		return
	}
	a.goal = st
	a.printf("%sGoal resumed:%s %s\n", cGreen, cReset, truncateRunes(st.Condition, 80))
	a.printf("%sSend a message to continue working toward it.%s\n", cDim, cReset)
}

func (a *App) printGoalStatus() {
	switch {
	case a.goal != nil && a.goal.Active():
		a.printf("%s● Goal active%s  %schecks %d%s\n", cGreen, cReset, cDim, a.goal.Iterations, cReset)
		a.printf("%s\n", a.goal.Condition)
		a.printf("%s/goal <condition> replaces · /goal clear stops%s\n", cDim, cReset)
	case a.pendingGoal != "":
		a.printf("%s● Goal armed%s\n%s\n", cGreen, cReset, a.pendingGoal)
	default:
		a.printGoalOnDiskStatus()
	}
}

func (a *App) printGoalOnDiskStatus() {
	if a.goalStore == nil || a.sessionID == "" {
		a.printf("No goal set. Usage: /goal <condition> | clear | resume\n")
		return
	}
	st, err := a.goalStore.Load(a.sessionID)
	if err != nil || st == nil {
		a.printf("No goal set. Usage: /goal <condition> | clear | resume\n")
		return
	}
	a.printf("Goal %s after %d check(s): %s\n", st.Status, st.Iterations, truncateRunes(st.Condition, 80))
	if st.LastReason != "" {
		a.printf("%s%s%s\n", cDim, truncateRunes(st.LastReason, 200), cReset)
	}
	a.printf("%s/goal resume reactivates a paused goal · /goal <condition> sets a new one%s\n", cDim, cReset)
}

// goalCheck is one evaluator outcome, surfaced to headless renderers.
type goalCheck struct {
	Iterations int    `json:"iterations"`
	Verdict    string `json:"verdict"`
	Reason     string `json:"reason,omitempty"`
}

// continueGoal runs after the first user turn: it materializes an armed
// goal and, while the goal stays active, evaluates and self-continues the
// same session through runTurn.
func (a *App) continueGoal(ctx context.Context, transcript *store.SessionStore,
	runTurn func(string, []loop.Attachment) error, onCheck func(goalCheck)) error {
	a.materializePendingGoal()
	for a.goal != nil && a.goal.Active() {
		if err := ctx.Err(); err != nil {
			return err
		}
		reason, keepGoing := a.evaluateActiveGoal(ctx, transcript, onCheck)
		if !keepGoing {
			return nil
		}
		a.printGoalAutoTurn(reason)
		if err := runTurn(a.goal.ContinuationPrompt(), nil); err != nil {
			if errors.Is(err, ark.ErrInterrupted) {
				return err
			}
			a.goalLog("%s%v%s\n", cRed, err, cReset)
			a.pauseGoal(goal.StatusPaused, "continuation turn failed: "+err.Error())
			return err
		}
	}
	return nil
}

// evaluateActiveGoal runs the tool-free evaluator and persists the
// outcome. keepGoing is true only when another continuation turn should
// run; reason then carries the evaluator's unmet rationale.
func (a *App) evaluateActiveGoal(ctx context.Context, transcript *store.SessionStore,
	onCheck func(goalCheck)) (reason string, keepGoing bool) {
	st := a.goal
	events, err := transcript.Load(a.sessionID)
	if err != nil {
		a.goalLog("%sgoal check skipped — cannot read transcript: %v%s\n", cYellow, err, cReset)
		return "", false
	}
	userPrompt := goal.EvaluatorUserPrompt(st.Condition, goal.FormatTranscript(events))

	evaluate := a.goalEvalFunc
	if evaluate == nil {
		evaluate = a.ctrl.Client.EvaluateGoal
	}
	var v ark.GoalVerdict
	for {
		v, err = evaluate(ctx, a.evaluatorModel(), goal.EvaluatorSystemPrompt, userPrompt)
		if err == nil {
			a.goalEvalFailures = 0
			break
		}
		a.goalEvalFailures++
		a.emitCheck(onCheck, st.Iterations, "evaluator_error", err.Error())
		if a.goalEvalFailures < goal.MaxEvaluatorFailures && ctx.Err() == nil {
			a.goalLog("%sgoal check failed (%d/%d): %v — retrying…%s\n",
				cYellow, a.goalEvalFailures, goal.MaxEvaluatorFailures, err, cReset)
			delay := evaluatorRetryDelay
			if a.goalRetryDelay != nil {
				delay = a.goalRetryDelay
			}
			if !sleepWithContext(ctx, delay(a.goalEvalFailures)) {
				return "", false
			}
			continue
		}
		now := time.Now()
		st.SetStatus(goal.StatusPaused, "goal evaluator unavailable: "+err.Error(), now)
		_ = a.goalStore.Save(st)
		a.goalEvalFailures = 0
		a.goalLog("%sGoal paused — the goal check could not complete: %v%s\n", cYellow, err, cReset)
		a.goalLog("%sSend a message to continue, or run /goal clear.%s\n", cDim, cReset)
		a.goal = nil
		return "", false
	}
	now := time.Now()
	switch {
	case v.Met:
		checks := st.Iterations + 1
		st.SetStatus(goal.StatusMet, v.Reason, now)
		_ = a.goalStore.Save(st)
		a.emitCheck(onCheck, checks, "met", v.Reason)
		a.printGoalMet(checks, v.Reason)
		a.goal = nil
		return "", false
	case v.Impossible:
		checks := st.Iterations + 1
		st.SetStatus(goal.StatusImpossible, v.Reason, now)
		_ = a.goalStore.Save(st)
		a.emitCheck(onCheck, checks, "impossible", v.Reason)
		a.goalLog("%sGoal could not be achieved%s — %s\n", cRed, cReset, v.Reason)
		a.goalLog("%sRun /goal resume to retry, or /goal <condition> to replace.%s\n", cDim, cReset)
		a.goal = nil
		return "", false
	}

	st.MarkChecked(v.Reason, now)
	if st.Iterations >= goal.MaxIterations {
		st.SetStatus(goal.StatusPaused, fmt.Sprintf("goal checks kept finding it unmet after %d turns", st.Iterations), now)
		_ = a.goalStore.Save(st)
		a.emitCheck(onCheck, st.Iterations, "cap", v.Reason)
		a.goalLog("%sGoal paused after %d goal checks without confirmation.%s\n", cYellow, st.Iterations, cReset)
		a.goalLog("%s%s%s\n", cDim, truncateRunes(v.Reason, 200), cReset)
		a.goal = nil
		return v.Reason, false
	}
	_ = a.goalStore.Save(st)
	a.emitCheck(onCheck, st.Iterations, "unmet", v.Reason)
	return v.Reason, true
}

func (a *App) emitCheck(onCheck func(goalCheck), iterations int, verdict, reason string) {
	if onCheck != nil {
		onCheck(goalCheck{Iterations: iterations, Verdict: verdict, Reason: reason})
	}
}

func (a *App) pauseGoal(status, reason string) {
	if a.goal == nil {
		return
	}
	a.goal.SetStatus(status, reason, time.Now())
	_ = a.goalStore.Save(a.goal)
	a.goal = nil
}

func (a *App) evaluatorModel() string {
	return a.model
}

func (a *App) printGoalAutoTurn(reason string) {
	n := 0
	if a.goal != nil {
		n = a.goal.Iterations
	}
	line := fmt.Sprintf("%s⏵ goal check #%d: not yet met%s", cCyan, n, cReset)
	if r := strings.TrimSpace(reason); r != "" {
		line += " " + a.style(cDim, "— "+truncateRunes(r, 160))
	}
	a.goalLog("%s\n", line)
	a.goalLog("%s⏵ continuing toward goal…%s\n", cDim, cReset)
}

func (a *App) printGoalMet(n int, reason string) {
	a.goalLog("%s● Goal achieved%s after %d goal check(s).%s\n", cGreen, cReset, n, cReset)
	if r := strings.TrimSpace(reason); r != "" {
		a.goalLog("%s%s%s\n", cDim, truncateRunes(r, 200), cReset)
	}
}

func truncateRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func evaluatorRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 2 * time.Second
	case 2:
		return 5 * time.Second
	default:
		return 10 * time.Second
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// goalLog prints goal-progress lines in interactive and headless text
// mode only; structured headless output stays machine-readable.
func (a *App) goalLog(format string, args ...interface{}) {
	if a.opts.Print && a.opts.OutputFormat != FormatText {
		return
	}
	a.printf(format, args...)
}
