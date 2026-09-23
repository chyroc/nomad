package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Options are the parsed command-line options.
type Options struct {
	Print        bool
	OutputFormat OutputFormat
	MaxTurns     int

	Resume    string
	Continue  bool
	SessionID string

	Model           string
	ReasoningEffort string
	EffortSet       bool
	PermissionMode  string
	AllowedTools    []string
	DisallowedTools []string
	SystemPrompt    string
	AppendSystem    bool
	AddDirs         []string
	Images          []string

	SyncSkills []string

	Verbose bool
	Debug   bool
	Version bool
	Help    bool

	Subcommand string

	PromptArgs []string
}

// ParseOptions parses argv (without program name), supporting both
// --flag and -flag, "--flag=value", boolean flags and repeatable values.
func ParseOptions(argv []string) (Options, error) {
	var o Options
	o.OutputFormat = FormatText
	o.MaxTurns = 0
	o.ReasoningEffort = "max"
	o.PermissionMode = "bypassPermissions"

	store := func(name, value string) error {
		switch normalize(name) {
		case "p", "print":
			o.Print = true
			if value != "" {
				o.PromptArgs = append(o.PromptArgs, value)
			}
		case "output-format":
			f := OutputFormat(value)
			if f != FormatText && f != FormatJSON && f != FormatStreamJSON {
				return fmt.Errorf("invalid --output-format %q", value)
			}
			o.OutputFormat = f
		case "input-format":
			if value != "text" {
				return fmt.Errorf("--input-format %q is not supported (only text)", value)
			}
		case "model":
			o.Model = value
		case "effort", "reasoning-effort":
			o.ReasoningEffort = value
			o.EffortSet = true
		case "resume":
			o.Resume = value
		case "c", "continue":
			o.Continue = true
		case "session-id":
			o.SessionID = value
		case "max-turns":
			n, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid --max-turns: %w", err)
			}
			o.MaxTurns = n
		case "permission-mode":
			switch value {
			case "default", "acceptEdits", "plan", "bypassPermissions":
			default:
				return fmt.Errorf("invalid --permission-mode %q", value)
			}
			o.PermissionMode = value
		case "allowed-tools", "allowedTools":
			o.AllowedTools = append(o.AllowedTools, splitList(value)...)
		case "disallowed-tools", "disallowedTools":
			o.DisallowedTools = append(o.DisallowedTools, splitList(value)...)
		case "system-prompt":
			o.SystemPrompt = value
			o.AppendSystem = false
		case "append-system-prompt":
			o.SystemPrompt = value
			o.AppendSystem = true
		case "add-dir":
			o.AddDirs = append(o.AddDirs, value)
		case "sync-skills":
			o.SyncSkills = append(o.SyncSkills, splitList(value)...)
		case "image":
			o.Images = append(o.Images, value)
		case "verbose":
			o.Verbose = boolValue(value, true)
		case "debug":
			o.Debug = boolValue(value, true)
		case "dangerously-skip-permissions":
			o.PermissionMode = "bypassPermissions"
		case "version":
			o.Version = boolValue(value, true)
		case "help", "h":
			o.Help = boolValue(value, true)
		default:
			return fmt.Errorf("unknown option --%s", name)
		}
		return nil
	}

	boolFlags := map[string]bool{
		"p": true, "print": true, "c": true, "continue": true,
		"verbose": true, "debug": true, "dangerously-skip-permissions": true,
		"version": true, "help": true, "h": true,
	}

	i := 0
	for i < len(argv) {
		arg := argv[i]
		switch {
		case !strings.HasPrefix(arg, "-") || arg == "-":
			if o.Subcommand == "" && (arg == "login" || arg == "logout") {
				o.Subcommand = arg
			} else {
				o.PromptArgs = append(o.PromptArgs, arg)
			}
			i++
		default:
			name := strings.TrimLeft(arg, "-")
			var value string
			hasValue := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				value = name[eq+1:]
				name = name[:eq]
				hasValue = true
			}
			if boolFlags[normalize(name)] && !hasValue {
				if err := store(name, ""); err != nil {
					return o, err
				}
				i++
				continue
			}
			if !hasValue {
				if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
					value = argv[i+1]
					hasValue = true
					i++
				}
			}
			if !hasValue {
				return o, fmt.Errorf("option --%s requires a value", name)
			}
			if err := store(name, value); err != nil {
				return o, err
			}
			i++
		}
	}
	if len(o.PromptArgs) > 0 && !o.Print {
		o.Print = true
	}
	if o.Resume != "" && o.Continue {
		return o, errors.New("--resume and --continue are mutually exclusive")
	}
	return o, nil
}

func normalize(name string) string {
	return strings.ReplaceAll(strings.TrimLeft(name, "-"), "_", "-")
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func boolValue(v string, def bool) bool {
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

// HelpText is the --help output.
func HelpText(version string) string {
	return `nomad ` + version + ` — a self-hosted coding agent on Volcengine Ark managed-agents

USAGE
  nomad [options] [prompt]          start interactive TUI (or headless with -p)
  nomad -p "task"                   run non-interactively and print the result
  nomad login | logout              sign in with browser OAuth / remove credentials

CORE OPTIONS
  -p, --print                       non-interactive mode
      --output-format text|json|stream-json
  -m, --model <id>                  model id (default: auto-provisioned)
      --resume <session-id>         resume a remote session
  -c, --continue                    resume the most recent session
      --session-id <id>             attach to an existing remote session
      --max-turns <n>               maximum agentic turns (headless)
      --permission-mode <mode>      default|acceptEdits|plan|bypassPermissions
      --dangerously-skip-permissions   alias for bypassPermissions
      --allowed-tools bash,read,...  tool allow list
      --disallowed-tools bash,...    tool deny list
      --system-prompt <text|@file>   replace the system prompt
      --append-system-prompt <text>  append to the system prompt
      --add-dir <dir>                grant an additional workspace dir
      --sync-skills <names>         consent to upload name+description of the
                                    given local skills and bind them (comma list);
                                    interactive: /skills sync
      --image <path|url>             attach an image (repeatable)
      --verbose                      verbose output (also enables stream messages)
      --version --help

TUI COMMANDS
  /help /clear /model /effort /status /cost /resume /sessions /skills /memory
  /permissions /config /init /login /logout /exit

`
}
