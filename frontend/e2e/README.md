# End-to-end tests

Playwright drives the **real Go backend** (`bin/hidane-nogui serve`, started by
`serve.mjs` on a fresh `HIDANE_HOME`) with `bin/fake/{claude,codex,pi}` standing
in for the agent CLIs (`cmd/fakeagent`: real wire protocols, real guard hook,
scripted answers — a message containing `hello`/`你好` is small talk, `代码示例` gets a reply holding a code block, `流程图` one holding a Mermaid flowchart, anything
else becomes a work item whose worker writes `result.txt` (`REPO=<name or path>`,
`FROM=<work item>`, `INPLACE` (`UNASKED`: without quoting the person), `ROUTE=<work item>` steer the Primary's repo choices; `ASK_OPTIONS` makes the Manager ask a question offering 周五晚上 / 周六早上); `FAKE_FAIL` fails the
CLI; `FAKEAGENT_DELAY_MS` slows each turn, `FAKEAGENT_WORKER_DELAY_MS` only a worker's). Every spec runs in zh and fails on any
uncaught page error, any console error, and any native `confirm()`/`alert()`/
`prompt()` (the desktop webview implements none of them).

Four projects: `chromium` and `webkit` run the UI as `hidane serve` serves it to a
browser; `chromium-desktop` and `webkit-desktop` run the **desktop app's UI** —
the fixture answers `/boot.js` with `{desktop: true, auth: false}` (the token
still sits in localStorage, and `apiFetch` sends it whenever present), serves an
empty `/wails/runtime.js` so the live stream falls back to SSE, and a fake host
answers `/api/desktop/*` and `/api/work-items/*/reveal`, recording each call in
the `hostCalls` fixture (`test.use({ fakeHost: false })` leaves them 404, as in
serve mode). WebKit stands in for the macOS WKWebView. `token-gate.spec.ts` is
browser-only; `desktop.spec.ts` is desktop-only; everything else runs in all four.

```sh
make build-nogui fakeagent        # the SPA is embedded at build time: rebuild after UI changes
pnpm -C frontend e2e              # everything: four projects
pnpm -C frontend exec playwright test e2e/settings.spec.ts --project=webkit-desktop
make screenshots                  # every page and settings section, zh/en, desktop/browser/phone → bin/screenshots/
make screenshots ONLY=run-as      # only the pages and states whose names start with these prefixes (comma-separated)
make readme-shots                 # the root README's pictures → docs/images/ (English; the fakes read their lines from FAKEAGENT_DEMO)
```

Two servers run: the normal one on 2797, and a slow one on 2798
(`FAKEAGENT_WORKER_DELAY_MS=8000`) for anything that must be caught mid-run.
`screenshots.mjs` uses 2796, `readme-shots.mjs` 2795. `HIDANE_E2E_PORT=<n>` moves them to n, n+1, n−1 and n−2,
so runs in two checkouts at once do not collide — keep the ranges apart (e.g.
2897): a server clears its port's leftover temp homes before it binds. Tests
share one server, so each uses unique text.

## What each spec guards — extend the one that covers what you change

| Spec | Guards |
|---|---|
| `token-gate.spec.ts` | (browser only) the token gate in serve mode: shown without a token, a wrong token is rejected with a toast, the right one opens the app and survives a reload; `/api/*` without a bearer token is 401 |
| `conversation.spec.ts` | the full loop on **each** CLI (claude, codex, pi): composer → Primary creates a work item → Manager → guarded worker writes `result.txt` → `已完成` reply grouped under the question; the event chain order; every reply's `payload.root`; the tool name per CLI in `side_effect.intent`; the view stays pinned to the newest reply (no "回到最新", no new-reply notice); the focus panel's artifact list (download in the browser, show-in-folder in the desktop UI) and execution. On the claude run: the message line's 改 menu (open tasks and 新任务; Esc returns focus to it) and the 按日期 popover listing today. Also: small talk gets a reply and no work item; a deterministic 1,000-event archive stays bounded to 320 events while paging both ways, preserves the visible turn through eviction, returns to the live edge and reopens an evicted message; a `?at=` permalink opens at its message and, back at the live edge, drops `?at=` and shows no false "正在判断归属…"; at a 900px window a late answer's "有新回复" notice leaves the newest message uncovered while the view stays pinned; the composer's two pickers: **Task** (agent → CLI-listed models in the model combobox → that model's efforts) pins the created task (`runAs`, worker really ran on codex, role settings untouched), changes an addressed task at once (the **Chat** picker steps aside there), and is remembered for new tasks; **Chat** saves `roles.primary` at once (other roles untouched, the next small talk still answered) and links to Settings → Roles; all three picker scopes keep a pi provider unsaved until a model is chosen, discard an incomplete choice on close, and refuse to clear a required model; a favorite stores the whole combination and applies it in one click to either, and survives a reload; Esc closes a picker and gives focus back to its button; with a scrollable history, hiding a message through its right-click menu leaves the view following the next replies (a click in the log is not a scroll gesture) |
| `addressing.spec.ts` | the conversation as an entry, the tasks as the work: `@` in the composer names two tasks (picked from the list by keyboard, shown as removable chips, the placeholder and run-on picker say "these 2 tasks"), one `POST /api/chat` with `targets`, each Manager's report `report: true` under the one message, shown as a one-line row opened in place, the "同时发给" chips; `/do` offers `/done`, with no task named it asks for one, with one named it marks it done and posts no message; a question's options (`ASK_OPTIONS`) answer it in one click from the conversation (`user.message` with `replyTo`), the result then waits as 待审阅 on the 待处理 page (sidebar link with a count) until 标记完成; a question answered in words from the 待处理 page; a Primary answer's menu 转成任务 creates a task (`by: user`, `created`), opens it, and is gone from the menu after |
| `settings.spec.ts` | Settings → Agent CLI: detection with versions, a path override that saves on Enter/blur only; Providers: a preset shows only a masked key (never in the page, `/api/settings` or events), delete asks first and a provider in use is refused (409, its reason shown); Roles: every choice auto-saves, the model on Enter, a click in the model field lists the CLI's own models on its own login, an incompatible agent × provider is shown unsaved (server 400 for the same pair); a pi provider without a model is shown unsaved with Test disabled until the model is filled; the role Test button's real round trip |
| `memory-policies.spec.ts` | memory add / list / forget (in-app confirm) with `memory.promoted` and `memory.forgotten`; a task's own `MEMORY.md` listed under that task and forgettable (`memory.forgotten` carries its `workItemId`); Settings → Safety rules (also reached from the old `/policies`): add / show / delete (in-app confirm) with their events |
| `schedules-status.spec.ts` | an HTTP schedule's "wake Primary" checkbox (ticked through its label text) reaches the saved spec; a prompt schedule created in the UI, run now, its run history and its answer in the conversation, deleted after an in-app confirm; Settings → Runtime status (also reached from `/status`): runtime, roles and detected CLIs |
| `webhook.spec.ts` | a signed webhook is captured, triaged and answered (`rootKind: external`, visible in the UI) |
| `cancel.spec.ts` | Stop on a running card (slow server): the running task is in the sidebar's in-progress list and its menu offers Stop; cancelling the confirm leaves it running; confirming records `execution.cancelled` before `execution.finished` with `cancelled: true`, the "执行已取消。" reply, the card shows 已中止 not 失败 |
| `shell.spec.ts` | the window shell, both flavours: ⌘, opens settings at `/settings/general`, Esc (first letting go of a field, and closing an open select list without leaving), ⌘, again and Back return to the page it was opened from; the work log's day picked from its calendar popover by keyboard, and back with 今天; the ⌘K palette (commands by keyboard, a settings section by English keyword, a task and a message from the search; Esc); ⌘N creates a work item and opens it; ⌘L focuses the composer; the confirm dialog (Cancel, Tab kept inside it, Esc with focus returned, Enter); a message's menu by right click and by ⋯ (copy text via the host clipboard in the desktop UI, no "copy link" there; hide after confirm → `message.redacted`); a code block in a reply copies its code with its 复制代码 button (shown on hover, 已复制 after; the host clipboard in the desktop UI); a ```mermaid block in a reply is drawn as a diagram in its place, its source still copied by 复制代码; a task's menu (open, mark done, archive after confirm, reveal workspace in the desktop UI); ⌘B collapse survives a reload; an image dropped on the conversation is attached; every icon (imported one module each) still gets the shell's `setLucideProps` stroke width |
| `worktrees.spec.ts` | a message naming a local repo (`REPO=<path>` steers the fake Primary) gives the task its own worktree: the repo's `hidane.json` setup ran first, the worker wrote into the worktree and not the person's checkout, the card names repo and branch; its panel's 改动 lists `result.txt` as 未跟踪 and opens its diff; Tasks → Worktrees (`?view=worktrees`) lists it with its uncommitted change, archiving asks, then asks again about the uncommitted change (409 until forced), removes the directory, keeps the branch and records `checkout.archived`; archived ones show with 含已归档 and open their task. A registered repo moved away is marked missing in the list, asked about in the conversation (`escalation` reason `repo_missing`, its own turn labelled 仓库检查) and can be removed (`repo.forgotten`) |
| `desktop.spec.ts` | (desktop only) no token gate, sign-out or live-status, and no browser notification settings; the live stream works without the Wails runtime (SSE fallback); About opens the data directory through the host and copies its path; a task finishing in the background posts a system notification and a Dock badge, cleared when the window comes back; with both switched off the host is left alone; the webview's own context menu is suppressed except in fields; with the host endpoints 404ing, nothing surfaces as an error toast; with the host endpoints failing, a host action the person asked for (About → 在访达中显示) explains "only the desktop app can do this" in a neutral toast, not a red one |

Not covered here, and where it is instead: the desktop shell, the native menu's
`hidane:command` events and the Wails event transport (`make smoke-gui`; the
command unwrapping and de-duplication are unit-tested in `test/commands.test.ts`); real CLIs (`make smoke-live`, the
`hidane-live-check` skill); everything behind the API without a browser
(`go test ./internal/api/`); Feishu (`go test ./internal/feishu/`).
