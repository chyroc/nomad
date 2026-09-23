package cli

import (
	"sync"

	"github.com/charmbracelet/glamour"
)

var (
	mdMu        sync.Mutex
	mdRenderer  *glamour.TermRenderer
	mdWidth     int
	mdColorized bool
)

func renderMarkdown(text string, width int, colorized bool) string {
	if width < 20 {
		width = 80
	}
	r := markdownRenderer(width, colorized)
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return out
}

func markdownRenderer(width int, colorized bool) *glamour.TermRenderer {
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdRenderer != nil && mdWidth == width && mdColorized == colorized {
		return mdRenderer
	}
	style := "dark"
	if !colorized {
		style = "notty"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width-2),
	)
	if err != nil {
		return mdRenderer
	}
	mdRenderer, mdWidth, mdColorized = r, width, colorized
	return r
}
