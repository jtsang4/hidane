# Agent Rules for hidane

Rules for AI coding agents working in this repository. Project introduction lives in README.md — this file is only about how to work here.

## Git

- Commit messages use Conventional Commits in English: `type(scope): subject` (e.g. `feat: add memory distiller consumer`, `fix(kernel): commit cursor after batch`).
- Keep subjects imperative and specific. One logical change per commit.
- Never commit secrets. Provider API keys live only in the user's `settings.json` (mode 0600, under `HIDANE_HOME`) or the environment — never in the repo, the event log, logs, or any API response.

## Layout

- One Go module at the root: `main.go` (desktop app + CLI dispatch), `gui_on.go` / `gui_off.go` (build tag `nogui` drops Wails and cgo), `internal/*`, `cmd/fakeagent` (test double for the agent CLIs).
- `frontend/` is the Svelte 5 SPA (the only pnpm workspace). Its build output `frontend/dist` is embedded into the binary (`frontend/assets.go`).
- Kernel code lives in `internal/kernel/` — its invariants below apply there. Agent roles live in `internal/agents/`, the CLI drivers in `internal/agentcli/`, the capture-phase guard in `internal/guard/`.
- `docs/wails-migration.md` records how the Node/Postgres web version became this desktop app.

## Toolchain

- Go ≥ 1.26 (`gofmt`, `go vet` clean). Node ≥ 24 with **pnpm only** for the frontend (never npm/yarn commands or lockfiles).
- Wails v3 (`github.com/wailsapp/wails/v3`, currently `v3.0.0-beta.27`); keep it on the latest release when bumping.
- Prefer the latest stable version when adding a dependency; justify any pin. Prefer the standard library.
- SQLite through the pure-Go `modernc.org/sqlite` driver, so `-tags nogui` builds stay `CGO_ENABLED=0`.
- Frontend TypeScript strict mode is intentionally harsh (`exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`). Fix type errors properly; do not loosen tsconfig to make errors go away.

## Verification

