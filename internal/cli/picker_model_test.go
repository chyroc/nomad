package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestPickerModel(items []pickItem, multi bool) *pickerModel {
	p := newPickerFull(nil, nil, items, 0, "Title", "subtitle", nil, 0)
	if multi {
		p.withMultiSelect(nil)
	}
	return newPickerModel(p, 120, 24)
}

func key(t rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{t}}
}

func TestPickerModelFilterAndSelection(t *testing.T) {
	items := []pickItem{
		{id: "a", label: "alpha"},
		{id: "b", label: "beta"},
		{id: "c", label: "gamma"},
	}
	m := newTestPickerModel(items, false)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.sel != 2 {
		t.Fatalf("sel=%d want 2", m.sel)
	}
	m.Update(key('g'))
	if len(m.vis) != 1 || m.vis[0].id != "c" {
		t.Fatalf("filter 'g' vis=%+v want gamma only", m.vis)
	}
	if m.sel != 0 {
		t.Fatalf("sel after refilter=%d want 0", m.sel)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.vis) != 3 || m.sel != 0 {
		t.Fatalf("backspace vis=%d sel=%d", len(m.vis), m.sel)
	}
	m.Update(key('z'))
	m.Update(key('z'))
	if len(m.vis) != 0 {
		t.Fatalf("filter 'zz' vis=%d want 0", len(m.vis))
	}
	if !strings.Contains(m.View(), "(no matches)") {
		t.Fatalf("view missing no-matches row:\n%s", m.View())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.done {
		t.Fatalf("enter on empty list must not confirm")
	}
}

func TestPickerModelConfirmSingle(t *testing.T) {
	items := []pickItem{{id: "a", label: "alpha"}, {id: "b", label: "beta"}}
	m := newTestPickerModel(items, false)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.done || m.result.id != "b" || !m.result.confirmed {
		t.Fatalf("result=%+v done=%v", m.result, m.done)
	}
	if m.View() != "" {
		t.Fatalf("final view must be empty")
	}
}

func TestPickerModelCancel(t *testing.T) {
	m := newTestPickerModel([]pickItem{{id: "a", label: "alpha"}}, false)
	cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil || !m.done || m.result.confirmed {
		t.Fatalf("esc must cancel, result=%+v", m.result)
	}
	m2 := newTestPickerModel([]pickItem{{id: "a", label: "alpha"}}, false)
	if cmd := m2.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || !m2.done {
		t.Fatalf("ctrl+c must cancel")
	}
}

func TestPickerModelMultiToggle(t *testing.T) {
	items := []pickItem{
		{id: "a", label: "alpha"},
		{id: "b", label: "beta"},
		{id: "c", label: "gamma"},
	}
	m := newTestPickerModel(items, true)
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if !m.checked["a"] || !m.checked["b"] || m.checked["c"] {
		t.Fatalf("checked=%v", m.checked)
	}
	m.handleKey(key('*'))
	if !m.checked["a"] || !m.checked["b"] || !m.checked["c"] {
		t.Fatalf("toggle-all with partial selection should check all, checked=%v", m.checked)
	}
	m.handleKey(key('*'))
	if m.checked["a"] || m.checked["b"] || m.checked["c"] {
		t.Fatalf("toggle-all with everything checked should uncheck all, checked=%v", m.checked)
	}
	m.handleKey(key('*'))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.result.confirmed || strings.Join(m.result.ids, "") != "abc" {
		t.Fatalf("ids=%v want items order abc", m.result.ids)
	}
}

func TestPickerModelMultiSpaceDoesNotFilter(t *testing.T) {
	m := newTestPickerModel([]pickItem{{id: "a", label: "alpha"}}, true)
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if len(m.query) != 0 {
		t.Fatalf("space in multi must toggle, not filter, query=%q", string(m.query))
	}
}

func TestPickerModelSessionKey(t *testing.T) {
	items := []pickItem{{id: "a", label: "alpha"}, {id: "b", label: "beta"}}
	p := newPickerFull(nil, nil, items, 0, "Title", "", nil, 0).withSessionSave()
	m := newPickerModel(p, 120, 24)
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(key('s'))
	if !m.done || !m.result.session || m.result.id != "b" {
		t.Fatalf("session result=%+v", m.result)
	}
	m2 := newPickerModel(newPickerFull(nil, nil, items, 0, "Title", "", nil, 0), 120, 24)
	m2.handleKey(key('s'))
	if m2.done {
		t.Fatalf("'s' without session mode must filter, not confirm")
	}
	if string(m2.query) != "s" {
		t.Fatalf("query=%q", string(m2.query))
	}
}

func TestPickerModelEffortArrows(t *testing.T) {
	items := []pickItem{{id: "a", label: "alpha"}}
	efforts := []string{"low", "medium", "high"}
	p := newPickerFull(nil, nil, items, 0, "Title", "", efforts, 1)
	m := newPickerModel(p, 120, 24)
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.effort != 2 {
		t.Fatalf("right effort=%d want 2", m.effort)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.effort != 2 {
		t.Fatalf("effort must clamp at max, got %d", m.effort)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if m.effort != 0 {
		t.Fatalf("left effort=%d want 0", m.effort)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.result.effortIdx != 0 {
		t.Fatalf("effortIdx=%d", m.result.effortIdx)
	}
}

func TestPickerModelPasteIgnored(t *testing.T) {
	m := newTestPickerModel([]pickItem{{id: "a", label: "alpha"}}, false)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("zz")})
	if len(m.query) != 0 {
		t.Fatalf("paste must not reach the filter query, query=%q", string(m.query))
	}
}

func TestPickerPanelRowsMatchesView(t *testing.T) {
	items := make([]pickItem, 30)
	for i := range items {
		items[i] = pickItem{id: string(rune('a' + i)), label: string(rune('a' + i))}
	}
	m := newTestPickerModel(items, false)
	if got := strings.Count(m.View(), "\n") + 1; got != m.panelRows() {
		t.Fatalf("view lines=%d panelRows=%d", got, m.panelRows())
	}
	m.Update(key('z'))
	m.Update(key('z'))
	if got := strings.Count(m.View(), "\n") + 1; got != m.panelRows() {
		t.Fatalf("empty view lines=%d panelRows=%d", got, m.panelRows())
	}
}
