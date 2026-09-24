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
	if !strings.Contains(m2.hintRow(), "/model") {
		t.Fatalf("hintRow=%q", m2.hintRow())
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
	got, err := ed.ReadLine("> ")
	if err != nil || got != "hello" {
		t.Fatalf("ReadLine=%q err=%v", got, err)
	}
	got, err = ed.ReadLine("> ")
	if err != nil || got != "world" {
		t.Fatalf("ReadLine2=%q err=%v", got, err)
	}
}

func TestEditorHeadlessDoesNotTouchHistory(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/history"
	ed := newLineEditor(strings.NewReader("first\n"), io.Discard, nil, path)
	if _, err := ed.ReadLine("> "); err != nil {
		t.Fatal(err)
	}
	if loaded := loadHistory(path); len(loaded) != 0 {
		t.Fatalf("headless read must not write history, got %v", loaded)
	}
}
