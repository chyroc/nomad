# Nomad

> A self-hosted coding agent CLI backed **only** by Volcengine Ark
> **managed-agents**. Zero required environment variables: it logs in with
> OAuth, provisions its own self-hosted environment + coding agent, runs
> tools locally.

## What it is

Nomad has a single backend. The agent loop, model calls and session state
all live in the Ark managed-agents service; your machine is the
**self-hosted worker** that executes coding tools (`bash`, `read`,
`write`, `edit`, `glob`, `grep`) inside the current repository and posts
results back.

```
CLI (TUI / -p / stream-json)
        │ user.message (SSE event stream)
        ▼
Ark managed-agents control plane  ── agent.tool_use ──▶  nomad (this machine)
        │                                                   bash/read/write/...
        ◀──────────── user.tool_result (local execution) ──┘
```

## Install

```bash
npm install -g @chyroc/nomad   # or: npx @chyroc/nomad
```

Prebuilt binaries are also published on
[GitHub Releases](https://github.com/chyroc/nomad/releases) (macOS/Linux/FreeBSD,
x64/arm64), or build from source:

```bash
go install github.com/chyroc/nomad/cmd/nomad@latest
```

## Quick start

```bash
nomad                  # interactive TUI; first run prints a login URL
cd /your/repo && nomad
```

Build from source:

```bash
make build            # bin/nomad
cd /your/repo
bin/nomad
```

On first run nomad:

1. starts a cross-device OAuth 2.0 + PKCE login (browser approval, paste
   the code), or reuses an existing public `arkcli` login if present;
2. creates a `self_hosted` environment and a coding agent (built-in
   coding system prompt + toolset), caching their ids in
   `~/.nomad/profile.json`;
3. lists `/models`, selects a tool-calling chat model, and opens a
   managed-agents session.

## Non-interactive (headless)

```bash
nomad -p "add tests for the ark package"
nomad -p --output-format stream-json "refactor runner.go"   # NDJSON events
nomad -p --output-format json "summarize this repo"         # single JSON
nomad --resume <session-id> -p "continue"
nomad -c -p "what did we just do?"
nomad --model <model-id> -p "..."
nomad --permission-mode acceptEdits -p "..."
nomad --dangerously-skip-permissions -p "..."
nomad --image ./diagram.png -p "what is this?"
```

Flags: `-p/--print`, `--output-format text|json|stream-json`, `--model`,
`--resume`, `-c/--continue`, `--session-id`, `--max-turns`,
`--permission-mode default|acceptEdits|plan|bypassPermissions`,
`--allowed-tools`, `--disallowed-tools`, `--system-prompt`,
`--append-system-prompt`, `--add-dir`, `--image`, `--effort`, `--verbose`.

### Goal mode (keep working until a condition is met)

`--goal "<condition>"` keeps the session self-continuing after every
turn: a separate, tool-free model call checks the transcript against the
condition; when it is not met, its reason is injected as the next turn
and work continues. It stops when the check confirms the goal, judges it
impossible, the evaluator repeatedly fails, or the 50-turn safety cap is
hit. Write a condition the evaluator can verify from conversation
evidence alone (e.g. `"go test ./... exits 0"`). `stream-json` emits a
`goal_check` event per evaluation; the final JSON carries a `goal`
object. Example:

```bash
nomad -p --goal "the feature works and go test ./... exits 0" "implement X"
```

## Skills

Nomad discovers local `SKILL.md` bundles from:

- `./.claude/skills/<name>/`, `./.agents/skills/<name>/`
- `~/.claude/skills/<name>/`, `~/.agents/skills/<name>/`
- `~/.nomad/skills/<name>/`

Uploading skills is **always opt-in with explicit consent**, because it
sends data to the platform:

- TUI: `/skills sync` shows a catalogue and a **multi-select picker**,
  then a final `y/N` confirmation.
- Headless: `--sync-skills name1,name2` is itself the explicit consent;
  without it nothing is uploaded.

Only the **name + description** frontmatter is uploaded (a minimal stub
zip) — the `SKILL.md` body never leaves the machine, and the agent skill
binding has a cap so you choose exactly which skills to publish. The
platform only needs the metadata to discover/trigger the skill; when a
bound skill runs, nomad symlinks the local bundle into
`<workspace>/.nomad/skills/<name>` and instructs the agent to read the
real local `SKILL.md`, so execution uses your local file (no copy).

## Memory & project context

- `~/.nomad/MEMORY.md` — global user memory
- `NOMAD.md` / `CLAUDE.md` / `AGENTS.md` in the workspace root — project
  instructions

Both are injected into the agent's system context. `/memory` views/adds
global memory; `/init` scaffolds a project `NOMAD.md`.

## TUI commands

`/help` `/clear` `/model` `/status` `/cost` `/resume` `/sessions`
`/session` `/skills` (`/skills sync`) `/memory` `/permissions` `/goal`
`/config` `/init` `/login` `/logout` `/exit`.

`Ctrl+C` interrupts a turn (also sends `user.interrupt` server-side) and
keeps context; twice exits.

### Session goals

`/goal <condition>` arms a completion condition for the current session
and immediately starts working. After every turn a separate tool-free
check decides, from transcript evidence only, whether the condition is
met; if not, the session self-continues with the check's reason. The
status line shows `◉ goal N` while active. `/goal` shows status,
`/goal clear` stops early, `/goal resume` reactivates a paused goal, and
a new `/goal <condition>` replaces the current one. State is persisted
under `~/.nomad/goals/`, so an active goal survives `/resume`.

## Architecture

| Path | Responsibility |
| --- | --- |
| `cmd/nomad` | Entry point and flag parsing |
| `internal/ark` | All Volcengine/Ark provider code: OAuth hosts, runtime/TOP endpoints, credentials, MA provisioning (env/agent/session), SSE worker, local tool gate, skill registration |
| `internal/cli` | TUI, headless runner, `text/json/stream-json` renderers, slash commands, skill consent UI |
| `internal/config` | Local file paths only (no provider domains) |
| `internal/control` | Wires credentials, cached profile, client and runner |
| `internal/contextinfo` | Memory and local skill discovery |
| `internal/goal` | Session goal state, evaluator prompts and transcript formatting |
| `internal/loop` | Backend-neutral event model and Runner interface |
| `internal/store` | Local JSONL transcripts |

Managed-agents resources are created exclusively via the MA data plane
(`POST /environments` with `config.type=self_hosted`, `POST /agents`,
`POST /sessions`).

## Security

Credentials are stored in `~/.nomad/auth.json` (`0600`). Skill uploads
require explicit per-sync consent and carry name/description only.
Provider domains are confined to `internal/ark`. Mutating tools honor the
permission mode; `default` prompts in the TUI and auto-denies headless
sessions unless allowed.

## Testing

```bash
make test          # unit + fake control-plane e2e (race detector)
```

The fake MA server test drives a full two-phase `requires_action` →
`end_turn` turn with a real local file write; OAuth code/PKCE, stream-json,
option parsing, skill discovery and the metadata-only stub are unit
tested.
