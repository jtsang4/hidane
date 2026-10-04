# Prune Keep List

Code that prune passes must not delete or refactor. Maintained by the prune skill; edit freely.

## Inventory Assets

Do not delete unused members or refactor internals.

| Path | What it is | How it is updated | Confirmed |
| --- | --- | --- | --- |
| `frontend/src/components/ui/` | design-system primitives over bits-ui | by hand, per `frontend/DESIGN.md` | 2026-10-05 (project rules) |

## External Contracts

Consumers live outside this repository.

| Path or symbol | Consumer | Confirmed |
| --- | --- | --- |
| `/api/*`, `/webhook/:name`, `/health`, `/boot.js` | the SPA, the desktop host, webhook senders | 2026-10-05 (project rules) |
| `HIDANE_*` environment variables, `settings.json`, `POLICY.json` keys | people configuring hidane | 2026-10-05 (project rules) |

## Intentional Complexity

Looks prunable but is deliberate.

| Path or symbol | Why it stays | Confirmed |
| --- | --- | --- |
| the four Playwright projects in `frontend/playwright.config.ts` | each is a shipped target: webkit-desktop (macOS/Linux webview), chromium-desktop (Windows WebView2), chromium and webkit (serve in a browser); dropping `webkit` loses serve-in-Safari | 2026-10-05 by user |
| scenario 5I driving a real browser | the user declined reducing it to a screenshot judgement | 2026-10-05 by user |
| fixed default ports 2718 / 2719 / 2796–2798 | deliberate uncommon defaults (AGENTS.md); `HIDANE_E2E_PORT` only overrides | 2026-10-05 (project rules) |
| `FAKEAGENT_DELAY_MS` alongside `FAKEAGENT_WORKER_DELAY_MS` | some tests catch a reasoning turn mid-flight (`TestAnInterruptedTurnKeepsItsMessages`, the agentcli driver tests) | 2026-10-05 |
