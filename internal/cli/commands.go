package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chyroc/nomad/internal/ark"
	"github.com/chyroc/nomad/internal/store"
)

// handleCommand executes a TUI slash command. Returns (quit, error).
func (a *App) handleCommand(ctx context.Context, transcript *store.SessionStore, input string) (bool, error) {
	fields := strings.Fields(input)
	cmd := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(input, cmd))

	switch cmd {
	case "/exit", "/quit":
		a.closeRunner()
		return true, nil
	case "/help":
		a.printf("%s\n", commandHelp())
		return false, nil
	case "/clear", "/reset", "/new":
		a.closeRunner()
		a.sessionID = ""
		a.foldMu.Lock()
		a.folds = nil
		a.foldOrder = nil
		a.foldSeen = nil
		a.nextFoldID = 0
		a.foldMu.Unlock()
		a.thinkingBuf.Reset()
		a.thinkingStart = time.Time{}
		a.printf("%sStarted a new session (created on first message).%s\n\n", cDim, cReset)
		return false, nil
	case "/copy":
		if strings.TrimSpace(a.lastAnswer) == "" {
			a.printf("%snothing to copy yet%s\n", cDim, cReset)
		} else {
			_ = copyToClipboard(a.out, a.lastAnswer)
			a.printf("%scopied last reply (%d chars)%s\n", cGreen, len(a.lastAnswer), cReset)
		}
		return false, nil
	case "/session":
		a.printf("session: %s\nmodel: %s\nworkspace: %s\n",
			orDefault(a.sessionID, "(not started)"), a.model, a.paths.Workspace)
		return false, nil
	case "/sessions":
		recs, err := transcript.List()
		if err != nil {
			return false, err
		}
		if len(recs) == 0 {
			a.printf("(no local transcripts)\n")
			return false, nil
		}
		for _, r := range recs {
			marker := " "
			if r.ID == a.sessionID {
				marker = "*"
			}
			title := r.Title
			if title == "" {
				title = "(empty)"
			}
			a.printf("%s %s  %s %s\n", marker, shortID(r.ID),
				a.style(cDim, r.UpdatedAt.Format("2006-01-02 15:04")), title)
		}
		return false, nil
	case "/resume":
		if arg == "" {
			return false, fmt.Errorf("usage: /resume <remote-session-id>")
		}
		if err := a.attachSession(ctx, transcript, strings.TrimSpace(arg)); err != nil {
			return false, err
		}
		a.printf("%sResumed session %s%s\n", cGreen, arg, cReset)
		return false, nil
	case "/model":
		a.cmdModel(ctx, arg)
		return false, nil
	case "/effort":
		a.cmdEffort(ctx, arg)
		return false, nil
	case "/status":
		a.cmdStatus()
		return false, nil
	case "/cost", "/usage":
		a.cmdCost(transcript)
		return false, nil
	case "/skills":
		if strings.HasPrefix(input, "/skills sync") {
			rest := strings.TrimSpace(strings.TrimPrefix(input, "/skills sync"))
			return false, a.syncSkills(ctx, splitCSV(rest), false)
		}
		a.cmdSkills(arg)
		return false, nil
	case "/memory":
		a.cmdMemory(arg)
		return false, nil
	case "/init":
		return false, a.cmdInit()
	case "/add-dir":
		a.printf("%snote: nomad tools are scoped to the workspace root; restart in the target directory instead.%s\n", cDim, cReset)
		return false, nil
	case "/login":
		return false, a.runLogin(ctx)
	case "/logout":
		return false, a.runLogout()
	case "/permissions":
		a.printf("permission mode: %s\n", a.opts.PermissionMode)
		return false, nil
	case "/config":
		a.cmdConfig(arg)
		return false, nil
	default:
		return false, fmt.Errorf("unknown command %s (try /help)", cmd)
	}
}

func (a *App) cmdModel(ctx context.Context, arg string) {
	arg = strings.TrimSpace(arg)
	if arg != "" {
		a.model = arg
		_ = a.ctrl.SetModel(arg)
		a.closeRunner()
		a.printf("model: %s %s(new session on next message)%s\n",
			a.style(cCyan, arg), cDim, cReset)
		return
	}
	models, err := a.ctrl.Client.ListModels(ctx)
	if err != nil {
		a.printf("%slist models failed: %v%s\n", cRed, err, cReset)
		return
	}
	if len(models) == 0 {
		a.printf("(no models available)\n")
		return
	}
	items := make([]pickItem, 0, len(models))
	selIdx := 0
	for i, m := range models {
		items = append(items, pickItem{id: m.SelectID(), label: m.Name})
		if m.SelectID() == a.model {
			selIdx = i
		}
	}
	efforts := effortLadder
	effortIdx := effortIndex(efforts, a.opts.ReasoningEffort)
	pk := newPickerFull(a.in, a.out, items, selIdx,
		"Select model",
		"Switch the model and thinking effort. Enter saves as default, s applies to this session.",
		efforts, effortIdx).withSessionSave()
	res, ok := pk.RunFull()
	if !ok || res.id == "" {
		return
	}
	a.model = res.id
	if res.effortIdx < len(efforts) {
		a.opts.ReasoningEffort = efforts[res.effortIdx]
	}
	if !res.session {
		_ = a.ctrl.SetModel(res.id)
		_ = a.ctrl.SetEffort(a.opts.ReasoningEffort)
	}
	a.closeRunner()
	note := ""
	if supported := a.currentModelEfforts(ctx); len(supported) > 0 {
		if effective := ark.ResolveEffort(a.opts.ReasoningEffort, supported); effective != a.opts.ReasoningEffort {
			note = " · model will use " + effective
		}
	}
	a.printf("%smodel: %s · effort: %s%s%s\n", cGreen, res.id, a.opts.ReasoningEffort, note, cReset)
}

