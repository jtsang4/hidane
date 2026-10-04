/**
 * Class strings shared by controls that are not built on one component:
 * every text field, select trigger and popover list draws from these, so a
 * native `<input>` and a bits-ui trigger cannot drift apart.
 */

/** A text field or a select trigger, without its size. */
export const field =
  "rounded-md border border-input bg-field text-foreground placeholder:text-muted/70 transition-colors hover:border-input-strong focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/15 disabled:pointer-events-none disabled:opacity-50";

/** Heights match `Button`'s sizes, so a field and a button on one row line up. */
export const fieldSize = {
  default: "h-7 px-2.5 text-sm coarse:min-h-9",
  sm: "h-6 px-2 text-xs coarse:min-h-9",
} as const;

/** The floating panel of a select, combobox, menu or date picker. */
export const popover = "z-[65] animate-pop-in rounded-lg border border-border bg-popover p-1 shadow-popover outline-none";

/** One row in a popover list; bits-ui marks the keyboard/pointer row `data-highlighted`. */
export const popoverItem =
  "flex w-full items-center gap-2 rounded-sm px-2 py-1 text-left text-sm outline-none select-none data-disabled:opacity-50 data-highlighted:bg-accent";

/** A heading over a group of popover rows. */
export const popoverLabel = "px-2 pt-2 pb-1 text-2xs font-medium text-muted";

/** A quiet button in a page's toolbar row: icon plus a short label, filled only on hover or when pressed. */
export const toolbarButton =
  "flex h-7 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-md px-2 text-xs text-muted transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary/70 coarse:min-h-9 coarse:min-w-9 [&_svg]:size-3.5";

/** `toolbarButton` while it is pressed or its popover is open. */
export const toolbarButtonOn = "bg-primary/12 text-primary hover:bg-primary/20 hover:text-primary";

/** A segmented control: one track holding a choice among siblings; the chosen segment is raised. */
export const segmented = "inline-flex gap-0.5 rounded-md bg-well p-0.5 shadow-hairline";
export const segment =
  "h-6 min-w-0 truncate rounded-sm px-2.5 text-xs transition-colors disabled:opacity-40 focus-visible:outline-2 focus-visible:outline-primary/70 coarse:min-h-9";
export const segmentOn = "bg-surface-2 text-foreground shadow-segment";
export const segmentOff = "text-muted hover:text-foreground";

/** The dimmed backdrop behind a dialog; the dialog adds its z layer. */
export const dialogOverlay = "fixed inset-0 animate-fade-in bg-overlay backdrop-blur-[2px]";

/** A dialog's own surface; the dialog adds its position, size and z layer. */
export const dialogSurface = "animate-dialog-in rounded-xl border border-border bg-popover shadow-dialog outline-none";

/** A centred dialog that becomes a bottom sheet on a phone, within reach of the thumb. */
export const sheetOnPhone =
  "max-sm:inset-x-0 max-sm:top-auto max-sm:bottom-0 max-sm:left-0 max-sm:w-full max-sm:max-w-none max-sm:translate-x-0 max-sm:translate-y-0 max-sm:animate-rise-in max-sm:rounded-b-none max-sm:pb-[max(1rem,env(safe-area-inset-bottom))]";

/** The grab handle at the top of a bottom sheet. */
export const sheetHandle = "mx-auto -mt-1 mb-3 h-1 w-9 rounded-full bg-track";
