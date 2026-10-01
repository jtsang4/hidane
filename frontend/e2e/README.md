# End-to-end tests

Playwright drives the **real Go backend** (`bin/hidane-nogui serve`, started by
`serve.mjs` on a fresh `HIDANE_HOME`) with `bin/fake/{claude,codex,pi}` standing
in for the agent CLIs (`cmd/fakeagent`: real wire protocols, real guard hook,
scripted answers — a message containing `hello`/`你好` is small talk, anything
else becomes a work item whose worker writes `result.txt`; `FAKE_FAIL` fails the
CLI; `FAKEAGENT_DELAY_MS` slows each turn). Every spec runs on chromium and on
webkit (WebKit stands in for the desktop app's WKWebView), in zh, and fails on
any uncaught page error.

```sh
make build-nogui fakeagent        # the SPA is embedded at build time: rebuild after UI changes
pnpm -C frontend e2e              # everything, both browsers
pnpm -C frontend exec playwright test e2e/settings.spec.ts --project=webkit
make screenshots                  # every page, zh/en, desktop/phone → bin/screenshots/
```

Two servers run: the normal one on 2797, and a slow one on 2798
(`FAKEAGENT_DELAY_MS=8000`) for anything that must be caught mid-run.
`screenshots.mjs` uses 2796. Tests share one server, so each uses unique text.

## What each spec guards — extend the one that covers what you change

| Spec | Guards |
|---|---|
| `token-gate.spec.ts` | the token gate in serve mode: shown without a token, a wrong token is rejected with a toast, the right one opens the app and survives a reload; `/api/*` without a bearer token is 401 |
| `conversation.spec.ts` | the full loop on **each** CLI (claude, codex, pi): composer → Primary creates a work item → Manager → guarded worker writes `result.txt` → `已完成` reply grouped under the question; the event chain order; every reply's `payload.root`; the tool name per CLI in `side_effect.intent`; the focus panel's artifact list and execution. Also: small talk gets a reply and no work item |
| `settings.spec.ts` | CLI detection with versions; a provider from a preset shows only a masked key (never in the page, `/api/settings` or events); role ↔ provider compatibility (warning, disabled save, server 400); 409 when deleting a provider in use; the role Test button's real round trip |
| `memory-policies.spec.ts` | memory add / list / forget with `memory.promoted` and `memory.forgotten`; policy rule add / show / delete with their events |
| `schedules-status.spec.ts` | a prompt schedule created in the UI, run now, its run history and its answer in the conversation; the status page's runtime, roles and detected CLIs |
| `webhook.spec.ts` | a signed webhook is captured, triaged and answered (`rootKind: external`, visible in the UI); unsigned, wrongly signed or replayed-body deliveries are 401 and leave no event |
| `cancel.spec.ts` | Stop on a running card (slow server): `execution.cancelled` before `execution.finished` with `cancelled: true`, the "执行已取消。" reply, the card shows 已中止 not 失败 |

Not covered here, and where it is instead: the desktop shell and Wails event
transport (`make smoke-gui`); real CLIs (`make smoke-live`, the
`hidane-live-check` skill); everything behind the API without a browser
(`go test ./internal/api/`); Feishu (`go test ./internal/feishu/`).
