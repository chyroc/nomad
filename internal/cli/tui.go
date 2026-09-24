package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/loop"
	"github.com/chyroc/nomad/internal/store"
)

func isTerminal(w interface{}) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (a *App) runTUI(ctx context.Context) error {
	transcript, err := store.NewSessionStore(a.paths.SessionsDir())
	if err != nil {
		return err
	}
	if fdOf(a.out) >= 0 {
		io.WriteString(a.out, "\x1b[?2004l")
	}
	a.editor = newLineEditor(a.in, a.out, nil, a.paths.HistoryFile())
	a.editor.setCompleter(a.completeSlash)
	a.editor.onFold = a.latestFoldLines

	a.printf("%s◆ Nomad%s · %s · model %s · %s\n",
		a.style(cBold, ""), cReset, a.style(cGreen, "managed-agents"),
		a.style(cCyan, a.model), a.style(cDim, a.paths.Workspace))
	a.printf("%s /help for commands · /model to switch · Ctrl+C interrupt, twice to quit%s\n\n", cDim, cReset)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		for range sigCh {
			if a.handleInterrupt() {
				io.WriteString(a.out, "\x1b[?2004l")
				a.printResumeHint()
				os.Exit(130)
			}
		}
	}()
	defer signal.Stop(sigCh)

	if id := a.resumeSessionID(transcript); id != "" {
		if err := a.attachSession(ctx, transcript, id); err != nil {
			return err
		}
	}

	for {
		a.printStatusline()
		a.printf("%s\n", a.modeLine())
		input, err := a.readInput()
		if errors.Is(err, errEOF) {
			a.printf("\n")
			a.printResumeHint()
			return nil
		}
		if err != nil {
			return err
		}
		if strings.TrimSpace(input) == "" {
			continue
		}
		if strings.HasPrefix(input, "/") {
			quit, err := a.handleCommand(ctx, transcript, input)
			if err != nil {
				a.printf("%s%v%s\n", cRed, err, cReset)
			}
			if quit {
				return nil
			}
			if kickoff := a.goalKickoff; kickoff != "" {
				a.goalKickoff = ""
				if err := a.turn(ctx, transcript, kickoff, nil); err == nil {
					_ = a.continueGoal(ctx, transcript,
						func(text string, _ []loop.Attachment) error { return a.turn(ctx, transcript, text, nil) },
						nil)
				} else if errors.Is(err, ark.ErrInterrupted) {
					a.printf("%sTurn interrupted; context retained.%s\n\n", cYellow, cReset)
				} else {
					a.printf("%s%v%s\n\n", cRed, err, cReset)
				}
			}
			continue
		}
		if strings.HasPrefix(input, "!") {
			a.runBangCommand(strings.TrimPrefix(input, "!"))
			continue
		}
		if err := a.turn(ctx, transcript, input, nil); err != nil {
			if errors.Is(err, ark.ErrInterrupted) {
				a.printf("%sTurn interrupted; context retained.%s\n\n", cYellow, cReset)
				continue
			}
			if errors.Is(err, loop.ErrMaxTurns) {
				a.printf("%sReached maximum number of turns; context retained.%s\n\n", cYellow, cReset)
				continue
			}
			a.printf("%s%v%s\n\n", cRed, err, cReset)
			continue
		}
		if err := a.continueGoal(ctx, transcript,
			func(text string, _ []loop.Attachment) error { return a.turn(ctx, transcript, text, nil) },
			nil); err != nil {
			if errors.Is(err, ark.ErrInterrupted) {
				a.printf("%sTurn interrupted; context retained.%s\n\n", cYellow, cReset)
			}
		}
	}
}

var errEOF = errors.New("eof")

func (a *App) readInput() (string, error) {
	var input string
	for {
		prompt := a.style(cBold, "❯ ")
		if input != "" {
			prompt = a.style(cDim, "… ")
		}
		line, err := a.editor.ReadLine(prompt)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", errEOF
			}
			if errors.Is(err, ErrInterrupt) {
				if a.handleInterrupt() {
					return "", errEOF
				}
				return "", nil
			}
			return "", err
		}
		if strings.TrimSpace(line) == "" {
			return input, nil
		}
		if strings.HasSuffix(line, "\\") {
			input += strings.TrimSuffix(line, "\\") + "\n"
			continue
		}
		input += line
		return input, nil
	}
}

