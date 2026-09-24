package cli

import (
	"github.com/chyroc/nomad/internal/store"
)

// pickSession shows a fuzzy single-select over local transcripts.
func (a *App) pickSession(transcript *store.SessionStore) (string, bool) {
	recs, err := transcript.List()
	if err != nil || len(recs) == 0 {
		a.printf("%s(no local transcripts)%s\n", cDim, cReset)
		return "", false
	}
	items := make([]pickItem, 0, len(recs))
	for _, r := range recs {
		title := r.Title
		if title == "" {
			title = "(empty)"
		}
		items = append(items, pickItem{
			id:    r.ID,
			label: title,
			desc:  r.UpdatedAt.Format("2006-01-02 15:04") + "  " + r.ID,
		})
	}
	pk := newPickerFull(a.in, a.out, items, 0,
		"Resume session",
		"Select a transcript to attach to. Enter resumes, Esc cancels.", nil, 0)
	id, ok := pk.Run()
	return id, ok
}