- `make test` (go vet, also with `-tags nogui`; `go test ./...`; `pnpm -C frontend check`; `pnpm -C frontend test`) must pass before any commit. Unit tests guard kernel invariants; extend them when touching `internal/kernel/`.
- `make e2e` runs Playwright (chromium + webkit) against the real Go backend (`hidane serve`) with `cmd/fakeagent` standing in for the CLIs. `frontend/e2e/README.md` says what each spec guards: extend the spec that covers what you change, and keep that table current when you add one.
- After a UI change, run `make screenshots` and look at the affected pages (`bin/screenshots/<page>-<zh|en>-<desktop|browser|phone>.png`; desktop = the app's own chrome) — a diff does not show a broken layout. It fails on any uncaught page error. While iterating, `make screenshots ONLY=run-as,settings-roles` takes only the pages whose names start with those prefixes; take the full set once before committing.
- `make smoke-gui` launches the real Wails window; it must end with `ui ready (live transport: wails)`. Run it after touching `internal/desktop`, `/boot.js`, or the live transport.
- After touching `scripts/package.sh`, `build/`, `.github/workflows/release.yml` or a platform-specific dependency, run `.agents/skills/hidane-release/scripts/dry-run.sh`: it builds every release artifact locally and opens the macOS and (in Docker) Linux packages. Windows is cross-compiled; `GOOS=windows go vet ./...` must stay clean (its dependencies live in `go.sum` too).
- `make smoke-live` / `make acceptance` spend real tokens on the locally installed CLIs. The **agent-driven acceptance** (`scripts/acceptance.sh` executing `acceptance/scenarios.md`) is the end-to-end source of truth: when behavior changes, update the natural-language scenarios — do not encode acceptance in assertion scripts.
- Pay for verification by the size of the change — a full acceptance costs about an hour and real tokens:
  - every change: `make test` (and `make e2e` for anything a person sees); CI runs them on every push, with the fakes and no tokens;
  - a driver, the guard, a charter or a role's context: the `hidane-live-check` skill (minutes, a few real calls);
  - behavior a scenario describes: `make acceptance ARGS="--changed origin/main"` (only the scenarios the changed paths map to) or `ARGS="--only 4F,6E"`. Rebase onto the trunk first: the script refuses to start while `main` or `origin/main` has commits the branch lacks (`--behind-ok` overrides), since a run against code about to be rebased is paid for again;
  - the full `make acceptance`: before a minor or major release (the `hidane-release` skill), or after a sweeping change.
- A finding the acceptance makes more than once becomes a Go or Playwright test, so the next run need not rediscover it. Only a violated written expectation is a FAIL; everything else the tester notices is a note for the backlog, and a gateway error is retried, not a FAIL.
- The fakes prove protocol handling, not the real CLIs: after changing a driver, the guard, a charter or a role's context, follow the `hidane-live-check` skill (real `claude` / `codex` / `pi`, fresh `HIDANE_HOME`, judged against the files on disk — not the reply).
- Tests are hermetic: a temp `HIDANE_HOME`, fake CLIs by absolute path, `HIDANE_LOGIN_SHELL=0` for any spawned `hidane`. A test must never read the developer's `~/.hidane`, `~/.claude`, `~/.codex`, keychain, or shell startup files, and never start a real agent CLI.
- Verify claims with evidence: when you say something works, show the command output, file content, or event rows that prove it.
- Uncommon ports are a deliberate choice (serve **2718**, Vite **2719**, E2E **2797**) — do not switch to 3000/8080-style defaults.

## Architecture invariants (do not violate)

- The event log is **append-only** (enforced by triggers). Never UPDATE or DELETE rows in `events`. Replay = reset a consumer cursor, never mutate history.
- Events record **facts, not thinking**: state changes, boundary crossings (messages, side effects, user interaction), and decision outcomes. Model reasoning and raw tool I/O belong to traces referenced by `execution_id`.
- Write-through at occurrence time. No batch back-filling of events after the fact.
- Projections (daily worklog, board, indexes) are derived, rebuildable, read-only views over the log — never a second source of truth.
- Connectors **capture and normalize only**. They never judge, never call agents directly, never block on processing.
- Triage is rules-first; models wake rarely. Events from `agent:*` / `kernel:*` sources must never re-enter triage (loop protection).
- Side effects are two-phase: `side_effect.intent` before the action, `side_effect.result` after.
- The kernel stays domain-agnostic: work items, threads, executions, workspaces, artifacts. No software-engineering semantics in the kernel (no built-in CI/CD, dependency graphs, deploy pipelines).
- **Memory must be able to expire.** Distillation never records current bugs, missing paths, deployment breakage, or safety-bypassing workarounds — those get fixed, and the memory then actively misleads future runs (this happened: a "use `pi -ne` to skip the guard" lesson survived the fix and was injected into new tasks). Every promotion needs a matching removal path (`hidane forget <id>`, `DELETE /api/memories/:id`), and removals are recorded as `memory.forgotten`.
- **Database vs files boundary**: the database holds ONLY what needs atomic multi-writer ordering and queries — the event log spine plus small state tables (work_items, threads, cursors, executions, schedules, channel_bindings, repos, checkouts). Everything agents or humans consume is FILES: layered memory (`memory/MEMORY.md`, `<workspace>/MEMORY.md`), daily worklog + session-trace archives (`worklogs/YYYY/MM/DD/`), session traces, workspace artifacts, `settings.json`, `POLICY.json`. Never add a table for content that agents read — write a file projection instead; never make agents query the database directly.
- Three agent roles (Primary / Manager / Worker) are **one loop at three scopes**. Differences live in charter, context, and lifecycle only. Do not fork per-role frameworks: Primary and Manager are turn handlers on the same `kernel.Runtime` sharing `think()` and the effect-list charter format.
- **A turn decides; it never waits.** A turn handler must not await a worker execution or another agent's answer. Dispatch (`WorkerPool.Dispatch`) and return; the outcome comes back as a message to the owner's mailbox. Anything held in memory across a wait is lost on restart.
- **Mailboxes are views, not tables.** An agent's mailbox is the events with `mailbox = <address>` after its `mailbox:<address>` cursor. Deliver with `Kernel.Post` (which also enforces the causal hop budget); never add a queue table or an in-process queue as the source of truth.
- Every execution has an owner mailbox and is recorded in `executions`; the only way an execution ends is `reportOutcome` posting `execution.finished` to that owner (including `lost` after a restart and `cancelled`). Never leave an execution without someone to read its outcome.
- Every person's message enters through `System.SubmitMessage` (`internal/agents/ingress.go`) and lands on the main thread; every answer (`agent.reply`, `escalation`, `attribution.ambiguous`, `execution.steered`) carries `payload.root` — the UI groups by it.
- Propagation across the work tree: bubbling kinds are declared in `kernel.Bubbles` (tool traffic never bubbles); the capture phase is `internal/guard` — the built-in deny list, the policy files, keeping workers off hidane's own data and the pending-input pause, rules only, never a model call; cancel flows down with `CancelTree`.
- Skills are a shared global pool loaded by each CLI's native discovery. Do not build skill routing, scoping, or injection mechanisms.
- Every work item owns one workspace: where its executions start and put their products by default, not a fence — workers may change files anywhere else. Its repositories are checkouts (`internal/repos`): a git worktree inside the workspace on `hidane/<wi>`, or the person's own directory only when they ask (a model must quote their words; one task per repo at a time). A task working on a repo in a worktree may not change the person's checkout of it. The guard refuses changes to hidane's data directory (except the task's own workspace) and to a workspace's `.hidane`, and any access to `settings.json`; codex gets the same limits from its permission profile (never `sandbox_mode`, which switches it off), since the hook cannot see a command's `workdir`.
- Repo and branch are never guessed: an ambiguous or missing repository is a question before anything is created. Only the person archives a worktree (teardown, directory removed, branch kept), and a directory git cannot vouch for is never deleted. Git mechanics stay in `internal/repos`, out of the kernel.

