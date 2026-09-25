package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func newTestEditorModel(history []string) *editorModel {
	ti := textinput.New()
	ti.Prompt = "> "
	_ = ti.Focus()
	return &editorModel{
		ed:     &lineEditor{history: history, histIdx: len(history)},
		prompt: "> ",
		ti:     ti,
		width:  80,
	}
}

func (m *editorModel) typeText(s string) {
	for _, part := range strings.Split(s, "") {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(part)})
	}
}

func TestEditorModelTypingAndSubmit(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("hello world")
	if got := m.ti.Value(); got != "hello world" {
		t.Fatalf("value=%q", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.status != editorSubmitted {
		t.Fatalf("status=%v want editorSubmitted", m.status)
	}
	if got := m.fullText(); got != "hello world" {
		t.Fatalf("fullText=%q", got)
	}
	if m.echo != "> hello world" {
		t.Fatalf("echo=%q", m.echo)
	}
}

func TestEditorModelPasteChips(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("look:")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("line one\nline two")})
	if got := len(m.pasted); got != 1 {
		t.Fatalf("pasted=%d want 1", got)
	}
	if !strings.Contains(m.inputRow(), "[Pasted text +1 lines]") {
		t.Fatalf("inputRow missing chip: %q", m.inputRow())
	}
	m.typeText("done")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if want := "look:done\nline one\nline two"; m.fullText() != want {
		t.Fatalf("fullText=%q want %q", m.fullText(), want)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if m.echo != "> look:done\nline one\nline two" {
		t.Fatalf("echo=%q", m.echo)
	}
}

func TestEditorModelHistoryNavigation(t *testing.T) {
	m := newTestEditorModel([]string{"first", "second"})
	m.typeText("draft")
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.ti.Value(); got != "second" {
		t.Fatalf("up1=%q want second", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := m.ti.Value(); got != "first" {
		t.Fatalf("up2=%q want first", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.ti.Value(); got != "second" {
		t.Fatalf("down1=%q want second", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.ti.Value(); got != "draft" {
		t.Fatalf("down2=%q want saved draft", got)
	}
}

func TestEditorModelCtrlRSearch(t *testing.T) {
	m := newTestEditorModel([]string{"fix parser bug", "add parser test"})
	m.typeText("parser")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := m.ti.Value(); got != "add parser test" {
		t.Fatalf("search1=%q", got)
	}
	m.ed.searching = true
	m.ed.searchQuery = []rune("parser")
	m.ed.searchHit = 1
	m.setLine("parser")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := m.ti.Value(); got != "fix parser bug" {
		t.Fatalf("search2=%q want walk to older match", got)
	}
}

func TestEditorModelCtrlKeys(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("hello world")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	if got := m.ti.Position(); got != 0 {
		t.Fatalf("ctrl+a pos=%d", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlE})
	if got := m.ti.Position(); got != len("hello world") {
		t.Fatalf("ctrl+e pos=%d", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := m.ti.Value(); got != "hello " {
		t.Fatalf("ctrl+w=%q want %q", got, "hello ")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := m.ti.Value(); got != "" {
		t.Fatalf("ctrl+u=%q", got)
	}
}

func TestEditorModelWordMotionCJK(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("hello 世界 now")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'f'}})
	if got := m.ti.Position(); got != 5 {
		t.Fatalf("alt+f pos=%d want 5", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'f'}})
	if got := m.ti.Position(); got != 7 {
		t.Fatalf("alt+f2 pos=%d want 7 (one CJK char)", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Alt: true, Runes: []rune{'b'}})
	if got := m.ti.Position(); got != 6 {
		t.Fatalf("alt+b pos=%d want 6", got)
	}
}

func TestEditorModelCtrlDAndInterrupt(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("x")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.status != editorEditing {
		t.Fatalf("ctrl+d on non-empty should not quit, status=%v", m.status)
	}
	m2 := newTestEditorModel(nil)
	m2.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m2.status != editorEOF {
		t.Fatalf("ctrl+d on empty status=%v want editorEOF", m2.status)
	}
	m3 := newTestEditorModel(nil)
	m3.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m3.status != editorInterrupted {
		t.Fatalf("ctrl+c status=%v", m3.status)
	}
}

func TestEditorModelTabCompletion(t *testing.T) {
	complete := func(line string) []string {
		var out []string
		for _, c := range []string{"/help", "/model", "/memory"} {
			if strings.HasPrefix(c, line) {
				out = append(out, c)
			}
		}
		return out
	}
	m := newTestEditorModel(nil)
	m.ed.completer = complete
	m.typeText("/mod")
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := m.ti.Value(); got != "/model" {
		t.Fatalf("tab=%q", got)
	}
	hints := slashHints(complete, m.displayed())
	if len(hints) != 0 {
		t.Fatalf("unique completion should clear hints, got %v", hints)
	}
	m2 := newTestEditorModel(nil)
	m2.ed.completer = complete
	m2.typeText("/m")
	if hints := slashHints(complete, m2.displayed()); len(hints) != 2 {
		t.Fatalf("hints=%v want 2 entries", hints)
	}
	if !strings.Contains(m2.slashHintRow(), "/model") {
		t.Fatalf("hintRow=%q", m2.slashHintRow())
	}
}

func TestEditorModelSubmitAppliesSlashUnique(t *testing.T) {
	complete := func(line string) []string { return []string{"/model"} }
	m := newTestEditorModel(nil)
	m.ed.completer = complete
	m.typeText("/mod")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.echo != "> /model" {
		t.Fatalf("echo=%q want > /model", m.echo)
	}
}

func TestEditorModelBackspacePopsChip(t *testing.T) {
	m := newTestEditorModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("a\nb")})
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.pasted) != 0 {
		t.Fatalf("chip not popped, pasted=%v", m.pasted)
	}
}

func TestEditorModelCtrlVSwallowed(t *testing.T) {
	m := newTestEditorModel(nil)
	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Fatalf("ctrl+v should be swallowed, got cmd %v", cmd)
	}
	if m.ti.Value() != "" {
		t.Fatalf("ctrl+v must not alter value, got %q", m.ti.Value())
	}
}

func TestEditorHeadlessReadLine(t *testing.T) {
	ed := newLineEditor(strings.NewReader("hello\nworld\n"), io.Discard, nil, "")
	got, err := ed.ReadLine("> ", readLineOptions{})
	if err != nil || got != "hello" {
		t.Fatalf("ReadLine=%q err=%v", got, err)
	}
	got, err = ed.ReadLine("> ", readLineOptions{})
	if err != nil || got != "world" {
		t.Fatalf("ReadLine2=%q err=%v", got, err)
	}
}

func TestEditorModelFrameAssembly(t *testing.T) {
	m := newTestEditorModel(nil)
	m.frame = inputFrame{top: "TOP", rows: []string{"RULE", "STATUS1", "STATUS2"}}
	m.typeText("hi")
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 4 {
		t.Fatalf("view lines=%d want 4 (input + rule + 2 status): %q", len(lines), view)
	}
	if !strings.Contains(lines[0], "hi") {
		t.Fatalf("first row should be the input row: %q", lines[0])
	}
	if lines[1] != "RULE" || lines[2] != "STATUS1" || lines[3] != "STATUS2" {
		t.Fatalf("pinned rows out of order: %q", view)
	}
}

func TestEditorModelFrameAssemblyWithTopRule(t *testing.T) {
	m := newTestEditorModel(nil)
	m.showTopRule = true
	m.frame = inputFrame{top: "TOP", rows: []string{"RULE", "STATUS1", "STATUS2"}}
	m.typeText("hi")
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 5 {
		t.Fatalf("view lines=%d want 5 (top + input + rule + 2 status): %q", len(lines), lines)
	}
	if lines[0] != "TOP" {
		t.Fatalf("first row should be the top rule: %q", lines[0])
	}
	if !strings.Contains(lines[1], "hi") {
		t.Fatalf("second row should be the input: %q", lines[1])
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	echoLines := strings.Split(m.View(), "\n")
	if echoLines[0] != "TOP" || !strings.Contains(echoLines[1], "hi") {
		t.Fatalf("submitted echo should keep the top rule: %q", echoLines)
	}
}

func TestEditorModelHintSitsAboveInput(t *testing.T) {
	complete := func(line string) []string { return []string{"/help"} }
	m := newTestEditorModel(nil)
	m.ed.completer = complete
	m.frame = inputFrame{rows: []string{"RULE", "S1", "S2"}}
	m.typeText("/he")
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 5 {
		t.Fatalf("view lines=%d want 5 (hint + input + rule + 2 status): %q", len(lines), view)
	}
	if !strings.Contains(lines[0], "/help") {
		t.Fatalf("slash hint should render above input: %q", lines[0])
	}
	if lines[1] == "" || !strings.Contains(lines[1], "/he") {
		t.Fatalf("second row should be the input: %q", lines[1])
	}
	if lines[2] != "RULE" || lines[3] != "S1" || lines[4] != "S2" {
		t.Fatalf("pinned rows out of order: %q", view)
	}
}

func TestEditorModelCycleModeRefreshesFrame(t *testing.T) {
	m := newTestEditorModel(nil)
	m.frame = inputFrame{rows: []string{"RULE", "old", "mode"}}
	cycled := false
	m.ed.renderFrame = func(width int) inputFrame {
		return inputFrame{rows: []string{"RULE", "new", "mode"}}
	}
	m.ed.onCycleMode = func() { cycled = true }
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !cycled {
		t.Fatalf("shift+tab should invoke the cycle callback")
	}
	lines := strings.Split(m.View(), "\n")
	if lines[2] != "new" {
		t.Fatalf("frame should refresh after mode cycle: %q", strings.Join(lines, "\n"))
	}
}

func TestEditorHeadlessDoesNotTouchHistory(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/history"
	ed := newLineEditor(strings.NewReader("first\n"), io.Discard, nil, path)
	if _, err := ed.ReadLine("> ", readLineOptions{}); err != nil {
		t.Fatal(err)
	}
	if loaded := loadHistory(path); len(loaded) != 0 {
		t.Fatalf("headless read must not write history, got %v", loaded)
	}
}

func TestEditorModelBlankEnterContinuesWhenConfigured(t *testing.T) {
	m := newTestEditorModel(nil)
	m.blankContinues = true
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.status != editorEditing {
		t.Fatalf("blank enter should keep editing, status=%v", m.status)
	}
	m.typeText("go")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.status != editorSubmitted {
		t.Fatalf("non-blank enter should submit, status=%v", m.status)
	}
}

func TestEditorModelBlankEnterSubmitsByDefault(t *testing.T) {
	m := newTestEditorModel(nil)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.status != editorSubmitted {
		t.Fatalf("blank enter should submit without blankContinues, status=%v", m.status)
	}
}

func TestEditorModelEchoRowFullWidthHighlight(t *testing.T) {
	ti := textinput.New()
	ti.Prompt = "> "
	_ = ti.Focus()
	m := &editorModel{
		ed:     &lineEditor{color: true},
		prompt: "> ",
		ti:     ti,
		width:  20,
	}
	m.typeText("hello")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	row := m.echoRow()
	if !strings.Contains(row, cRowBg) {
		t.Fatalf("echo row should carry the row background: %q", row)
	}
	want := cDim + "> " + cReset + cRowBg + "hello" + strings.Repeat(" ", 13) + cReset
	if row != want {
		t.Fatalf("echo row=%q want %q", row, want)
	}
}

func TestEditorModelEchoRowNoColor(t *testing.T) {
	m := newTestEditorModel(nil)
	m.typeText("hello")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.echoRow(); got != "> hello" {
		t.Fatalf("plain echo row=%q want > hello", got)
	}
}