// attachSession builds a runner attached to an existing remote session and
// prints its recent transcript.
func (a *App) attachSession(ctx context.Context, transcript *store.SessionStore, remoteID string) error {
	runner, err := a.newRunner(ctx, remoteID, a.askToolPermission)
	if err != nil {
		return err
	}
	a.setRunner(runner)
	a.sessionID = runner.SessionID()
	a.installToolHooks(runner)
	a.loadGoal()
	if evs, err := transcript.Load(remoteID); err == nil {
		a.replay(evs)
	}
	return nil
}

func (a *App) turn(ctx context.Context, transcript *store.SessionStore, text string, atts []loop.Attachment) error {
	isNewRunner := a.runner == nil
	if isNewRunner {
		runner, err := a.newRunner(ctx, "", a.askToolPermission)
		if err != nil {
			return err
		}
		a.setRunner(runner)
		a.sessionID = runner.SessionID()
		a.installToolHooks(runner)
		var started bool
		text, started = a.runSessionStartHook(ctx, a.sessionID, text)
		if !started {
			a.printf("%sSessionStart hook blocked the turn.%s\n", cYellow, cReset)
			return nil
		}
	}
	var allowed bool
	text, allowed = a.applyPromptHooks(ctx, a.sessionID, text)
	if !allowed {
		a.printf("%sUserPromptSubmit hook blocked the message.%s\n", cYellow, cReset)
		return nil
	}

	a.startActivity("Working…")
	a.lastAnswer = ""
	a.assistantStreamed = false
	a.answerAnchorPrinted = false
	a.thinkingBuf.Reset()
	a.thinkingStart = time.Time{}
	a.setToolCount(0)
	turnCtx, cancel := context.WithCancel(ctx)
	a.turnMu.Lock()
	a.turnCancel = cancel
	a.turnStart = time.Now()
	a.turnMu.Unlock()

	err := a.runner.Run(turnCtx, text, atts)

	cancel()
	a.turnMu.Lock()
	a.turnCancel = nil
	a.turnMu.Unlock()
	a.endTurnPanel("")
	if err == nil {
		a.printf("\n")
	}
	return err
}

func (a *App) setRunner(r loop.Runner) {
	a.turnMu.Lock()
	a.runner = r
	a.turnMu.Unlock()
	r.Subscribe(loop.ObserverFunc(a.onInteractiveEvent))
}

// onInteractiveEvent renders and persists events.
func (a *App) onInteractiveEvent(ev loop.Event) {
	transcript, _ := store.NewSessionStore(a.paths.SessionsDir())
	if transcript != nil && a.sessionID != "" {
		_ = transcript.Append(a.sessionID, ev)
	}
	fn := func() {
		a.renderInteractiveEvent(ev)
	}
	a.modalMu.Lock()
	if a.modalActive {
		a.modalPending = append(a.modalPending, fn)
		a.modalMu.Unlock()
		return
	}
	a.modalMu.Unlock()
	fn()
}

func (a *App) renderInteractiveEvent(ev loop.Event) {
	switch ev.Kind {
	case loop.EvAssistantThinking:
		if a.thinkingStart.IsZero() {
			a.thinkingStart = time.Now()
		}
		a.thinkingBuf.WriteString(strings.TrimSpace(ev.Content))
		a.thinkingBuf.WriteByte('\n')
		a.setActivity(a.style(cPurple, "✻") + " " + a.style(cDim, "Thinking…"))
	case loop.EvAssistantChunk:
		a.lastAnswer += ev.Content
		a.assistantStreamed = true
		a.flushThinking()
		a.finishActivity("")
		a.renderAnswer(ev.Content)
	case loop.EvAssistantMessage:
		a.flushThinking()
		a.finishActivity("")
		if a.assistantStreamed {
			// Already rendered from the chunk event; the two events
			// carry the same content.
			break
		}
		a.renderAnswer(ev.Content)
	case loop.EvToolCall:
		a.flushThinking()
		a.finishActivity("")
		if ev.ToolCall != nil {
			a.rememberToolCall(ev.ToolCall, ev.Time)
			a.startActivity(toolRunningLabel(ev.ToolCall.Name, ev.ToolCall.Arguments))
		}
	case loop.EvToolResult:
		a.finishTool(ev)
	case loop.EvTurnEnd:
		a.flushThinking()
		a.finishActivity("")
		line := turnEndLine(time.Now(), a.elapsedTurn(), a.toolStepCount())
		if ev.Usage != nil && a.opts.Verbose {
			line += a.style(cDim, fmt.Sprintf(" · ↑%d ↓%d tokens", ev.Usage.InputTokens, ev.Usage.OutputTokens))
		}
		a.endTurnPanel(line)
		go a.runStopHooks(context.Background(), a.sessionID)
	case loop.EvError:
		a.finishActivity("")
		a.flushThinking()
		a.printf("%s● %v%s\n", cRed, ev.Content, cReset)
	}
}

