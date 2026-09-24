package settings

import "github.com/chyroc/nomad/internal/hooks"

// HookConfig merges hooks from user, project and compat documents into
// one event-indexed config.
func (s *Settings) HookConfig() hooks.Config {
	cfg := hooks.Config{}
	add := func(f *SourcedFile) {
		if f == nil {
			return
		}
		for event, hs := range f.File.Hooks {
			for _, h := range hs {
				cfg[event] = append(cfg[event], hooks.Hook{
					Matcher:        h.Matcher,
					Command:        h.Command,
					TimeoutSeconds: h.TimeoutSeconds,
				})
			}
		}
	}
	add(s.User)
	add(s.Project)
	for i := range s.Compat {
		add(&s.Compat[i])
	}
	return cfg
}
