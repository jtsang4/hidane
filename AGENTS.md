# Agent Rules for hidane

Rules for AI coding agents working in this repository. Project introduction lives in README.md — this file is only about how to work here.

## Git

- Commit messages use Conventional Commits in English: `type(scope): subject` (e.g. `feat: add memory distiller consumer`, `fix(kernel): commit cursor after batch`).
- Keep subjects imperative and specific. One logical change per commit.
- Never commit secrets. Provider API keys and passwords are env-only.

## Layout

- pnpm monorepo: `apps/server` (runtime daemon + CLI), `apps/web` (frontend), `packages/*` (shared). Root scripts proxy (`pnpm test` runs all workspaces).
- Kernel code lives in `apps/server/src/kernel/` — its invariants below apply there.

## Toolchain

- Node ≥ 24, package manager is **pnpm only** (never npm/yarn commands or lockfiles).
- Prefer the latest stable version when adding a dependency; justify any pin.
- Pure ESM: `"type": "module"`, `moduleResolution: NodeNext`. Local imports use the `.js` extension (`./kernel/events.js`). No `require()`.
- TypeScript strict mode is intentionally harsh (`exactOptionalPropertyTypes`, `noUncheckedIndexedAccess`, `verbatimModuleSyntax`). Fix type errors properly; do not loosen tsconfig to make errors go away.

## Verification

- Local Postgres for dev/tests: `docker compose -f docker-compose.dev.yml up -d` (host port **2716**; the daemon listens on **2718**). Uncommon ports are a deliberate choice — do not switch to 5432/3000/8080-style defaults.
- `pnpm typecheck` and `pnpm test` must pass before any commit. Unit tests guard kernel invariants; extend them when touching `src/kernel/`.
- `pnpm e2e` runs the **agent-driven acceptance** (`scripts/acceptance.ts` executing `acceptance/scenarios.md` against the real system). This is the end-to-end source of truth. When behavior changes, update the natural-language scenarios — do not encode acceptance in assertion scripts.
- `pnpm smoke` is a scripted happy-path sanity check only, never the source of truth.
- Verify claims with evidence: when you say something works, show the command output, file content, or event rows that prove it.

## Architecture invariants (do not violate)

- The event log is **append-only**. Never UPDATE or DELETE rows in `events`. Replay = reset a consumer cursor, never mutate history.
- Events record **facts, not thinking**: state changes, boundary crossings (messages, side effects, user interaction), and decision outcomes. Model reasoning and raw tool I/O belong to traces referenced by `execution_id`.
- Write-through at occurrence time. No batch back-filling of events after the fact.
- Projections (daily worklog, indexes) are derived, rebuildable, read-only views over the log — never a second source of truth.
- Connectors **capture and normalize only**. They never judge, never call agents directly, never block on processing.
- Triage is rules-first; models wake rarely. Events from `agent:*` / `kernel:*` sources must never re-enter triage (loop protection).
- Side effects are two-phase: `side_effect.intent` before the action, `side_effect.result` after.
- The kernel stays domain-agnostic: work items, threads, executions, workspaces, artifacts. No software-engineering semantics in the kernel (no built-in CI/CD, dependency graphs, deploy pipelines).
- **Database vs files boundary**: the database holds ONLY what needs atomic multi-writer ordering and queries — the event log spine plus small state tables (work_items, threads, cursors). Everything agents or humans consume is FILES: layered memory (`memory/MEMORY.md`, `<workspace>/MEMORY.md`), daily worklog + session-trace archives (`worklogs/YYYY/MM/DD/`), session traces, workspace artifacts. Never add a table for content that agents read — write a file projection instead; never make agents query the database directly.
- Three agent roles (Primary / Manager / Worker) are **one loop at three scopes**. Differences live in charter, context, and lifecycle only. Do not fork per-role frameworks.
- Skills are a shared global pool loaded by pi's native discovery. Do not build skill routing, scoping, or injection mechanisms.
- Every work item owns exactly one workspace directory; an execution's cwd is always inside its work item's workspace.

## pi integration (hard-won specifics)

- Primary/Manager run as in-process SDK sessions; Workers run as `RpcClient` subprocesses. Keep that split: reasoning roles need persistence and low latency, workers need cwd and crash isolation.
- A caller-provided `DefaultResourceLoader` is **not** reloaded by `createAgentSession` — call `await loader.reload()` yourself, or charters silently never reach the system prompt.
- The pi package is ESM-only; resolve its `cli.js` via `import.meta.resolve`, not `require.resolve`.
- Any direct pi subprocess must set `stdin: "ignore"` (pi waits forever on an open piped stdin) and `PI_OFFLINE=1`.
- Use the exported `RpcClient`; never hand-roll the RPC JSONL protocol.

## External integrations

- Prefer official SDKs over hand-rolled protocol code. The Feishu connector uses `@larksuiteoapi/node-sdk` for token refresh, AES decryption, the challenge handshake, signature verification and dispatch — do not reintroduce hand-written crypto or token caching. Same principle as pi (native skill discovery) and memory (files).

## Security

- `/health` is the only open endpoint. `/api/*` requires `HIDANE_API_TOKEN` (Bearer); `/webhook/:name` requires an `x-hidane-signature` HMAC-SHA256 when `HIDANE_WEBHOOK_SECRET` is set. Production sets both — never remove these gates or add unauthenticated write endpoints.

## Configuration & deployment

- All runtime configuration goes through `src/config.ts` env vars; document new vars in the README environment table.
- Production deploys via Coolify using `docker-compose.yml`. Env vars only reach containers if referenced in the compose `environment:` block — adding them in the Coolify UI alone is not enough.
- Local development and verification never involve Coolify.

## Language & i18n

- Code identifiers, comments, and README are English. Acceptance scenarios and other operator-facing docs may be Chinese.
- Comments state constraints the code cannot express; no narrating what the next line does.
- **UI strings go through react-i18next** (`apps/web/src/i18n/resources.ts`): never hardcode user-visible literals in components. Supported locales are `zh` (default) and `en` — every new key must be added to BOTH, and keys are typo-checked at compile time via the typed resources declaration.
