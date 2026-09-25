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
connectors (web / cli / feishu / webhook / timer / schedule)
    │ capture + normalize only, never judge
    ▼
event log (append-only spine) ◄──────────────────────────────┐
    │  an event with a `mailbox` is also a message to that agent loop
    ├─► triage (rules first) ──► primary mailbox             │
    ├─► primary turn ──► manager:<wi> mailbox                │
    ├─► manager turn ──► worker pool ──► execution.finished ─┘ (back to its owner)
    └─► projections (daily worklog, task board)
```

**The runtime is an event loop per agent.** Each agent loop (`primary`,
`manager:<work item>`) has a mailbox: the events addressed to it, read from its
own cursor. A **turn** takes everything that piled up since the last one (so
several messages sent minutes apart are decided together), makes one model call,
and applies the resulting effects. A turn never waits on long work: it dispatches
a worker and ends; the worker's `execution.finished` is posted back to the
Manager's mailbox as the next message. Executions are recorded in a small
`executions` table with their owner, so one lost to a restart is reported rather
than forgotten. Mailboxes with a person's message in them (interrupt lane) are
served before background work; idle work (memory distillation, archiving) runs
only when nothing else is pending.

Three agent roles, one loop — the same turn machinery at three scopes:

| Role | Lifetime | Persists |
|---|---|---|
| **Primary** | permanent | identity, attribution of messages to work items, lifecycle actions |
| **Manager** | per work item | work item state, current understanding (`TASK.md`), thread |
| **Worker** | per execution | nothing — trace goes to the log |

Work items form a **tree**. A Manager fans out by creating child work items (one
workspace, one writer each) and hears back once when all of them have settled.
Across the tree, facts propagate like DOM events:

- **Bubbling** — a question a level cannot settle (`escalation.raised`) or a
  message that is not its own (`message.reroute_requested`) climbs one level at
  a time to the Primary and then to the person, carrying what each level tried.
  Which kinds bubble is declared per kind; tool traffic never does.
- **Capture** — before any worker tool call runs, it passes the policy files
  from the global one down through each ancestor workspace to its own
  (`POLICY.json`), and any of them can refuse it. Rules only, no model. While a
  person's new message is queued for a running worker, changes are paused until
  it has been read.
- **Cancel** flows down the tree; its source is a person, a deadline, or a spent
  budget (`HIDANE_MAX_HOPS` bounds any causal chain of messages).

Every message a person sends lands on the main thread and is **attributed** to a
work item cheapest-first: an explicit target or a reply needs no model; only the
rest go to the Primary, which asks ("which one?") rather than guessing when it is
not confident. The person can move a message to another item at any time. Every
answer names the message it answers (`payload.root`), which is how the web UI
shows a late reply under its question instead of at the bottom.

The main conversation only ever grows, so it is read as **history, not a feed**:
it is searched on the server (every message ever said, plus work item titles),
opened at any event (`/?at=<event id>` — search hits, day jumps, links, a work
item's origin), and walked in both directions from there. A person can hide
something they said (`message.redacted`); the row stays in the append-only log,
but every reader — API, stream, search, worklog, agent context, distiller —
masks it. The Primary does not carry the conversation in an ever-growing model
session: each turn opens a fresh one and reads a bounded window of recent turns
rebuilt from the log, and when a message refers to something older it emits a
`recall`, whose search results come back as its next message.

## Quickstart (local dev)

Requirements: Node ≥ 24, pnpm, Docker, [pi](https://github.com/earendil-works/pi-mono) installed and authed.

```bash
cp .env.example .env                              # fill in the provider API key
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
- `DATABASE_URL` — optional explicit connection URL; set this with a percent-encoded
  password when `POSTGRES_PASSWORD` contains URL-reserved characters such as `#`
  or `@` (host `db`, port `5432`, user and database `hidane`)
- provider API key(s) for pi, e.g. `DEEPSEEK_API_KEY`

For an initialized database, changing `POSTGRES_PASSWORD` does not change the
database role's password. Back up the database, change the `hidane` role's password
with `psql` (`\password hidane`), update both Coolify variables, and redeploy while
preserving the existing volumes. Coordinate the change because new connections
using the previous password fail as soon as the role's password changes.

The app listens on `2718` (`/health` for probes). Workspaces, worklogs and agent
session traces live in the `/data` volume.

## Environment

`.env.example` is the full annotated list; copy it to `.env` (gitignored). Local
dev loads it from the repo root, and docker compose / Coolify substitute it into
`docker-compose.yml`.

