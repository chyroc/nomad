# CLAUDE.md

Nomad is a **managed-agents-only** coding agent CLI backed by Volcengine
Ark. This file guides the coding agent working on the repository.

## Project overview

- The only backend is the remote Ark managed-agents service: the server
  runs the agent loop and model, nomad is the local self-hosted worker.
- Normal usage requires **no environment variables**: first run does
  cross-device OAuth (or reuses an `arkcli` login), then auto-provisions a
  self-hosted environment and a coding agent, cached under `~/.nomad`.

## Architecture invariants

- **All Volcengine/Ark names and hosts live in `internal/ark`** — OAuth
  host, runtime base URL, OpenAPI TOP host, provisioning, SSE worker,
  credentials and skill registration. `internal/config` must contain only
  local paths and no provider domains.
- Resources are created exclusively through the MA managed-agents data
  plane: `POST /environments` (`config.type=self_hosted`), `POST /agents`,
  `POST /sessions`. Never bypass that for a non-MA backend.
- `internal/loop` is the backend-neutral event/Runner surface; `internal/ark`
  is the sole implementation.
- Session turns emit two `status_idle` kinds: `requires_action` after a
  tool call (keep waiting for the post-result running phase) and
  `end_turn` (the turn is finished). Don't treat `requires_action` as
  completion.
- Tool execution happens locally via the official
  `ark-runtime-go` `selfhosted` + `toolset` packages, with nomad's
  permission gate wrapping every tool.

## Skills rules (important)

- Discover local skills from `.claude/skills` and `.agents/skills` at user
  and project scope plus `~/.nomad/skills`.
- Uploading skill metadata **requires explicit user consent every time**:
  a multi-select picker + confirmation in the TUI, or an explicit
  `--sync-skills names` flag in headless mode. Never auto-upload.
- Upload **only `name` + `description`** as a stub zip; the SKILL.md body
  must never be sent. Bind via the agent update API (top-level agent
  `version` for optimistic concurrency, `skill_id`+`type=custom` entries).
- Link the local bundle into `<workspace>/.nomad/skills/<name>` (symlink,
  no copy) and point the agent at the local SKILL.md in the system prompt.

## Development conventions

- Language: Go 1.24+, modules in `internal/`.
- `gofmt -w` and `go vet` must be clean; run `go test -race ./...` before
  every commit.
- Follow Conventional Commits (English prefix: `feat:`/`fix:`/`docs:`/
  `test:`/`chore:`), body in English.
- Exported symbols need doc comments; avoid unused options/fields.
- The CLI is non-interactive when stdin is not a TTY; permission prompts
  must auto-deny rather than hang in headless mode.

## Working rules (must follow)

- **No inline code comments.** Do not write explanatory comments inside
  functions or above statements. When touching a file, delete existing
  non-doc comments rather than adding to them.
- **Commit after every completed feature or fix.** Once a requirement is
  implemented, do a local self-review (read the diff, run `gofmt`, `go vet`,
  `go test -race ./...`); if clean, commit immediately with a Conventional
  Commit message. Do not batch unrelated changes into one commit.

## Testing

- `test/` and `internal/ark` contain fake control-plane e2e tests; do not
  hit the network in unit tests (use `httptest`). Network-live checks are
  manual opt-in and must clean up every created cloud resource.
- Before pushing/committing, verify both e2e shapes: a simple chat turn
  and a multi-tool (parallel write) turn that exits on `end_turn`.

## Secrecy red line

Never commit company-internal information: internal hostnames, IPs,
feishu links, internal doc ids, account/project numbers, raw tokens or
API keys. Public Volcengine endpoints (`*.volces.com`,
`signin.volcengine.com`) and public open-source SDKs are fine. Internal
research notes stay local, outside the repo.