func effortIndex(efforts []string, current string) int {
	if current == "" {
		current = "max"
	}
	for i, e := range efforts {
		if e == current {
			return i
		}
	}
	return len(efforts) - 1
}

var effortLadder = []string{"low", "medium", "high", "xhigh", "max"}

func (a *App) currentModelEfforts(ctx context.Context) []string {
	models, err := a.ctrl.Client.ListModels(ctx)
	if err != nil {
		return nil
	}
	for _, m := range models {
		if m.SelectID() == a.model {
			return m.Efforts
		}
	}
	return nil
}

func (a *App) cmdEffort(ctx context.Context, arg string) {
	arg = strings.TrimSpace(arg)
	apply := func(level string, session bool) {
		a.opts.ReasoningEffort = level
		if !session {
			_ = a.ctrl.SetEffort(level)
		}
		a.closeRunner()
		scope := "default"
		if session {
			scope = "this session"
		}
		note := ""
		if supported := a.currentModelEfforts(ctx); len(supported) > 0 {
			if effective := ark.ResolveEffort(level, supported); effective != level {
				note = " · current model will use " + effective
			}
		}
		a.printf("%seffort: %s · %s%s%s\n", cGreen, level, scope, note, cReset)
	}
	if arg != "" {
		apply(arg, false)
		return
	}
	supported := map[string]bool{}
	for _, e := range a.currentModelEfforts(ctx) {
		supported[e] = true
	}
	items := make([]pickItem, 0, len(effortLadder))
	for _, e := range effortLadder {
		tag := ""
		if len(supported) > 0 && !supported[e] {
			tag = "unsupported by current model"
		}
		items = append(items, pickItem{id: e, label: e, tag: tag})
	}
	pk := newPickerFull(a.in, a.out, items, effortIndex(effortLadder, a.opts.ReasoningEffort),
		"Select thinking effort",
		"Depth of reasoning before answering. Enter saves as default, s applies to this session.",
		nil, 0).withSessionSave()
	res, ok := pk.RunFull()
	if !ok || res.id == "" {
		return
	}
	apply(res.id, res.session)
}

func (a *App) cmdStatus() {
	branch := gitBranch(a.paths.Workspace)
	account := "(unknown)"
	if a.ctrl != nil && a.ctrl.Creds != nil {
		account = firstNonEmpty(a.ctrl.Creds.ProjectName, a.ctrl.Creds.UserID, a.ctrl.Creds.AccountID, "api-key")
	}
	lines := [][2]string{
		{"Version", Version},
		{"Backend", "managed-agents (Volcengine Ark)"},
		{"Account", account},
		{"Model", a.model},
		{"Thinking effort", orDefault(a.opts.ReasoningEffort, "max")},
		{"Agent", a.ctrl.Profile.AgentID},
		{"Environment", a.ctrl.Profile.EnvironmentID},
		{"Session", orDefault(a.sessionID, "(not started)")},
		{"Permission mode", a.opts.PermissionMode},
		{"Workspace", a.paths.Workspace},
		{"Git branch", orDefault(branch, "(not a git repo)")},
		{"Skills dir", a.paths.SkillsDir()},
	}
	for _, kv := range lines {
		a.printf("%-16s %s\n", a.style(cDim, kv[0]), kv[1])
	}
}

func (a *App) cmdCost(transcript *store.SessionStore) {
	if a.sessionID == "" {
		a.printf("no active session\n")
		return
	}
	var in, out int
	if evs, err := transcript.Load(a.sessionID); err == nil {
		for _, ev := range evs {
			if ev.Kind == "turn_end" && ev.Usage != nil {
				in += ev.Usage.InputTokens
				out += ev.Usage.OutputTokens
			}
		}
	}
	a.printf("session %s\ntokens: %d input, %d output (cost: server-side accounting)\n",
		a.sessionID, in, out)
}

