package cli

import (
	"context"
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
	a.editor.color = a.color
	a.editor.setCompleter(a.completeSlash)
	a.editor.onFold = a.latestFoldLines
	a.bar = newStatusBar()
	a.editor.setFrame(a.renderInputFrame)
	a.editor.setCycleMode(func() { a.cyclePermissionMode() })

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

// renderInputFrame builds the pinned chrome around one editor
// invocation: a full-width top rule and the rows beneath the input
// (bottom rule plus two status rows).
func (a *App) renderInputFrame(width int) inputFrame {
	if width < 8 {
		width = 8
	}
	rule := cDim + strings.Repeat("-", width-1) + cReset
	if !a.color {
		rule = strings.Repeat("-", width-1)
	}
	line1, line2 := a.bar.render(a, width)
	return inputFrame{top: rule, rows: []string{rule, line1, line2}}
}

var errEOF = errors.New("eof")

func (a *App) readInput() (string, error) {
	var input string
	for {
		prompt := a.style(cBold, "❯ ")
		firstLine := input == ""
		if !firstLine {
			prompt = a.style(cDim, "… ")
		}
		line, err := a.editor.ReadLine(prompt, readLineOptions{
			showTopRule:    firstLine,
			blankContinues: firstLine,
		})
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

	a.startActivity("")
	a.lastAnswer = ""
	a.assistantStreamed = false
	a.answerAnchorPrinted = false
	a.thoughtLineShown = false
	a.browseNotes = nil
	a.thinkingBuf.Reset()
	a.thinkingStart = time.Time{}
	a.turnEndSummary = ""
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
	summary := a.turnEndSummary
	a.turnEndSummary = ""
	a.endTurnPanel(summary)
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
			a.thinkingStart = ev.Time
			if a.thinkingStart.IsZero() {
				a.thinkingStart = time.Now()
			}
		}
		a.thinkingBuf.WriteString(strings.TrimSpace(ev.Content))
		a.thinkingBuf.WriteByte('\n')
		a.setSpinnerPhase(phaseThinking)
	case loop.EvAssistantChunk:
		a.lastAnswer += ev.Content
		a.assistantStreamed = true
		a.feedStreamedChars(len([]rune(ev.Content)))
		a.flushThinking()
		a.ensureThoughtLine()
		a.finishActivity("")
		a.renderAnswer(ev.Content)
	case loop.EvAssistantMessage:
		a.flushThinking()
		a.ensureThoughtLine()
		a.finishActivity("")
		if a.assistantStreamed {
			// Already rendered from the chunk event; the two events
			// carry the same content.
			break
		}
		a.renderAnswer(ev.Content)
	case loop.EvToolCall:
		if ev.ToolCall != nil && !isBrowseTool(ev.ToolCall.Name) && webToolKind(ev.ToolCall.Name) == "" {
			a.flushThinking()
		}
		if ev.ToolCall != nil {
			a.rememberToolCall(ev.ToolCall, ev.Time)
			if webToolKind(ev.ToolCall.Name) == "" {
				a.startToolSpinner(ev.ToolCall.Name, ev.ToolCall.Arguments)
			}
		}
	case loop.EvToolResult:
		a.finishTool(ev)
	case loop.EvUsage:
		if ev.Usage != nil {
			a.setSpinnerOutput(ev.Usage.OutputTokens)
		}
	case loop.EvTurnEnd:
		a.flushDeferredToolResults()
		a.flushThinking()
		a.collapseTurnPanel()
		line := turnEndLine(time.Now(), a.elapsedTurn(), a.toolStepCount())
		if ev.Usage != nil && a.opts.Verbose {
			line += a.style(cDim, fmt.Sprintf(" · ↑%d ↓%d tokens", ev.Usage.InputTokens, ev.Usage.OutputTokens))
		}
		a.turnEndSummary = line
		go a.runStopHooks(context.Background(), a.sessionID)
	case loop.EvError:
		a.finishActivity("")
		a.flushThinking()
		a.printf("%s● %v%s\n", cRed, ev.Content, cReset)
	}
}

// stripFirstLineMargin removes the glamour document indentation
// (two columns) from only the first rendered line, which may begin
// with SGR sequences. Continuation lines keep the indent so the body
// aligns under the first line's text.
func stripFirstLineMargin(s string) string {
	i := 0
	for i < len(s) {
		switch s[i] {
		case '\n', '\r':
			i++
			continue
		case 0x1b:
			j := i + 2
			for j < len(s) {
				c := s[j]
				j++
				if c >= 0x40 && c <= 0x7e {
					break
				}
			}
			i = j
			continue
		}
		break
	}
	return s[:i] + strings.TrimPrefix(s[i:], "  ")
}

