package cli

import (
	"fmt"
	"time"
)

// formatTurnDuration renders an elapsed turn duration compactly.
func formatTurnDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return d.String()
	}
	return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
}

func (a *App) elapsedTurn() time.Duration {
	a.turnMu.Lock()
	start := a.turnStart
	a.turnMu.Unlock()
	if start.IsZero() {
		return 0
	}
	return time.Since(start)
}
