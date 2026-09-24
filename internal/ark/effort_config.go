package ark

import "strings"

// effortBaseConfig is the authoritative deep-thinking capability table
// transcribed from the Ark documentation
// (https://console.volcengine.com/ark/.../deep-thinking). The catalog
// metadata (ArkModels reasoning_effort.support_value) is known to drift
// from what the managed-agent override endpoint actually accepts, so we
// consult this table first and only fall back to the live model list
// when a model is not listed here.
type effortBaseConfig struct {
	// patterns are runtime-id/model-name substrings (lowercase); a model
	// matches if any is contained in its id.
	patterns []string
	// defaultEffort is the server default; used as the answer when the
	// user asks for the default rather than a specific level.
	defaultEffort string
	// accepts is the canonical set of effort levels sent verbatim.
	accepts map[string]bool
	// aliases maps any of the seven UI levels to the wire value, for
	// inputs that the model folds onto another level (e.g. max -> high).
	aliases map[string]string
	// offLevel is the wire value that disables thinking ("none" is a
	// real level on some models, others only accept "minimal").
	offLevel string
	// thinkingSwitch reports whether thinking.type enabled/disabled is
	// supported (false for always-on models like glm-5-3-flash).
	thinkingSwitch bool
}

// nil means "send none/minimal to turn thinking off"; map lookups below
// handle each model's exact wire spelling.
var effortBaseTable = []effortBaseConfig{
	{
		patterns:       []string{"doubao-seed-evolving", "doubao-seed-2-1-pro-260915", "doubao-seed-2-1-lite-260915", "doubao-seed-2-1-pro-260628", "doubao-seed-2-1-turbo-260628"},
		defaultEffort:  "high",
		accepts:        effortSet("minimal", "low", "medium", "high"),
		offLevel:       "minimal",
		thinkingSwitch: true,
		aliases: map[string]string{
			"none":  "minimal",
			"xhigh": "high",
			"max":   "high",
		},
	},
	{
		patterns:       []string{"doubao-seed-2-0-lite-260428", "doubao-seed-2-0-mini-260428", "doubao-seed-2-0-pro-260215", "doubao-seed-2-0-lite-260215", "doubao-seed-2-0-mini-260215", "doubao-seed-2-0-code-preview-260215", "doubao-seed-character-260628"},
		defaultEffort:  "medium",
		accepts:        effortSet("minimal", "low", "medium", "high"),
		offLevel:       "minimal",
		thinkingSwitch: true,
		aliases: map[string]string{
			"none":  "minimal",
			"xhigh": "high",
			"max":   "high",
		},
	},
	{
		patterns:       []string{"glm-5-3-flash"},
		defaultEffort:  "max",
		accepts:        effortSet("low", "high", "max"),
		thinkingSwitch: false, // always on, cannot be disabled
		aliases: map[string]string{
			"none":    "low",
			"minimal": "low",
			"medium":  "high",
			"xhigh":   "max",
		},
	},
	{
		patterns:       []string{"glm-5-2"},
		defaultEffort:  "high",
		accepts:        effortSet("minimal", "high", "max"),
		offLevel:       "minimal",
		thinkingSwitch: true,
		aliases: map[string]string{
			"none":    "minimal",
			"low":     "high",
			"medium":  "high",
			"xhigh":   "max",
		},
	},
	{
		patterns:       []string{"deepseek-v4-1-flash-260910"},
		defaultEffort:  "high",
		accepts:        effortSet("none", "low", "high", "xhigh"),
		offLevel:       "none",
		thinkingSwitch: true,
		aliases: map[string]string{
			"minimal": "low",
			"medium":  "high",
			"xhigh":   "high",
			"max":     "high", // doc only maps ultra->max; plain max is rejected
			"ultra":   "max",
		},
	},
	{
		patterns:       []string{"deepseek-v4-pro-ga", "deepseek-v4-flash-ga"},
		defaultEffort:  "high",
		accepts:        effortSet("minimal", "low", "high", "xhigh"),
		offLevel:       "minimal",
		thinkingSwitch: true,
		aliases: map[string]string{
			"none":   "minimal",
			"medium": "low",
			"max":    "xhigh",
		},
	},
	{
		patterns:       []string{"deepseek-v4-pro-260425"},
		defaultEffort:  "high",
		accepts:        effortSet("minimal", "low", "high", "xhigh"),
		offLevel:       "minimal",
		thinkingSwitch: true,
		aliases: map[string]string{
			"none": "minimal",
			"max":  "xhigh",
		},
	},
}

func effortSet(levels ...string) map[string]bool {
	m := make(map[string]bool, len(levels))
	for _, l := range levels {
		m[l] = true
	}
	return m
}

// lookupEffortBaseConfig finds the documented config for a model id.
// The longest matching family token wins, so a dated variant is not
// shadowed by a shorter prefix (e.g. deepseek-v4-pro-ga vs ...-260425).
func lookupEffortBaseConfig(modelID string) *effortBaseConfig {
	id := strings.ToLower(modelID)
	best := (*effortBaseConfig)(nil)
	bestLen := 0
	for i := range effortBaseTable {
		cfg := &effortBaseTable[i]
		for _, pat := range cfg.patterns {
			p := strings.ToLower(pat)
			if strings.Contains(id, p) && len(p) > bestLen {
				best, bestLen = cfg, len(p)
			}
		}
	}
	return best
}

// mapEffort resolves a requested UI level to the wire value the model
// accepts. Returns "" when the model should be called without a
// reasoning_effort parameter at all.
// EffectiveEffort applies the documented base mapping for a model and
// returns the wire value to send. ok is false when the model is not in
// the base table (caller should fall back to live metadata) and also
// false when "" means "omit the parameter".
func EffectiveEffort(modelID, requested string) (wire string, known bool) {
	cfg := lookupEffortBaseConfig(modelID)
	if cfg == nil {
		return "", false
	}
	return cfg.mapEffort(requested), true
}

// ModelEffortInfo describes one UI effort level after applying the
// documented mapping for a model.
type ModelEffortInfo struct {
	// Level is the UI ladder value (none/minimal/low/.../max).
	Level string
	// Wire is the value actually sent for this model.
	Wire string
	// Supported reports whether the model honors this level distinctly
	// (false when it is folded onto another level or has no effect).
	Supported bool
}

// EffortLadder is the canonical UI ordering.
var EffortLadder = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}

// EffortMenu returns per-level info for the model picker from the base
// table. Returns nil when the model is not documented.
func EffortMenu(modelID string) []ModelEffortInfo {
	cfg := lookupEffortBaseConfig(modelID)
	if cfg == nil {
		return nil
	}
	out := make([]ModelEffortInfo, 0, len(EffortLadder))
	for _, lvl := range EffortLadder {
		wire := cfg.mapEffort(lvl)
		out = append(out, ModelEffortInfo{
			Level:     lvl,
			Wire:      wire,
			Supported: cfg.accepts[lvl],
		})
	}
	return out
}

func (c *effortBaseConfig) mapEffort(requested string) string {
	r := strings.ToLower(strings.TrimSpace(requested))
	if r == "" {
		return c.defaultEffort
	}
	if v, ok := c.aliases[r]; ok {
		return v
	}
	if c.accepts[r] {
		return r
	}
	return c.defaultEffort
}