## Agent CLI integration (hard-won specifics)

- Every role is a CLI subprocess (`internal/agentcli`), started in its own process group and killed as a group. Reasoning roles (Primary, Manager, distiller) run without tools — MCP servers from the person's own CLI config included (`claude --tools "" --strict-mcp-config`; codex with its tool features off and each server from `config/read` disabled in `thread/start`); workers run with tools behind the guard.
- A reasoning role's charter **replaces** the CLI's system prompt (`--system-prompt`; codex `baseInstructions`); a worker's is appended (codex `developer_instructions`). Told it was a coding agent with file tools, a Primary once claimed edits nothing had made.
- Reasoning roles run in an empty per-role directory under `runtime/roles/`, never in `HIDANE_HOME` itself: a model writes its cwd into briefs, and a worker then wrote into hidane's data directory.
- The guard is one Go implementation behind `hidane guard --format claude|codex|pi`: a Claude Code PreToolUse hook via `--settings`, a Codex hook via `-c hooks.PreToolUse=…` on `codex app-server` plus `config: {"bypass_hook_trust": true}` in `thread/start` / `thread/resume` (app-server silently skips an untrusted command-line hook, and the key does nothing on its command line; the driver stops any run whose command or file change the guard never saw), and a pi extension shim written to `runtime/pi-guard.ts`. Never reimplement rules in TypeScript or per CLI. A policy file that exists but cannot be parsed refuses changes — it must never fail open.
- Keys reach CLIs through the environment (`ANTHROPIC_AUTH_TOKEN`, `HIDANE_CODEX_API_KEY`), never on a command line other local users can read; pi has no other way and gets `--api-key`.
- Strip parent-session markers (`CLAUDECODE`, …) from every CLI's environment; set `PI_OFFLINE=1` for pi.
- pi's RPC mode is driven over stdin JSONL (`prompt`, `steer`, `abort`, `get_state`); wait for `agent_settled`, not `agent_end`. Claude's stream-json emits `result` per turn while stdin stays open — close stdin when no queued turn remains. Codex runs as `codex app-server` (one per run, closed by stdin EOF): answer every server request at once and tell them apart from replies first (they share the id space); steer with `turn/steer` + `expectedTurnId` + `clientUserMessageId` and count the words taken in only when that `userMessage` item (its `clientId`) appears; a hook-refused call leaves no tool item.
- A codex worker's permission profile marks the person's checkout `read` and names `write` only what a worktree's commits need (its own git dir, the repo's `objects`, `refs/heads/hidane`, `logs/refs/heads/hidane`); codex otherwise keeps a worktree's git metadata read-only.
- A Finder-launched app has a minimal PATH: resolve the login shell's PATH (`agentcli.LoginShellPath`) and allow absolute paths per CLI in settings.

## External integrations