// renderAnswer prints an assistant text block (markdown when a TTY).
func (a *App) renderAnswer(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	anchor := ""
	if !a.answerAnchorPrinted {
		anchor = a.style(cPurple, "● ") + cReset
		a.answerAnchorPrinted = true
	}
	if a.color {
		width, _ := cachedTermSize()
		rendered := strings.TrimLeft(renderMarkdown(text, width, true), "\n")
		a.printf("%s%s", anchor, rendered)
		return
	}
	a.printf("%s%s\n\n", anchor, text)
}

// startActivity opens the turn's live progress region.
func (a *App) startActivity(label string) {
	if a.panel == nil {
		a.panel = newTurnPanel(a.out, a.color)
	}
	a.panel.begin(label)
}

func (a *App) setActivity(label string) {
	if a.panel == nil {
		a.startActivity(label)
		return
	}
	a.panel.setSpinner(label)
}

// finishActivity clears the spinner but keeps the progress region.
func (a *App) finishActivity(string) {
	if a.panel != nil {
		a.panel.setSpinner("")
	}
}

// endTurnPanel erases the live progress region, printing final as a
// permanent line when non-empty. It is safe on inactive panels.
func (a *App) endTurnPanel(final string) {
	if a.panel != nil {
		a.panel.finish(final)
	}
}

// pushPanel adds one transient progress line to the live region, or
// prints it directly when no panel exists (tests, replay).
func (a *App) pushPanel(line string) {
	if a.panel != nil {
		a.panel.push(line)
		return
	}
	a.printf("%s\n", line)
}

// toolRunningLabel renders the spinner label for an in-flight tool
// call: tool name plus a short argument preview.
func toolRunningLabel(name, arguments string) string {
	return "⠿ " + name + " " + toolInvocation(name, arguments, 40)
}

// pendingTool remembers one in-flight call so its result line can show
// the arguments and the elapsed time.
type pendingTool struct {
	name  string
	args  string
	start time.Time
}

// rememberToolCall records a call for the matching result event. The
// start time comes from the event so replayed transcripts measure the
// original duration.
func (a *App) rememberToolCall(call *loop.ToolCall, at time.Time) {
	if call == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	if a.pendingTools == nil {
		a.pendingTools = map[string]pendingTool{}
	}
	if len(a.pendingTools) > 64 {
		a.pendingTools = map[string]pendingTool{}
	}
	a.pendingTools[call.ID] = pendingTool{name: call.Name, args: call.Arguments, start: at}
}

// takeToolCall removes and returns the pending call for a result event.
func (a *App) takeToolCall(id string) pendingTool {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	call := a.pendingTools[id]
	delete(a.pendingTools, id)
	return call
}

// toolResultSummary builds the one-line result digest for a tool step.
func toolResultSummary(name, args, result string) string {
	var parsed map[string]any
	if args != "" {
		_ = json.Unmarshal([]byte(args), &parsed)
	}
	str := func(k string) string { v, _ := parsed[k].(string); return v }
	switch name {
	case "write":
		path := str("file_path")
		if path == "" {
			path = str("path")
		}
		if n := strings.Count(str("content"), "\n") + 1; path != "" {
			return fmt.Sprintf("Wrote %d lines to %s", n, path)
		}
	case "edit":
		path := str("file_path")
		if path == "" {
			path = str("path")
		}
		added := strings.Count(str("new_string"), "\n") + 1
		removed := strings.Count(str("old_string"), "\n") + 1
		if path != "" {
			return fmt.Sprintf("Added %d lines, removed %d lines", added, removed)
		}
	}
	first := strings.TrimSpace(strings.Split(strings.TrimSpace(result), "\n")[0])
	if first == "" {
		return "(No output)"
	}
	r := []rune(first)
	if len(r) > 72 {
		return string(r[:72]) + "…"
	}
	return first
}

// renderThinkingFold registers one reasoning block as a fold and adds
// its transient summary line to the live region.
func (a *App) renderThinkingFold(lines []string, started time.Time) {
	if len(lines) == 0 {
		return
	}
	a.registerFold("reasoning", lines)
	clause := ""
	if !started.IsZero() {
		clause = " for " + formatThoughtDuration(time.Since(started))
	}
	a.pushPanel(a.style(cDim, "Thought"+clause) + a.style(cDim, " (ctrl+o to expand)"))
}

