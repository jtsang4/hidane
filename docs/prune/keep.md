# Prune Keep List

Code that prune passes must not delete or refactor. Maintained by the prune skill; edit freely.

## Inventory Assets

Do not delete unused members or refactor internals.

| Path | What it is | How it is updated | Confirmed |
| --- | --- | --- | --- |
| `frontend/src/components/ui/` | design-system primitives over bits-ui | by hand, per `frontend/DESIGN.md` | 2026-10-05 (project rules) |
| `.claude/agents/svelte-file-editor.md` | the Svelte project's agent template | upstream copy | 2026-10-05 by user |

## External Contracts

Consumers live outside this repository.

| Path or symbol | Consumer | Confirmed |
| --- | --- | --- |
| `/api/*`, `/webhook/:name`, `/health`, `/boot.js` | the SPA, the desktop host, webhook senders | 2026-10-05 (project rules) |
| `HIDANE_*` environment variables, `settings.json`, `POLICY.json` keys | people configuring hidane | 2026-10-05 (project rules) |
| `HIDANE_BRANCH`, `HIDANE_WORKTREE_PATH`, `HIDANE_WORK_ITEM_ID`, `HIDANE_SOURCE_CHECKOUT_PATH` | the `setup` / `teardown` scripts a repository's `hidane.json` runs | 2026-10-05 by user |

## Intentional Complexity

Looks prunable but is deliberate.

| Path or symbol | Why it stays | Confirmed |
| --- | --- | --- |
| the four Playwright projects in `frontend/playwright.config.ts` | each is a shipped target: webkit-desktop (macOS/Linux webview), chromium-desktop (Windows WebView2), chromium and webkit (serve in a browser); dropping `webkit` loses serve-in-Safari | 2026-10-05 by user |
| scenario 5I driving a real browser | the user declined reducing it to a screenshot judgement | 2026-10-05 by user |
| fixed default ports 2718 / 2719 / 2796–2798 | deliberate uncommon defaults (AGENTS.md); `HIDANE_E2E_PORT` only overrides | 2026-10-05 (project rules) |
| `FAKEAGENT_DELAY_MS` alongside `FAKEAGENT_WORKER_DELAY_MS` | some tests catch a reasoning turn mid-flight (`TestAnInterruptedTurnKeepsItsMessages`, the agentcli driver tests) | 2026-10-05 |
| readers of old event shapes (e.g. "older runtimes wrote …" in `frontend/src/lib/conversation.ts`) | the log is append-only: history keeps those shapes for good | 2026-10-05 by user |
| the provider-compatibility rule in both `internal/settings` and `frontend/src/lib/settings.ts` | the frontend copy gives live feedback in the form before anything is saved | 2026-10-05 by user |
| `desktopOnly`'s `Host == nil` check (`internal/api`) | in serve mode a remote client must never act on the host machine | 2026-10-05 by user |
| the chat handler's own target lookup before `System.SubmitMessage` | the API answers 404 for any unreadable target before every other refusal and before images are stored; `SubmitMessage` serves the CLI and Feishu in a different order (limits first, database errors passed through). Moving the check into the one door changed one side or the other (A7, reverted 2026-10-05) | 2026-10-05 by user |
| `repos.startPoint`'s `From` check | duplicates `resolveRepos`, but keeps the git layer from panicking on a bad branch | 2026-10-05 by user |
| `docs/wails-migration.md` | a history document: it describes the design at the time of the migration (it still names the dropped `threads` table) | 2026-10-05 by user |
| differing kind lists named against `kernel.AnswerKinds` / `frontend/src/lib/kinds.ts` (CLI `follow`, `projections.Recent`, Feishu outbound, `liveText.ts`), `projections.VisibleSQL`, the distiller's and the Primary's own kind sets | each leaves kinds out on purpose (see the comment at each); unifying them changes what that reader shows | 2026-10-05 by user |
| the fake CLI's `代码示例` / `流程图` answers | `shell.spec` (code copy, Mermaid) and `screenshots.mjs` both send them through the shared e2e server; moving them into demo data only adds setup | 2026-10-05 by user |