// renderAnswer prints an assistant text block (markdown when a TTY).
func (a *App) renderAnswer(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	first := !a.answerAnchorPrinted
	if first {
		a.answerAnchorPrinted = true
	}
	if a.color {
		width, _ := cachedTermSize()
		rendered := strings.TrimLeft(renderMarkdown(text, width-1, true), "\n")
		if first {
			rendered = stripFirstLineMargin(rendered)
			a.printf("%s %s", a.style(cPurple, "●"), rendered)
			return
		}
		a.printf("%s", rendered)
		return
	}
	if first {
		a.printf("%s %s\n\n", a.style(cPurple, "●"), text)
		return
	}
	a.printf("%s\n\n", text)
}

// dockChromeRows builds the rows kept pinned at the bottom while a
// turn runs: top rule, the static empty input row, bottom rule and the
// two status rows.
func (a *App) dockChromeRows(width int) []string {
	f := a.renderInputFrame(width)
	input := a.style(cBold, "❯ ")
	return append([]string{f.top, input}, f.rows...)
}

// startActivity opens the turn's live progress region above the
// docked input chrome with the dynamic phased spinner.
func (a *App) startActivity(label string) {
	if a.panel == nil {
		a.panel = newTurnPanel(a.out, a.color)
	}
	a.panel.setChrome(a.dockChromeRows)
	a.panel.begin(label)
}

func (a *App) setActivity(label string) {
	if a.panel == nil {
		a.startActivity(label)
		return
	}
	a.panel.setSpinner(label)
}

// setSpinnerPhase switches the dynamic spinner phase.
func (a *App) setSpinnerPhase(phase spinnerPhase) {
	if a.panel == nil {
		a.startActivity("")
		return
	}
	a.panel.setPhase(phase)
}

// startToolSpinner shows a static gerund for browsing tools and the
// dynamic working phase for everything else.
func (a *App) startToolSpinner(name, args string) {
	if isBrowseTool(name) {
		a.setActivity(runningLabel(name, args))
		return
	}
	a.setSpinnerPhase(phaseWorking)
}

func (a *App) setSpinnerOutput(n int) {
	if a.panel != nil {
		a.panel.setOutputTokens(n)
	}
}

func (a *App) feedStreamedChars(n int) {
	if a.panel != nil {
		a.panel.addStreamedChars(n)
	}
}

// finishActivity drops a pinned static label and returns to the
// dynamic working spinner; it never hides the spinner mid-turn.
func (a *App) finishActivity(string) {
	if a.panel != nil && a.panel.isPinned() {
		a.panel.setPhase(phaseWorking)
	}
}

// endTurnPanel erases the live progress region, printing final as a
// permanent line when non-empty. It is safe on inactive panels.
func (a *App) endTurnPanel(final string) {
	if a.panel != nil {
		a.panel.finish(final)
	}
}

