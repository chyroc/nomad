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
		io.WriteString(a.out, "\x1b[?2004h")
		defer io.WriteString(a.out, "\x1b[?2004l")
	}
	a.editor = newLineEditor(a.in, a.out, nil)
	a.editor.setCompleter(a.completeSlash)
	a.editor.onMouse = func(button, x, y int) bool {
		if button < 0 {
			a.expandLatestFold()
			return true
		}
		if id, ok := a.foldAtScreenY(y); ok {
			a.expandFold(id)
			return true
		}
		a.expandLatestFold()
		return false
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

	a.startSpinner()
	a.lastAnswer = ""
	a.thinkingBuf.Reset()
	a.thinkingStart = time.Time{}
	turnCtx, cancel := context.WithCancel(ctx)
	a.turnMu.Lock()
	a.turnCancel = cancel
	a.turnMu.Unlock()

	err := a.runner.Run(turnCtx, text, atts)

	cancel()
	a.turnMu.Lock()
	a.turnCancel = nil
	a.turnMu.Unlock()
	a.stopSpinner()
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
	case loop.EvAssistantChunk:
		a.stopSpinner()
		a.flushThinking()
		a.lastAnswer += ev.Content
	case loop.EvAssistantMessage:
		a.stopSpinner()
		a.flushThinking()
		if text := strings.TrimSpace(ev.Content); text != "" {
			if a.color {
				width, _ := cachedTermSize()
				a.printf("%s\n", renderMarkdown(text, width, true))
			} else {
				a.printf("%s\n\n", text)
			}
		}
	case loop.EvToolCall:
		a.stopSpinner()
		a.flushThinking()
		if ev.ToolCall != nil {
			args := oneLine(ev.ToolCall.Arguments, 100)
			if args == "{}" || args == "" {
				a.printf("%s %s%s\n", a.style(cCyan, "⚙"), a.style(cBold, ev.ToolCall.Name), cReset)
			} else {
				a.printf("%s %s(%s)%s\n", a.style(cCyan, "⚙"), a.style(cBold, ev.ToolCall.Name), a.style(cDim, args), cReset)
			}
		}
	case loop.EvToolResult:
		a.flushThinking()
		a.renderToolResult(ev)
	case loop.EvTurnEnd:
		a.flushThinking()
		if ev.Usage != nil && (ev.Usage.InputTokens > 0 || ev.Usage.OutputTokens > 0) {
			a.printf("%s tokens %d↑ %d↓%s\n", cDim, ev.Usage.InputTokens, ev.Usage.OutputTokens, cReset)
		}
	case loop.EvError:
		a.stopSpinner()
		a.flushThinking()
		a.printf("%s%v%s\n", cRed, ev.Content, cReset)
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
	lines := strings.Split(body, "\n")
	id := a.registerFold("thinking", lines)
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
	a.emitClickableLine2(id, bar)
}

func (a *App) renderToolResult(ev loop.Event) {
	color := cDim
	if ev.IsError {
		color = cRed
	}
	body := strings.TrimSpace(ev.Result)
	if body == "" {
		body = "(no output)"
	}
	header := ev.ToolName

	head, tail, more, folded := foldLines(header, body)
	a.emitLine("  └─ "+header+":", cDim)
	if !folded {
		for _, l := range head {
			a.emitLine("    "+l, color)
		}
		return
	}
	for _, l := range head {
		a.emitLine("    "+l, color)
	}
	id := a.registerFold(header, strings.Split(body, "\n"))
	row := a.emitClickableLine("    " + foldBar(id, more))
	a.recordFoldRow(id, row)
	for _, l := range tail {
		a.emitLine("    "+l, color)
	}
}

func (a *App) emitLine(text, color string) {
	a.printf("%s%s%s\n", color, text, cReset)
	a.advanceRows(1)
}

func (a *App) emitClickableLine(text string) int {
	a.printf("%s\n", text)
	row := a.screenRow
	a.advanceRows(1)
	return row
}

func (a *App) emitClickableLine2(id int, text string) {
	a.printf("%s\n", text)
	row := a.screenRow
	a.advanceRows(1)
	a.recordFoldRow(id, row)
}

func (a *App) advanceRows(n int) {
	a.foldMu.Lock()
	a.screenRow += n
	a.foldMu.Unlock()
}

func (a *App) registerFold(header string, lines []string) int {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	if a.folds == nil {
		a.folds = map[int]*foldBlock{}
		a.foldRows = map[int]int{}
	}
	a.nextFoldID++
	id := a.nextFoldID
	a.folds[id] = &foldBlock{id: id, header: header, lines: lines}
	a.foldOrder = append(a.foldOrder, id)
	return id
}

func (a *App) recordFoldRow(id, row int) {
	a.foldMu.Lock()
	a.foldRows[row] = id
	a.foldMu.Unlock()
}

func (a *App) expandFold(id int) {
	a.foldMu.Lock()
	b := a.folds[id]
	a.foldMu.Unlock()
	if b == nil {
		return
	}
	a.printf("%s  ┌─ expanded %s (%d lines)%s\n", cCyan, b.header, len(b.lines), cReset)
	a.advanceRows(1)
	for _, l := range b.lines {
		a.printf("%s  │ %s%s\n", cDim, l, cReset)
		a.advanceRows(1)
	}
	a.printf("%s  └──────────────%s\n", cCyan, cReset)
	a.advanceRows(1)
}

func (a *App) expandLatestFold() {
	a.foldMu.Lock()
	if len(a.foldOrder) == 0 {
		a.foldMu.Unlock()
		return
	}
	id := a.foldOrder[len(a.foldOrder)-1]
	a.foldMu.Unlock()
	a.expandFold(id)
}

func (a *App) foldAtScreenY(y int) (int, bool) {
	a.foldMu.Lock()
	defer a.foldMu.Unlock()
	for row, id := range a.foldRows {
		if row == y || row == y-1 {
			return id, true
		}
	}
	return 0, false
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
	a.stopSpinner()
	return a.askPermissionChoice(name, oneLine(args, 100))
}

func (a *App) startSpinner() {
	if a.spin == nil {
		a.spin = newSpinner(a.out, "thinking…")
	}
	a.spin.Start(a.color)
}

func (a *App) stopSpinner() {
	if a.spin != nil {
		a.spin.Stop()
	}
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