// turnEndLine renders the post-turn summary line, including the number
// of tool steps whose details were erased with the live region.
func turnEndLine(end time.Time, elapsed time.Duration, tools int) string {
	toolPart := ""
	if tools > 0 {
		toolPart = fmt.Sprintf(" · %d tools", tools)
	}
	return fmt.Sprintf("%s✻ Worked for %s%s · done %s%s",
		cDim, formatThoughtDuration(elapsed), toolPart, end.Format("3:04 PM"), cReset)
}

// completeToolStep resolves one tool result into its transient summary
// line and its expandable fold, counting the step. The line is not
// printed; callers route it to the live region or drop it.
func (a *App) completeToolStep(ev loop.Event) string {
	name := ev.ToolName
	if name == "" {
		name = "tool"
	}
	callID := ""
	if ev.ToolCall != nil {
		callID = ev.ToolCall.ID
	}
	call := a.takeToolCall(callID)
	if call.name != "" {
		name = call.name
	}
	summary := toolResultSummary(name, call.args, ev.Result)
	body := strings.TrimSpace(ev.Result)
	if body == "" {
		body = "(No output)"
	}
	fold := strings.Split(body, "\n")
	if diff, ok := toolCallDiffLines(name, call.args, a.paths.Workspace); ok {
		fold = append(fold, diff...)
	}

	line := fmt.Sprintf("● %s%s %s · %s", a.style(cBold, name),
		toolInvocation(name, call.args, 40), toolStatusMark(ev.IsError, call.start, ev.Time), summary)
	if !isTinyResult(fold) {
		a.registerFold(name, fold)
		line += a.style(cDim, " (ctrl+o to expand)")
	}
	if ev.IsError {
		line = cRed + line + cReset
	}
	a.addToolCount(1)
	return line
}

// finishTool renders one completed tool step into the live region,
// e.g. "● bash(go test) ✓ 3s · 12 lines"; the invocation, diff and
// full output stay available through the Ctrl+O fold.
func (a *App) finishTool(ev loop.Event) {
	a.finishActivity("")
	a.pushPanel(a.completeToolStep(ev))
}

func (a *App) setToolCount(n int) {
	a.foldMu.Lock()
	a.toolCount = n
	a.foldMu.Unlock()
}

func (a *App) addToolCount(n int) {
	a.foldMu.Lock()
	a.toolCount += n
	a.foldMu.Unlock()
}

func (a *App) toolStepCount() int {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	return a.toolCount
}

// toolStatusMark renders the ✓/✗ marker with the elapsed time when the
// call ran for at least a second.
func toolStatusMark(isErr bool, start, end time.Time) string {
	mark, color := "✓", cGreen
	if isErr {
		mark, color = "✗", cRed
	}
	out := color + mark + cReset
	if !start.IsZero() && !end.IsZero() {
		if d := end.Sub(start); d >= time.Second {
			out += cDim + " " + formatThoughtDuration(d) + cReset
		}
	}
	return out
}

// isTinyResult reports whether a result is short enough to skip the
// fold entirely: a single trimmed line under 80 columns with no diff.
func isTinyResult(lines []string) bool {
	if len(lines) != 1 {
		return false
	}
	return len([]rune(strings.TrimSpace(lines[0]))) <= 80
}

// renderFoldable prints an indented body, collapsing long output into a
// head/tail preview with an expandable fold.
func (a *App) renderFoldable(header, body, color string) {
	head, tail, more, folded := foldLines(header, body)
	if !folded {
		for _, l := range head {
			a.printf("%s  %s%s\n", color, l, cReset)
		}
		return
	}
	for _, l := range head {
		a.printf("%s  %s%s\n", color, l, cReset)
	}
	a.registerFold(header, strings.Split(body, "\n"))
	a.printf("%s\n", foldBar(more))
	for _, l := range tail {
		a.printf("%s  %s%s\n", color, l, cReset)
	}
}

// flushThinking renders accumulated reasoning as a single collapsed
// line registered as a fold (Ctrl+O or click expands it).
func (a *App) flushThinking() {
	body := strings.TrimSpace(a.thinkingBuf.String())
	a.thinkingBuf.Reset()
	if body == "" {
		return
	}
	a.finishActivity("")
	started := a.thinkingStart
	a.thinkingStart = time.Time{}
	a.renderThinkingFold(strings.Split(body, "\n"), started)
}

