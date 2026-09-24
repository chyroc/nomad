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
	a.editor = newLineEditor(a.in, a.out, nil)
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
	if evs, err := transcript.Load(remoteID); err == nil {
		a.replay(evs)
	}
	return nil
}

func (a *App) turn(ctx context.Context, transcript *store.SessionStore, text string, atts []loop.Attachment) error {
	if a.runner == nil {
		runner, err := a.newRunner(ctx, "", a.askToolPermission)
		if err != nil {
			return err
		}
		a.setRunner(runner)
		a.sessionID = runner.SessionID()
	}

	a.startActivity("Working…")
	a.lastAnswer = ""
	a.assistantStreamed = false
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

// beginTool renders the cc-style "⏺ Name(args)" invocation line and a
// running spinner while the tool executes.
func (a *App) beginTool(name, arguments string) {
	a.toolName = name
	a.toolArgs = arguments
	display := toolInvocation(name, arguments, 90)
	a.printf("%s %s%s\n", a.style(cPurple, "⏺"), a.style(cBold, name), display)
	a.startActivity(a.style(cPurple, "⠿") + " " + a.style(cDim, "Running "+name+"…"))
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
	a.printf("%s %s %s%s\n", color, okMark, a.style(cBold, ev.ToolName), cReset)
	head, tail, more, folded := foldLines(ev.ToolName, body)
	if !folded {
		for _, l := range head {
			a.printf("%s  %s%s\n", color, l, cReset)
		}
		return
	}
	for _, l := range head {
		a.printf("%s  %s%s\n", color, l, cReset)
	}
	a.registerFold(ev.ToolName, strings.Split(body, "\n"))
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
	lines := strings.Split(body, "\n")
	a.registerFold("reasoning", lines)
	dur := ""
	if !a.thinkingStart.IsZero() {
		dur = " · " + time.Since(a.thinkingStart).Round(time.Second).String()
	}
	a.thinkingStart = time.Time{}
	preview := strings.TrimSpace(lines[0])
	if len([]rune(preview)) > 60 {
		preview = string([]rune(preview)[:60]) + "…"
	}
	bar := fmt.Sprintf("  %s✦ thought %d lines%s  — %s  (Ctrl+O)%s",
		cDim, len(lines), dur, preview, cReset)
	a.printf("%s\n", bar)
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
	for _, ev := range evs {
		switch ev.Kind {
		case loop.EvUserMessage:
			a.printf("%s %s\n", a.style(cBold, ">"), ev.Content)
		case loop.EvAssistantMessage:
			if strings.TrimSpace(ev.Content) != "" {
				a.printf("%s\n\n", ev.Content)
			}
		case loop.EvToolCall:
			if ev.ToolCall != nil {
				a.printf("%s %s(%s)%s\n", a.style(cCyan, "⚙"), a.style(cBold, ev.ToolCall.Name),
					a.style(cDim, oneLine(ev.ToolCall.Arguments, 100)), cReset)
			}
		}
	}
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
		"/help", "/clear", "/copy", "/model", "/effort", "/status", "/cost", "/resume", "/sessions",
		"/session", "/skills", "/memory", "/permissions", "/config", "/init",
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
