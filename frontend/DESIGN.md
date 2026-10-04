# hidane design system

The single reference for how the UI looks and moves. Every screen is built
from what is listed here; anything new either reuses it or extends it here
first.

## Where it lives

| What | Where |
|---|---|
| Tokens: color, type, radius, shadow, motion | `src/styles.css`, the `@theme` block |
| Primitives: Button, Input, Textarea, Select, Combobox, Checkbox, Switch, DatePicker, Badge, Card | `src/components/ui/` |
| Shared class patterns for controls that are not one component (fields, popovers, toolbar buttons, segmented controls, sheets) | `src/lib/styles.ts` |
| Shared building blocks | `EmptyState`, `BrandMark`, `PathText`, `Page` + `Toolbar`, `MoreButton` in `src/components/` |
| Enforcement | `test/design-system.test.ts` (runs in `pnpm test` / `make test`) |

Tailwind's own palette, shadows and radii are switched off in `@theme`
(`--color-*: initial` and friends). Only the tokens below exist: a class such
as `bg-orange-500`, `shadow-xl` or `rounded-3xl` generates nothing, and the
design-system test names the file and line.

## Principles

1. **Flat and compact.** A desktop tool, not a web page: 13px body text, 28px
   controls, rows of 28–44px.
2. **Separate layers by light, not by lines.** Surfaces differ by small
   lightness steps; borders are translucent hairlines.
3. **Orange means something.** The primary color is for the one action a view
   is about (send, create, answer) and for live or focused state. Never a
   decoration, never a category label.
4. **Only floating things cast shadows.** Popovers, dialogs, toasts.
5. **Things arrive; nothing bounces.** Short, decelerating motion, all of it
   off under `prefers-reduced-motion`.
6. **The ember (火種) is the signature.** Used sparingly: the flame mark, the
   sign-in glow, work in progress that breathes and smolders.

## Tokens

### Color

| Token | Use |
|---|---|
| `background` | The window |
| `surface` | Cards, sidebar, lists |
| `surface-2` | A raised fill inside a surface (agent bubble, segment) |
| `popover` | Menus, popovers, dialogs, toasts |
| `border` | Hairlines between and around things |
| `input` / `input-strong` | A field's border / under the pointer |
| `field` | A field's fill |
| `accent` / `accent-strong` | Hover and selected fill / hover on something already accent |
| `well` | Recessed: code, a segmented control's groove |
| `track` | A switch that is off, a sheet's grab handle |
| `overlay` | Dims the app behind a dialog |
| `sheen` | The top-lit gradient on raised buttons (`from-sheen to-transparent`) |
| `foreground` / `muted` | Text / secondary text and icons |
| `primary` / `primary-foreground` | The ember: the main action, live and focused state / text on it |
| `success`, `danger`, `danger-foreground` | Outcomes and destructive actions |
| `knob` | A switch's thumb |
| `ember-tip`, `ember-base` | The flame mark's gradient (`BrandMark`) |

Tints use the opacity modifier on a token (`bg-primary/12`, `text-foreground/80`),
never a new literal.

### Type

| Class | Size | Use |
|---|---|---|
| `text-2xs` | 11px | Captions, timestamps, group labels |
| `text-xs` | 12px | Secondary text, small controls |
| `text-sm` | 13px | Body and controls (the default) |
| `text-base` | 14px | Page and panel titles |
| `text-xl` | 20px | Brand moments only (sign-in) |
| `text-code` | 0.85em | Inline code, relative to its text |

Latin is Geist (`font-sans`) and Geist Mono (`font-mono`), bundled; CJK falls
through to the platform's UI face. Ids, paths, cron expressions and other
machine tokens are mono. Numbers that change in place use `tabular-nums`.
Weight carries hierarchy sparingly: `font-medium` for section titles and the
primary action, `font-semibold` for page, panel and dialog titles.

### Radius

`rounded-sm` 4px (rows, chips, menu items, badges), `rounded-md` 6px
(controls, small panels), `rounded-lg` 8px (cards, popovers, bubbles),
`rounded-xl` 12px (dialogs, sheets), `rounded-2xl` 16px (brand tiles),
`rounded-full` (dots, pills, switches). A shape nested in another takes the
outer radius minus the padding between them: a menu item (`sm`) in a popover
(`lg`, `p-1`).

### Shadow