| Var | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | `postgres://hidane:hidane@localhost:2716/hidane` | Postgres connection |
| `HIDANE_HOME` | `~/.hidane` | workspaces / worklogs / memory / session traces |
| `PORT` | `2718` | daemon http port |
| `HIDANE_HEARTBEAT_SEC` | `300` | heartbeat connector interval |
| `HIDANE_DISTILL_SEC` | `600` | memory distiller interval (runs when idle; forced after 3×) |
| `HIDANE_PI_PROVIDER` / `HIDANE_PI_MODEL` | pi defaults | model override — set both or neither |
| `HIDANE_ROUTE_THINKING` | `low` | thinking level for routing/planning |
| `HIDANE_WORKER_THINKING` | `medium` | thinking level for executions |
| `HIDANE_ROUTE_TIMEOUT_SEC` | `180` | per-routing-call timeout |
| `HIDANE_WORKER_TIMEOUT_SEC` | `600` | per-execution timeout |
| `HIDANE_MAX_WORKERS` | `3` | worker subprocesses at once; further executions queue |
| `HIDANE_MAX_TURNS` | `4` | agent turns (model calls) running at once |
| `HIDANE_MAX_HOPS` | `24` | longest causal chain of messages before a person is asked |
| `HIDANE_MAX_EXECUTIONS_PER_ITEM` | `12` | executions one work item may start before it asks to continue |
| `HIDANE_ATTRIBUTION_THRESHOLD` | `0.6` | below this routing confidence the Primary asks instead of guessing |
| `HIDANE_API_TOKEN` | unset | bearer token for `/api/*` — **required in production** |
| `HIDANE_WEBHOOK_SECRET` | unset | HMAC-SHA256 secret for `/webhook/*` — **required in production** |

`HIDANE_PI_PROVIDER`/`HIDANE_PI_MODEL` must be set together: a half-configured
pair raises at startup rather than silently falling back to pi's own default.
At startup hidane performs the same forced model-catalog refresh as the `pi update --models`
command (bounded to 15 seconds), then merges the result with the project
catalog baked in at `deploy/pi-models.json`. The effective model is printed on
boot and reported by `/api/status`.
For a manual local refresh, run `pnpm -C apps/server exec pi update --models`.

## Schedules (active connectors)

User-defined timers managed at `/schedules` in the web UI (or `/api/schedules`):

- **http** — poll a URL on a period; the response is captured as a
  `connector.http` event. `wake: true` on the definition asks triage to wake
  the Primary with it; otherwise it is record-only.
- **prompt** — hand the Primary a task on a clock (cron with timezone, or a
  fixed interval ≥10s): firing posts a `schedule.prompt` to the Primary's
  mailbox and returns. The reply also goes to the bound Feishu main chat when
  one exists — reminders actually reach you.

After daemon downtime an overdue schedule fires once and re-anchors from now —
never a catch-up storm.

## Design principles

- The log records everything; not everything is delivered through a queue.
- Facts go to the event log; thinking stays in traces referenced by
  `execution_id`.
- Side effects are two-phase: intent event before, result event after.
- Connectors capture, never judge. Triage is rules-first; models wake rarely.
- A turn decides and returns; long work is dispatched and its outcome comes
  back as a message. Nothing waits in memory for something a restart can lose.
- The kernel knows work items, executions, side effects and artifacts — it does
  not model software engineering (no built-in CI/CD, dependency graphs, deploy
  pipelines).

## Feishu channel binding

Set these in the environment (production: Coolify) to enable the Feishu connector:

| Var | Meaning |
|---|---|
| `FEISHU_APP_ID` / `FEISHU_APP_SECRET` | app credentials (open.feishu.cn) |
| `FEISHU_VERIFICATION_TOKEN` | authenticates inbound events — one of these two is **required** |
| `FEISHU_ENCRYPT_KEY` | AES-256-CBC decrypt; an encrypted envelope authenticates itself |

`/feishu/events` runs agent executions, so it fails closed: events are rejected
with 401 unless a verification token matches or the envelope is encrypted. The
SDK's own token check is a no-op for plaintext schema-2.0 events, so hidane does
not rely on it.

Point the app's event subscription callback at `https://<host>/feishu/events`
and subscribe to `im.message.receive_v1`. Grant `im:message:send_as_bot`,
`im:message.p2p_msg:readonly`, `im:message.group_at_msg:readonly`,
`im:chat:readonly`, and `im:resource` (without the last one, image messages
arrive but their bytes cannot be downloaded for the vision model). A p2p message maps to the main thread; the reply-thread
under a bot-posted `📋 wi_x` root is addressed to that work item directly.
Answers are delivered by an outbox consumer (its own cursor), so a reply reaches
Feishu whichever process wrote it; answers about a work item go into its thread.

## Done in v1

- Memory distiller consumer → layered `MEMORY.md` files, cross-day recall
- Feishu channel binding (p2p ↔ main thread, reply-thread ↔ work item)
- Worktree workspace provider for coding work items
- Side-effect permission gate as a pi extension (`tool_call` intercept)
- Web UI (chat, work items, events, worklog, status) with SSE + i18n
- Per-agent event loop: durable mailboxes, turn batching, non-blocking dispatch,
  restart recovery, priority lanes, idle work
- Work tree: fan-out to child work items, bubbling escalations, policy capture,
  cascading cancel, causal budgets
- Conversation UI: task cards, attribution chips with re-routing, in-progress
  tray, focus panel, off-screen notices with a digest
- Conversation history: server-side search, windows around any event with
  permalinks, day index, hide-a-message, a bounded Primary context with recall

## Roadmap

- Perspective-diverse verification for high-stakes executions