- Prefer official SDKs over hand-rolled protocol code. The Feishu connector uses `github.com/larksuite/oapi-sdk-go/v3` for token refresh, the long connection and dispatch — do not reintroduce hand-written crypto, token caching or a public callback endpoint. Same principle as skills (native discovery) and memory (files).

## Security

- The desktop webview reaches the API through the Wails asset server only; it needs no token and nothing listens on a port.
- In `hidane serve`, `/health` is the only open endpoint. `/api/*` always requires a bearer token (`HIDANE_API_TOKEN`, or a random one generated and printed per run); `/webhook/:name` requires an `x-hidane-signature` HMAC-SHA256 and is refused outright while `HIDANE_WEBHOOK_SECRET` is unset (a webhook wakes the Primary; an unsigned one is reachable by any web page's cross-origin POST). Never remove these gates or add unauthenticated write endpoints. Any endpoint that can start an execution is spending money and granting code execution.
- **Never treat a library option name as a guarantee on a security boundary.** The old Node SDK's `EventDispatcher({ verificationToken })` read like validation but returned `true` outright when no encrypt key was set — `/feishu/events` was open in production as a result. Read the source, or re-check the invariant yourself at the boundary. Preferring official SDKs (above) applies to protocol mechanics, not to trust decisions.

## Configuration

- Runtime knobs go through `internal/config` env vars; document new vars in the README environment table. Model and provider choice lives in `settings.json` (`internal/settings`) and the Settings page, never in env vars — a desktop app is configured from its own UI.

## Language & i18n

- Code identifiers, comments, and README are English. Acceptance scenarios and other operator-facing docs may be Chinese.
- Comments state constraints the code cannot express; no narrating what the next line does.
- **UI strings go through the web i18n layer** (`frontend/src/i18n/resources.ts` plus its framework adapter): never hardcode user-visible literals in components. Supported locales are `zh` (default) and `en` — every new key must be added to BOTH, and keys are typo-checked at compile time via the typed resources declaration.

## Agent tooling

- This repository's project-local Codex configuration lives in `.codex/config.toml`; do not modify `~/.codex/config.toml` for repository-specific behavior.
- The runtime's agent-role skills remain each CLI's own pool described above; that is separate from coding-agent guidance. Shared repository coding-agent skills live in `.agents/skills/` and must remain agent-neutral. Claude Code's `.claude/skills` is only an adapter symlink to that directory.
- Project MCP servers are declared in both `.codex/config.toml` and `.mcp.json` so Codex and Claude Code get the same tools: **svelte** (Svelte docs and autofixer — required for `.svelte` edits) and **gopls** (the Go team's language server: `go_diagnostics`, `go_symbol_references`, `go_rename_symbol`, `go_package_api`, `go_vulncheck`, …). Prefer gopls over grep for "who calls this" and for renames across packages; run `go_diagnostics` on files you edited.
- Repository skills (`.agents/skills/`): `svelte-code-writer` and `svelte-core-bestpractices` for frontend work, `hidane-live-check` for verifying behavior on the real agent CLIs, `hidane-release` for publishing a version (a pushed `v*` tag runs `.github/workflows/release.yml`; packaging is `scripts/package.sh`).

## Frontend

- `frontend` is a Svelte 5 + Vite SPA. Do not introduce React, JSX, SvelteKit, or a router migration without explicit authorization.
- When changing `frontend`, also read `frontend/AGENTS.md`; it is the more specific rule layer when an agent starts in that directory.
- **Every visible change follows the design system in `frontend/DESIGN.md`** (tokens in `frontend/src/styles.css` `@theme`, primitives in `frontend/src/components/ui/`, shared patterns in `frontend/src/lib/styles.ts`). Tailwind's default palette, shadows and radii are switched off; a missing value becomes a new token documented there, never an arbitrary value or literal color. `test/design-system.test.ts` enforces it.
- Before editing Svelte files, use the project-local Svelte MCP/documentation workflow; after editing, run `pnpm -C frontend check`.
- Preserve the existing API, bearer-token, SSE / desktop live-frame, pending-state, pagination, notification, dark-theme, responsive-navigation, accessibility, and sanitized-Markdown contracts.
- Before handoff, run `pnpm -C frontend check`, `pnpm -C frontend test`, and `pnpm -C frontend build` in addition to the root checks.