func (a *App) registerFold(header string, lines []string) int {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	if a.folds == nil {
		a.folds = map[int]*foldBlock{}
		a.foldSeen = map[int]bool{}
	}
	a.nextFoldID++
	id := a.nextFoldID
	a.folds[id] = &foldBlock{id: id, header: header, lines: lines}
	a.foldOrder = append(a.foldOrder, id)
	return id
}

// foldLines marks a fold as seen and renders its expansion lines.
func (a *App) foldLines(id int) []string {
	a.foldMu.Lock()
	b := a.folds[id]
	a.foldSeen[id] = true
	a.foldMu.Unlock()
	if b == nil {
		return nil
	}
	lines := []string{fmt.Sprintf("%s  ┌─ expanded %s (%d lines)%s", cCyan, b.header, len(b.lines), cReset)}
	for _, l := range b.lines {
		lines = append(lines, fmt.Sprintf("%s  │ %s%s", cDim, l, cReset))
	}
	lines = append(lines, fmt.Sprintf("%s  └──────────────%s", cCyan, cReset))
	return lines
}

func (a *App) expandFold(id int) {
	for _, l := range a.foldLines(id) {
		a.printf("%s\n", l)
	}
}

// latestFoldLines returns the expansion of the earliest not-yet-seen
// fold, so repeated Ctrl+O walks folds top to bottom in scrollback
// order.
func (a *App) latestFoldLines() []string {
	a.foldMu.Lock()
	var id int
	for _, fid := range a.foldOrder {
		if !a.foldSeen[fid] {
			id = fid
			break
		}
	}
	a.foldMu.Unlock()
	if id == 0 {
		return nil
	}
	return a.foldLines(id)
}

// replay reprints a stored transcript. Ended turns keep only their
// question, answer and summary line: tool steps and thoughts are
// transient by design, so replaying them registers their folds for
// Ctrl+O without printing the per-step lines.
func (a *App) replay(evs []loop.Event) {
	var turnStart time.Time
	for _, ev := range evs {
		switch ev.Kind {
		case loop.EvUserMessage:
			turnStart = ev.Time
			a.setToolCount(0)
			a.printf("%s %s\n", a.style(cBold, "❯"), ev.Content)
		case loop.EvAssistantChunk:
		case loop.EvAssistantMessage:
			if strings.TrimSpace(ev.Content) != "" {
				a.answerAnchorPrinted = false
				a.renderAnswer(ev.Content)
			}
		case loop.EvToolCall:
			a.rememberToolCall(ev.ToolCall, ev.Time)
		case loop.EvToolResult:
			a.completeToolStep(ev)
		case loop.EvTurnEnd:
			if !ev.Time.IsZero() {
				elapsed := time.Duration(0)
				if !turnStart.IsZero() {
					elapsed = ev.Time.Sub(turnStart)
				}
				line := turnEndLine(ev.Time, elapsed, a.toolStepCount())
				if ev.Usage != nil && a.opts.Verbose {
					line += a.style(cDim, fmt.Sprintf(" · ↑%d ↓%d tokens", ev.Usage.InputTokens, ev.Usage.OutputTokens))
				}
				a.printf("%s\n", line)
			}
		case loop.EvError:
			a.printf("%s● %v%s\n", cRed, ev.Content, cReset)
		}
	}
}

// askToolPermission is the interactive permission callback (default
// mode). The live region is suspended while the picker is on screen.
func (a *App) askToolPermission(name, args string) string {
	a.finishActivity("")
	if a.panel != nil {
		a.panel.suspend()
		defer a.panel.resume()
	}
	return a.askPermissionChoice(name, args)
}

var _ = ark.PermDefault
var _ = fmt.Sprintf

func (a *App) completeSlash(line string) []string {
	first := strings.Fields(line)
	prefix := strings.TrimSpace(line)
	if len(first) > 1 {
		return nil
	}
	var out []string
	for _, c := range slashCommandNames() {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func slashCommandNames() []string {
	return []string{
		"/help", "/clear", "/copy", "/model", "/effort", "/status", "/diff", "/export", "/cost", "/resume", "/sessions",
		"/session", "/skills", "/memory", "/permissions", "/goal", "/config", "/init",
		"/login", "/logout", "/exit",
	}
}

func (a *App) handleInterrupt() bool {
	a.interruptMu.Lock()
	now := time.Now()
	double := now.Sub(a.lastInterrupt) < 2*time.Second
	if !double {
		a.lastInterrupt = now
	}
	a.interruptMu.Unlock()
	if double {
		return true
	}
	a.printf("\n%s(interrupting — Ctrl+C again to quit)%s\n", cYellow, cReset)
	a.cancelTurn()
	return false
}
