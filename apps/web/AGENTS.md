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
  actionable issues, then run `pnpm -C apps/web check`.
- Keep framework-independent domain logic in `src/lib/*.ts` so it can be tested
  without a browser or component renderer.
- Prefer small, disjoint work units. Do not have multiple agents edit the app
  shell, router, or the same page concurrently.

## Runtime contracts

- The existing API, bearer-token behavior, SSE event names, SSE query-token
  behavior, event-derived pending state, cursor pagination, and browser
  notification semantics are compatibility contracts.
- `src/lib/api.ts`, `grouping.ts`, `images.ts`, `live.ts`, `pending.ts`,
  `search.ts`, `notify.ts`, and their tests should be preserved unless a change
  is required by the Svelte adapter.
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

- `pnpm -C apps/web check`
- `pnpm -C apps/web test`
- `pnpm -C apps/web build`
- Before handoff, also run the relevant root checks from the repository
  `AGENTS.md`. A claim that something works must include command output or
  observable runtime evidence.
