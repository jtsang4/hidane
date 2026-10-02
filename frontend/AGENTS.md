# Web UI Agent Rules

## Scope

- This directory is the Svelte 5 frontend for hidane.
- Use Svelte components and TypeScript for new code. Do not add React, React DOM,
  JSX, or React-specific adapters.
- Keep the app as a Vite SPA unless a task explicitly authorizes a SvelteKit and
  deployment migration.

## Agent workflow

- Before editing `.svelte`, `.svelte.ts`, or `.svelte.js`, consult the Svelte MCP
  documentation: discover sections first, then load every relevant section.
- After writing Svelte code, run the Svelte static fixer until it reports no
  actionable issues, then run `pnpm -C frontend check`.
- Keep framework-independent domain logic in `src/lib/*.ts` so it can be tested
  without a browser or component renderer.
- Prefer small, disjoint work units. Do not have multiple agents edit the app
  shell, router, or the same page concurrently.

## Runtime contracts

- The existing API, bearer-token behavior, SSE event names, SSE query-token
  behavior, event-derived pending state, cursor pagination, and browser
  notification semantics are compatibility contracts.
- `/api/events/stream` carries two event names: `hidane` for durable log events
  and `stream` for the text of a reply still being written. `stream` frames are
  ephemeral, are never appended to the log, and must not trigger a query
  invalidation — they arrive per token.
- `/boot.js` (served by the Go host, loaded before the module script) publishes
  `window.hidaneBoot = { desktop, auth, version }`; read it only through
  `src/lib/boot.ts`. With `auth: false` (desktop webview) there is no token gate.
- Open the live channel only through `src/lib/stream.ts` `openLiveStream`: SSE in
  the browser, Wails `Events.On("hidane:frame")` in the desktop app (falling back
  to SSE). Both deliver the same `hello`/`hidane`/`stream`/`ping` frames.
- `src/lib/api.ts`, `grouping.ts`, `images.ts`, `live.ts`, `liveText.ts`,
  `pending.ts`, `search.ts`, `notify.ts`, and their tests should be preserved
  unless a change is required by the Svelte adapter.
- Browser-only APIs (`window`, `document`, `localStorage`, `EventSource`,
  `Notification`, and `File`) must be accessed from client lifecycle code or
  guarded helpers.
- Do not add an unauthenticated API call or change server routes as part of a
  frontend framework migration.

## UI and i18n

- All visible UI text belongs in `src/i18n/resources.ts` and must exist in both
  `zh` and `en`.
- Preserve the current dark theme, responsive navigation, scroll-container
  behavior, keyboard focus, and accessible names.
- Runtime agent Markdown must be rendered with the existing GFM behavior and
  must not turn untrusted response content into executable HTML.

## Required checks

- `pnpm -C frontend check`
- `pnpm -C frontend test`
- `pnpm -C frontend build`
- After a visible change: `make screenshots`, then look at the pages you touched
  in zh and en, in each flavour (`bin/screenshots/<page>-<zh|en>-<desktop|browser|phone>.png`:
  the desktop app's UI, the browser at window size, the browser at phone width).
- `boot().desktop` decides the flavour. The desktop window has a hidden-inset
  title bar: keep the top-left 78×52px free (traffic lights) and mark toolbar rows
  with `drag-region`; interactive elements are `no-drag` globally (`styles.css`).
- Never call `confirm()`/`alert()`/`prompt()` — WKWebView implements none of
  them. Use `confirmAction()` (`src/lib/confirm.svelte.ts`).
- `frontend/e2e/README.md` lists what each Playwright spec guards; extend the one
  that covers your change (`make e2e`).
- Before handoff, also run the relevant root checks from the repository
  `AGENTS.md`. A claim that something works must include command output or
  observable runtime evidence.
