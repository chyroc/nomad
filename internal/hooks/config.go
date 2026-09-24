package hooks

// Config maps event names to their hook commands.
type Config map[string][]Hook

// ForEvent returns the hooks configured for an event name.
func (c Config) ForEvent(event string) []Hook {
	return c[event]
}
