# hidane（火種）

> Banked embers: the flame may die, the seed fire never does.

**hidane** is a persistent personal agent runtime. Sessions end, processes exit,
models change — the agent's identity, memory and narrative persist. What persists
is loadable state, never a process.

## Architecture

The spine is an **append-only event log** (PostgreSQL). Message-queue semantics
are a derived view: consumers keep cursors, events are never destroyed, history
replays.

```
connectors (cli / webhook / timer / ...)
    │ capture + normalize only, never judge
    ▼
event log (append-only spine) ◄────────────────┐
    ├─► triage (deterministic rules first)      │
    │        ▼ few events wake the model        │
    │   Primary ─► Manager ─► Worker ───────────┘ (actions written back as events)
    └─► projections (daily worklog, indexes)

user chat: fast lane straight to Primary, write-through to the log
```

Three agent roles, one loop — the same pi agent instantiated at three scopes:

| Role | Lifetime | Persists |
|---|---|---|
| **Primary** | permanent | identity, routing policy |
| **Manager** | per work item | work item state, thread |
| **Worker** | per execution | nothing — trace goes to the log |

Every work item owns a **thread** (interaction lane) and a **workspace**
(execution home, one directory per work item). Worker executions run inside the
workspace with tools and native pi skill discovery; session traces are archived
under `<workspace>/.hidane/sessions`.

## Quickstart (local dev)

Requirements: Node ≥ 24, pnpm, Docker, [pi](https://github.com/earendil-works/pi-mono) installed and authed.

```bash
docker compose -f docker-compose.dev.yml up -d   # postgres on localhost:2716
pnpm install
pnpm dev init                                     # create schema

pnpm dev chat "帮我写一个 hello world 脚本并运行验证"   # fast lane, full loop
pnpm dev items                                    # work items
pnpm dev events --tail 30                         # the raw log
pnpm dev log                                      # daily worklog projection
pnpm dev daemon                                   # resident: http :2718 + heartbeat + triage
```

Webhook connector (while daemon runs):

```bash
curl -X POST localhost:2718/webhook/github -d '{"hello":"world"}' \
  -H 'content-type: application/json'
```

## Verification

Two layers, different jobs:

```bash
pnpm test    # deterministic: kernel invariants (event log, cursors, triage, projections)
pnpm e2e     # agent-driven acceptance: a tester agent executes acceptance/scenarios.md
             # against the real system and writes an evidence-based verdict report
pnpm smoke   # scripted live smoke of the happy path (fast sanity, not the source of truth)
```

Acceptance scenarios are natural language (`acceptance/scenarios.md`) — cheap to
evolve with requirements, and able to express semantic checks (does the reply
match what actually happened?) that assertion scripts cannot. Verdicts require
observed evidence; the report lands in `.acceptance-report.json`.

## Deploy (production: Coolify)

Deploy this repo as a **Docker Compose** resource in Coolify using
`docker-compose.yml`. Set in Coolify:

- `POSTGRES_PASSWORD` — database password
- provider API key(s) for pi, e.g. `DEEPSEEK_API_KEY`

The app listens on `2718` (`/health` for probes). Workspaces, worklogs and agent
session traces live in the `/data` volume.

## Environment

| Var | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | `postgres://hidane:hidane@localhost:2716/hidane` | Postgres connection |
| `HIDANE_HOME` | `~/.hidane` | workspaces / worklogs / session traces |
| `PORT` | `2718` | daemon http port |
| `HIDANE_HEARTBEAT_SEC` | `300` | heartbeat connector interval |
| `HIDANE_PI_PROVIDER` / `HIDANE_PI_MODEL` | pi defaults | model override |
| `HIDANE_ROUTE_THINKING` | `low` | thinking level for routing/planning |
| `HIDANE_WORKER_THINKING` | `medium` | thinking level for executions |
| `HIDANE_WORKER_TIMEOUT_SEC` | `600` | per-execution timeout |

## Design principles

- The log records everything; not everything is delivered through a queue.
- Facts go to the event log; thinking stays in traces referenced by
  `execution_id`.
- Side effects are two-phase: intent event before, result event after.
- Connectors capture, never judge. Triage is rules-first; models wake rarely.
- The kernel knows work items, executions, side effects and artifacts — it does
  not model software engineering (no built-in CI/CD, dependency graphs, deploy
  pipelines).

## Roadmap

- Memory distiller consumer (long-term memory promotion)
- Feishu channel binding (thread ↔ topic-group topic)
- Worktree workspace provider for coding work items
- Side-effect permission gate as a pi extension (`tool_call` intercept)
- Escalation policy & digests on the main thread
