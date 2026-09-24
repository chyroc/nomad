package settings

// Hook is one configured command for a lifecycle event.
type Hook struct {
	Matcher        string `json:"matcher,omitempty"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// Hooks maps event names (PreToolUse, PostToolUse, UserPromptSubmit,
// SessionStart, Stop) to configured hook commands.
type Hooks map[string][]Hook