// collapseTurnPanel drops the spinner and tool rows while leaving the
// docked input chrome on screen during the end_turn grace window.
func (a *App) collapseTurnPanel() {
	if a.panel != nil {
		a.panel.collapseDynamic()
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
// call: a gerund phrase for browsing actions, name plus short args
// otherwise.
func toolRunningLabel(name, arguments string) string {
	return "⠿ " + runningLabel(name, arguments)
}

// pendingTool remembers one in-flight call so its result line can show
// the arguments and the elapsed time.
type pendingTool struct {
	id    string
	name  string
	args  string
	start time.Time
}

// rememberToolCall records a call for the matching result event. The
// start time comes from the event so replayed transcripts measure the
// original duration. Calls are kept in arrival order because the
// managed-agents server emits server-side tool calls with empty ids.
func (a *App) rememberToolCall(call *loop.ToolCall, at time.Time) {
	if call == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	a.foldMu.Lock()
	if len(a.pendingTools) > 64 {
		a.pendingTools = nil
	}
	a.pendingTools = append(a.pendingTools, pendingTool{id: call.ID, name: call.Name, args: call.Arguments, start: at})
	a.foldMu.Unlock()
	a.reconcileDeferredTools()
}

// reconcileDeferredTools settles results that were processed before
// their own tool_use event arrived. The pump's result channel races
// the event stream, so a fast server-side result can render-arrive
// milliseconds before the call that owns it.
func (a *App) reconcileDeferredTools() {
	for {
		ev, call, ok := a.nextDeferredPair()
		if !ok {
			return
		}
		a.settleToolCall(call, ev)
	}
}

// nextDeferredPair pops the earliest deferred result whose tool call
// has arrived, together with the oldest pending call of that name.
func (a *App) nextDeferredPair() (loop.Event, pendingTool, bool) {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	for i, ev := range a.deferredTools {
		name := ev.ToolName
		if name == "" && ev.ToolCall != nil {
			name = ev.ToolCall.Name
		}
		if name == "" {
			continue
		}
		for j, c := range a.pendingTools {
			if c.name != name {
				continue
			}
			a.deferredTools = append(a.deferredTools[:i], a.deferredTools[i+1:]...)
			a.pendingTools = append(a.pendingTools[:j], a.pendingTools[j+1:]...)
			return ev, c, true
		}
	}
	return loop.Event{}, pendingTool{}, false
}

// flushDeferredToolResults settles results whose call never arrived
// before the turn ends, so no block is silently dropped.
func (a *App) flushDeferredToolResults() {
	a.foldMu.Lock()
	evs := a.deferredTools
	a.deferredTools = nil
	a.foldMu.Unlock()
	for _, ev := range evs {
		a.settleToolCall(pendingTool{}, ev)
	}
}

// takeToolCall removes and returns the pending call for a result event,
// matching by call id first and falling back to the oldest pending call
// with the same tool name when the call carried no id.
func (a *App) takeToolCall(id, name string) pendingTool {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	idx := -1
	if id != "" {
		for i, c := range a.pendingTools {
			if c.id == id {
				idx = i
				break
			}
		}
	}
	if idx < 0 && name != "" {
		for i, c := range a.pendingTools {
			if c.name == name {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return pendingTool{}
	}
	call := a.pendingTools[idx]
	a.pendingTools = append(a.pendingTools[:idx], a.pendingTools[idx+1:]...)
	return call
}

// renderThinkingFold registers one reasoning block as a fold and
// prints its collapsed summary as a permanent line above the answer.
func (a *App) renderThinkingFold(lines []string, started time.Time) {
	if len(lines) == 0 {
		return
	}
	a.registerFold("reasoning", lines)
	a.printThoughtLine(started)
}

// printThoughtLine emits the permanent "Thought for Ns" block once
// per turn: a blank separator, an indented dim line aligned with the
// answer body, and a trailing blank line before the answer.
func (a *App) printThoughtLine(started time.Time) {
	if a.thoughtLineShown {
		return
	}
	clause := ""
	if !started.IsZero() {
		clause = " for " + formatThoughtDuration(time.Since(started))
	}
	note := ""
	if len(a.browseNotes) > 0 {
		note = ", " + strings.Join(a.browseNotes, ", ")
	}
	a.printf("\n%s  Thought%s%s (ctrl+o to expand)%s\n\n", cDim, clause, note, cReset)
	a.thoughtLineShown = true
}

// ensureThoughtLine makes sure every answered turn carries at least
// one permanent Thought line even when the server sent no reasoning
// block; its duration falls back to the elapsed turn time.
func (a *App) ensureThoughtLine() {
	if a.thoughtLineShown {
		return
	}
	a.printThoughtLine(a.turnStart)
}

// turnEndLine renders the post-turn summary line, including the
// number of tool steps whose details were erased with the live
// region. A think-only turn "cogitated"; a turn that ran tools
// "worked".
func turnEndLine(end time.Time, elapsed time.Duration, tools int) string {
	verb := "Cogitated"
	toolPart := ""
	if tools > 0 {
		verb = "Worked"
		toolPart = fmt.Sprintf(" · %d tools", tools)
	}
	return fmt.Sprintf("%s✻ %s for %s%s · done %s%s",
		cDim, verb, formatThoughtDuration(elapsed), toolPart, end.Format("3:04 PM"), cReset)
}

// finishTool settles one completed tool call: browsing actions fold
// into the next Thought line, everything else prints its permanent
// block above the live region. Results that arrive before their call
// event are deferred until the call shows up.
func (a *App) finishTool(ev loop.Event) {
	call := a.takeToolCallForEvent(ev)
	if call.name == "" {
		a.foldMu.Lock()
		if len(a.deferredTools) > 64 {
			a.deferredTools = nil
		}
		a.deferredTools = append(a.deferredTools, ev)
		a.foldMu.Unlock()
		return
	}
	a.settleToolCall(call, ev)
}

// takeToolCallForEvent pulls the remembered call metadata for a
// result event, matching by call id first and by tool name second.
func (a *App) takeToolCallForEvent(ev loop.Event) pendingTool {
	name := ev.ToolName
	if ev.ToolCall != nil && ev.ToolCall.Name != "" {
		name = ev.ToolCall.Name
	}
	callID := ""
	if ev.ToolCall != nil {
		callID = ev.ToolCall.ID
	}
	return a.takeToolCall(callID, name)
}

// settleToolCall renders one completed call into its permanent block.
func (a *App) settleToolCall(call pendingTool, ev loop.Event) {
	name := ev.ToolName
	if name == "" {
		name = "tool"
	}
	if call.name != "" {
		name = call.name
	}
	if isBrowseTool(name) {
		a.finishActivity("")
		a.addBrowseNote(name, ev.Result)
		return
	}
	if webToolKind(name) == "" {
		a.finishActivity("")
	}
	block := a.buildToolBlock(name, call.args, ev.Result, ev.IsError, call.start, ev.Time)
	if len(block.fold) > 0 {
		a.registerFold(name, block.fold)
	}
	a.addToolCount(1)
	a.printToolBlock(block)
}

// printToolBlock writes the header, ⎿ row and preview above the live
// region (or straight through when no panel is active).
func (a *App) printToolBlock(b toolBlock) {
	var sb strings.Builder
	sb.WriteString(b.header)
	sb.WriteString("\n")
	for _, l := range b.lines {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	a.printf("%s", sb.String())
}

// addBrowseNote records a one-line summary of a browsing action so it
// can be appended to the surrounding Thought line.
func (a *App) addBrowseNote(name, result string) {
	note := browseSummary(name, result)
	if note == "" {
		return
	}
	a.foldMu.Lock()
	a.browseNotes = append(a.browseNotes, note)
	a.foldMu.Unlock()
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

// replay reprints a stored transcript with the same permanent blocks
// the live UI produces: highlighted user messages, the Thought line,
// tool blocks (with Ctrl+O folds), the answer and the summary.
func (a *App) replay(evs []loop.Event) {
	var turnStart, thoughtStart time.Time
	var thoughtBuf strings.Builder
	var browseNotes []string
	thoughtShown := false
	flushReplayThought := func(end time.Time) {
		if thoughtShown {
			return
		}
		body := strings.Split(strings.TrimSpace(thoughtBuf.String()), "\n")
		if len(body) == 1 && body[0] == "" {
			body = nil
		}
		if len(body) > 0 {
			a.registerFold("reasoning", body)
		}
		started := thoughtStart
		if started.IsZero() {
			started = turnStart
		}
		note := ""
		if len(browseNotes) > 0 {
			note = ", " + strings.Join(browseNotes, ", ")
		}
		if !end.IsZero() && !started.IsZero() {
			a.printf("\n%s  Thought for %s%s (ctrl+o to expand)%s\n\n",
				cDim, formatThoughtDuration(end.Sub(started)), note, cReset)
		} else {
			a.printf("\n%s  Thought%s (ctrl+o to expand)%s\n\n", cDim, note, cReset)
		}
		thoughtShown = true
	}
	for _, ev := range evs {
		switch ev.Kind {
		case loop.EvUserMessage:
			turnStart = ev.Time
			thoughtStart = time.Time{}
			thoughtBuf.Reset()
			browseNotes = nil
			thoughtShown = false
			a.setToolCount(0)
			width, _ := cachedTermSize()
			a.printf("%s\n", highlightMessageRows("❯ ", ev.Content, width, a.color))
		case loop.EvAssistantThinking:
			if thoughtStart.IsZero() {
				thoughtStart = ev.Time
			}
			thoughtBuf.WriteString(strings.TrimSpace(ev.Content))
			thoughtBuf.WriteByte('\n')
		case loop.EvAssistantChunk:
		case loop.EvAssistantMessage:
			if strings.TrimSpace(ev.Content) != "" {
				flushReplayThought(ev.Time)
				a.answerAnchorPrinted = false
				a.renderAnswer(ev.Content)
			}
		case loop.EvToolCall:
			a.rememberToolCall(ev.ToolCall, ev.Time)
		case loop.EvToolResult:
			call := a.takeToolCallForEvent(ev)
			if call.name == "" {
				a.foldMu.Lock()
				a.deferredTools = append(a.deferredTools, ev)
				a.foldMu.Unlock()
				continue
			}
			name := ev.ToolName
			if call.name != "" {
				name = call.name
			}
			if isBrowseTool(name) {
				if n := browseSummary(name, ev.Result); n != "" {
					browseNotes = append(browseNotes, n)
				}
			} else {
				block := a.buildToolBlock(name, call.args, ev.Result, ev.IsError, call.start, ev.Time)
				if len(block.fold) > 0 {
					a.registerFold(name, block.fold)
				}
				a.addToolCount(1)
				a.printToolBlock(block)
			}
		case loop.EvTurnEnd:
			a.flushDeferredToolResults()
			flushReplayThought(ev.Time)
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
