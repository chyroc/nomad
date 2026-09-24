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
	"golang.org/x/term"
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
		// Clear any bracketed-paste state a previous (possibly crashed)
		// process left behind, then enable it for this run.
		io.WriteString(a.out, "\x1b[?2004l\x1b[?2004h")
		resetModes := func() { io.WriteString(a.out, "\x1b[?2004l") }
		defer resetModes()
		a.resetTerminalModes = resetModes
	}
	a.editor = newLineEditor(a.in, a.out, nil, a.paths.HistoryFile())
	a.restoreRawTerm = func() {
		if a.editor != nil && a.editor.rawState != nil && a.editor.fd >= 0 {
			term.Restore(a.editor.fd, a.editor.rawState)
		}
	}
	a.editor.setCompleter(a.completeSlash)
	a.editor.onMouse = func(button, x, y int) bool {
		_ = x
		_ = y
		a.expandLatestFold()
		return true
	}

	a.printf("%s◆ Nomad%s · %s · model %s · %s\n",
		a.style(cBold, ""), cReset, a.style(cGreen, "managed-agents"),
		a.style(cCyan, a.model), a.style(cDim, a.paths.Workspace))
	a.printf("%s /help for commands · /model to switch · Ctrl+C interrupt, twice to quit%s\n\n", cDim, cReset)

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		for range sigCh {
			if a.handleInterrupt() {
				if a.restoreRawTerm != nil {
					a.restoreRawTerm()
				}
				if a.resetTerminalModes != nil {
					a.resetTerminalModes()
				}
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
		prompt := a.style(cBold, "> ")
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
	a.finishActivity("")
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
			a.beginTool(ev.ToolCall.Name, ev.ToolCall.Arguments)
		}
	case loop.EvToolResult:
		a.finishTool(ev)
	case loop.EvTurnEnd:
		a.flushThinking()
		a.finishActivity("")
		elapsed := a.elapsedTurn()
		if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
			a.printf("%s%d tokens · ↑%d ↓%d · %s%s\n", cDim, ev.Usage.InputTokens+ev.Usage.OutputTokens, ev.Usage.InputTokens, ev.Usage.OutputTokens, formatTurnDuration(elapsed), cReset)
		} else if elapsed > 0 {
			a.printf("%s%s%s\n", cDim, formatTurnDuration(elapsed), cReset)
		}
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
	if !a.answerAnchorPrinted {
		a.printf("%s●%s\n", cPurple, cReset)
		a.answerAnchorPrinted = true
	}
	if a.color {
		width, _ := cachedTermSize()
		a.printf("%s\n", renderMarkdown(text, width, true))
		return
	}
	a.printf("%s\n\n", text)
}

// startActivity shows the global working indicator.
func (a *App) startActivity(label string) {
	if a.act == nil {
		a.act = newActivityLine(a.out, a.color)
	}
	a.act.Start(label)
}

func (a *App) setActivity(label string) {
	if a.act == nil {
		a.startActivity(label)
		return
	}
	a.act.SetLabel(label)
}

func (a *App) finishActivity(final string) {
	if a.act != nil {
		a.act.Finish(final, false)
	}
}

// beginTool renders the "⏺ Name(args)" invocation line, a diff preview
// for edits and a running spinner while the tool executes.
func (a *App) beginTool(name, arguments string) {
	a.renderToolCallLine(name, arguments)
	a.startActivity(a.style(cPurple, "⠿") + " " + a.style(cDim, "Running "+name+"…"))
}

// renderToolCallLine prints the invocation line and diff preview without
// starting a spinner, so it is reusable during transcript replay.
func (a *App) renderToolCallLine(name, arguments string) {
	a.toolName = name
	a.toolArgs = arguments
	display := toolInvocation(name, arguments, 90)
	a.printf("%s %s%s\n", a.style(cPurple, "⏺"), a.style(cBold, name), display)
	a.renderToolDiff(name, arguments)
}

// renderThinkingFold registers and prints one reasoning block as a
// collapsed fold.
func (a *App) renderThinkingFold(lines []string, started time.Time) {
	if len(lines) == 0 {
		return
	}
	a.registerFold("reasoning", lines)
	dur := ""
	if !started.IsZero() {
		dur = " · " + time.Since(started).Round(time.Second).String()
	}
	preview := strings.TrimSpace(lines[0])
	if len([]rune(preview)) > 60 {
		preview = string([]rune(preview)[:60]) + "…"
	}
	a.printf("  %s✦ thought %d lines%s  — %s  (Ctrl+O)%s\n",
		cDim, len(lines), dur, preview, cReset)
}