| Class | Use |
|---|---|
| `shadow-popover`, `shadow-dialog` | Floating layers |
| `shadow-primary`, `shadow-primary-outline`, `shadow-danger` | Button variants (via `Button`) |
| `shadow-hairline`, `shadow-ember-edge` | Inside edges that take no layout space |
| `shadow-segment`, `shadow-knob` | A raised segment, a switch thumb |
| `shadow-ember-glow`, `shadow-ember-spark` | The focused task's halo; spark and smolder light |

### Motion

`animate-pop-in` (popovers and toasts), `animate-dialog-in` (dialogs),
`animate-fade-in` (overlays), `animate-rise-in` (new content, sheets),
`animate-ember` (anything in progress breathes), `animate-smolder` (a running
task's top edge), `animate-spark` (sending). Use `transition-colors` for
hover changes. Every animation is reduced to nothing under
`prefers-reduced-motion`; Svelte transitions check `prefersReducedMotion`
from `svelte/motion` themselves.

## Components and when to use them

### Button

| Variant | When |
|---|---|
| `default` (solid ember) | The one action a view is about: send, create, save, answer. At most one per view. Disabled, it draws as an outline. |
| `soft` | A visible but secondary "new / add" in a toolbar |
| `secondary` | Cancel, filters, ordinary actions |
| `ghost` | Toolbar and inline actions; icon buttons |
| `danger` | The confirming action of a destructive dialog |

Sizes: `default` 28px, `sm` 24px (inside cards and dense rows), `lg` 32px
(sign-in, wide forms), `icon` 28px, `icon-sm` 24px. A field and a button on
one row use the same size. On a touch screen every control keeps a 36px
target (`coarse:` variant) however small it is drawn. Delete buttons are
`ghost` icons that turn danger on hover; the dialog that confirms them uses
`danger`.

### Fields and choices

`Input`, `Textarea`, `Select`, `Combobox`, `DatePicker` share `field` and
`fieldSize` from `lib/styles.ts`. Use `Select` for a closed set, `Combobox`
when typing a value is allowed, `Switch` for on/off that applies at once,
`Checkbox` inside a form, and the `segmented` pattern for two to four mutually
exclusive options shown at once. Never a native `<select>`.

### Popovers, menus, dialogs

Popover and menu content use `popover` / `popoverItem` / `popoverLabel`. A
destructive menu item goes last, after a separator. Dialogs are `rounded-xl`,
`bg-popover`, `shadow-dialog` over `bg-overlay`; on a phone they become bottom
sheets (`sheetOnPhone` plus `sheetHandle`). Confirm destructive actions with
`confirmAction()`, never `confirm()`.

### Pages and lists

`Page` gives the 52px toolbar (`Toolbar`) and a centred content column.
Toolbar actions are `toolbarButton` (`toolbarButtonOn` while pressed) or
`Button`s. Lists of rows are one card with dividers
(`divide-y divide-border rounded-lg border border-border bg-surface`), not a
card per row. Section labels sit flush with the card edge, `text-sm
font-medium`. Every page-level empty state is an `EmptyState`. Long paths go through
`PathText`, which wraps only after a slash.

### Status

Badges label state, not category: `default` (ember) for live work or something
waiting on you, `success`
and `danger` for outcomes, `muted` for everything else. The ordinary state
(an open task) carries no badge. Work in progress shows an `animate-ember`
dot.

## Brand

The flame mark is `BrandMark` (never the generic Lucide flame). Brand moments
are few and fixed: the sign-in screen (`ember-light` glow over `grain`), the
About tile, the empty conversation (`EmptyState ember`), the sidebar mark.
The ember lights things that are alive: user bubbles, a focused task, work in
progress, the spark on send. Icons are Lucide at a 1.5 stroke (set once in
`App.svelte`), 12–16px in the interface; larger only in brand and empty-state
tiles. Inside a `Button` the size comes from the button.

## Changing the system

1. Need a value the tokens do not have? Add a token to `@theme` with a
   comment saying what it is for, and add it to this file. Do not reach for
   an arbitrary value (`bg-[…]`, `shadow-[…]`, `text-[13px]`) or a literal
   color; the design-system test rejects them.
2. Need a new control? Build it in `components/ui/` from the tokens, or add a
   pattern to `lib/styles.ts` when it cannot be one component.
3. Look at the result: `make screenshots` (or `ONLY=<page>` while iterating),
   in zh and en, desktop, browser and phone.