func (a *App) cmdSkills(arg string) {
	name := strings.TrimSpace(arg)
	if name == "sync" {
		a.printf("usage: /skills sync [skill-name,...] — select and upload skill metadata\n")
		return
	}
	if name != "" {
		for _, d := range a.discoverLocalSkills() {
			if d.Name == name {
				a.printf("%s\n", d.Body)
				return
			}
		}
		a.printf("%sno skill named %q%s\n", cRed, name, cReset)
		return
	}
	discovered := a.discoverLocalSkills()
	if len(discovered) == 0 {
		a.printf("(no skills) — add <name>/SKILL.md under ~/.claude/skills, ~/.agents/skills,\n ./.claude/skills, ./.agents/skills or %s\n", a.paths.SkillsDir())
		return
	}
	bound := map[string]bool{}
	for _, b := range a.ctrl.Profile.SkillBindings {
		bound[b.Name] = true
	}
	a.printf("%sLocal skills (use /skills sync to consent & upload metadata):%s\n", cBold, cReset)
	for _, d := range discovered {
		mark := " "
		if bound[d.Name] {
			mark = "*"
		}
		a.printf(" %s %-24s %s\n", mark, a.style(cCyan, "/"+d.Name), a.style(cDim, truncate(d.Description, 60)))
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) cmdMemory(arg string) {
	path := a.paths.MemoryFile()
	arg = strings.TrimSpace(arg)
	switch {
	case arg == "":
		if data, err := readFile(path); err == nil {
			a.printf("%s\n", data)
		} else {
			a.printf("(no global memory yet) — add content with /memory add <text>, or edit %s\n", path)
		}
	case strings.HasPrefix(arg, "add "):
		line := strings.TrimSpace(strings.TrimPrefix(arg, "add"))
		f, err := openAppend(path)
		if err != nil {
			a.printf("%s%v%s\n", cRed, err, cReset)
			return
		}
		fmt.Fprintf(f, "- %s\n", line)
		f.Close()
		a.printf("%sSaved to global memory.%s\n", cGreen, cReset)
	case arg == "edit":
		a.printf("edit %s in your editor, then restart the turn.\n", path)
	default:
		a.printf("usage: /memory [add <text>|edit]\n")
	}
}

func (a *App) cmdInit() error {
	path := "NOMAD.md"
	if _, err := readFile(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	content := "# Project instructions for nomad\n\n- Describe the build/test commands here.\n- Note conventions a coding agent should follow.\n"
	if err := writeFile(path, content); err != nil {
		return err
	}
	a.printf("%sCreated %s%s\n", cGreen, path, cReset)
	return nil
}

func (a *App) cmdConfig(arg string) {
	if strings.TrimSpace(arg) == "" {
		a.printf("model: %s\npermission: %s\neffort: %s\nworkspace: %s\n", a.model, a.opts.PermissionMode, a.opts.ReasoningEffort, a.paths.Workspace)
		a.printf("%sset values with /config <key=value> (supported: model, permission, effort)%s\n", cDim, cReset)
		return
	}
	kv := strings.SplitN(strings.TrimSpace(arg), "=", 2)
	var key, val string
	if len(kv) == 2 {
		key, val = strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
	} else if parts := strings.Fields(arg); len(parts) == 2 {
		key, val = parts[0], parts[1]
	} else {
		a.printf("%susage: /config model <id> | permission <mode> | effort <level>%s\n", cRed, cReset)
		return
	}
	switch key {
	case "model":
		a.model = val
		_ = a.ctrl.SetModel(a.model)
		a.printf("%smodel = %s%s\n", cGreen, val, cReset)
	case "permission":
		a.opts.PermissionMode = val
		a.printf("%spermission = %s%s\n", cGreen, val, cReset)
	case "effort", "reasoning_effort":
		a.opts.ReasoningEffort = val
		_ = a.ctrl.SetEffort(val)
		a.closeRunner()
		a.printf("%seffort = %s%s\n", cGreen, val, cReset)
	default:
		a.printf("%sunknown config key %q%s\n", cRed, key, cReset)
	}
}

func commandHelp() string {
	return strings.Join([]string{
		"Commands:",
		"  /help                 show this help",
		"  /clear                start a new session",
		"  /model [id]           list or switch model",
		"  /effort [level]       list or switch thinking effort",
		"  /status               show account/model/session/workspace status",
		"  /cost                 show token usage for the session",
		"  /resume <session-id>  resume a remote session",
		"  /sessions             list local transcripts",
		"  /session              show current session info",
		"  /skills [name]        list or show skills",
		"  /memory [add text]    show or add global memory",
		"  /permissions          show the active permission mode",
		"  /config [key=value]   show or set configuration",
		"  /init                 create a project NOMAD.md",
		"  /login  /logout       sign in or out",
		"  /exit                 quit (also Ctrl+D)",
		"",
		"End a line with '\\' for multi-line input. Ctrl+C interrupts a turn.",
	}, "\n")
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