func (a *App) finishTool(ev loop.Event) {
	a.finishActivity("")
	okMark, color := "✓", cDim
	if ev.IsError {
		okMark, color = "✗", cRed
	}
	body := strings.TrimSpace(ev.Result)
	if body == "" {
		body = "(no output)"
	}
	lines := strings.Split(body, "\n")
	a.printf("%s %s %s%s\n", color, okMark, a.style(cBold, ev.ToolName), cReset)

	// Errors and tiny single-line results stay inline; successful
	// output (even a few lines) collapses to one expandable summary by
	// default, matching a compact tool transcript.
	if !ev.IsError && !isTinyResult(lines) {
		a.registerFold(ev.ToolName, lines)
		preview := strings.TrimSpace(lines[0])
		a.printf("%s  %d %s%s\n", cDim, len(lines), resultNoun(len(lines)),
			a.toolSummarySuffix(preview))
		return
	}
	a.renderFoldable(ev.ToolName, body, color)
}

// isTinyResult reports whether a result is short enough to show inline
// without collapsing: a single trimmed line under 80 columns.
func isTinyResult(lines []string) bool {
	if len(lines) != 1 {
		return false
	}
	return len([]rune(strings.TrimSpace(lines[0]))) <= 80
}

func resultNoun(n int) string {
	if n == 1 {
		return "line"
	}
	return "lines"
}

func (a *App) toolSummarySuffix(firstLine string) string {
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return ""
	}
	r := []rune(firstLine)
	if len(r) > 60 {
		firstLine = string(r[:60]) + "…"
	}
	return " — " + firstLine + " " + a.style(cDim, "(Ctrl+O)")
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

func (a *App) expandFold(id int) {
	a.foldMu.Lock()
	b := a.folds[id]
	a.foldSeen[id] = true
	a.foldMu.Unlock()
	if b == nil {
		return
	}
	a.printf("%s  ┌─ expanded %s (%d lines)%s\n", cCyan, b.header, len(b.lines), cReset)
	for _, l := range b.lines {
		a.printf("%s  │ %s%s\n", cDim, l, cReset)
	}
	a.printf("%s  └──────────────%s\n", cCyan, cReset)
}

// expandLatestFold expands the earliest not-yet-expanded fold, so
// repeated Ctrl+O walks folds top to bottom in scrollback order.
func (a *App) expandLatestFold() {
	a.foldMu.Lock()
	var id int
	for _, fid := range a.foldOrder {
		if !a.foldSeen[fid] {
			id = fid
			break
		}
	}
	a.foldMu.Unlock()
	if id != 0 {
		a.expandFold(id)
	}
}

func (a *App) replay(evs []loop.Event) {
	var thinking []string
	var turnStart time.Time
	flush := func() {
		if len(thinking) > 0 {
			a.renderThinkingFold(thinking, time.Time{})
			thinking = nil
		}
	}
	for _, ev := range evs {
		switch ev.Kind {
		case loop.EvUserMessage:
			flush()
			turnStart = ev.Time
			a.printf("%s %s\n", a.style(cBold, ">"), ev.Content)
		case loop.EvAssistantChunk:
		case loop.EvAssistantMessage:
			flush()
			if strings.TrimSpace(ev.Content) != "" {
				a.answerAnchorPrinted = false
				a.renderAnswer(ev.Content)
			}
		case loop.EvAssistantThinking:
			thinking = append(thinking, strings.TrimSpace(ev.Content))
		case loop.EvToolCall:
			flush()
			if ev.ToolCall != nil {
				a.renderToolCallLine(ev.ToolCall.Name, ev.ToolCall.Arguments)
			}
		case loop.EvToolResult:
			flush()
			name := ev.ToolName
			if name == "" {
				ev.ToolName = "tool"
			}
			a.finishTool(ev)
		case loop.EvTurnEnd:
			flush()
			line := ""
			if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
				line = fmt.Sprintf("%d tokens · ↑%d ↓%d",
					ev.Usage.InputTokens+ev.Usage.OutputTokens,
					ev.Usage.InputTokens, ev.Usage.OutputTokens)
			}
			if !turnStart.IsZero() && !ev.Time.IsZero() {
				if d := ev.Time.Sub(turnStart); d > 0 {
					if line != "" {
						line += " · "
					}
					line += formatTurnDuration(d)
				}
			}
			if line != "" {
				a.printf("%s%s%s\n", cDim, line, cReset)
			}
		case loop.EvError:
			flush()
			a.printf("%s● %v%s\n", cRed, ev.Content, cReset)
		}
	}
	flush()
}

// askToolPermission is the interactive permission callback (default mode).
func (a *App) askToolPermission(name, args string) string {
	a.finishActivity("")
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
