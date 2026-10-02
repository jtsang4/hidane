import type { Attachment } from "svelte/attachments";

const FOCUSABLE = [
  "a[href]",
  "button:not([disabled])",
  "input:not([disabled]):not([type='hidden'])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !el.closest("[hidden], [inert]"));
}

/**
 * Keep Tab inside a modal surface, focus its `[data-autofocus]` element (or
 * the first focusable one) when it opens, and give focus back to whatever had
 * it when the surface goes away.
 */
export function trapFocus(): Attachment<HTMLElement> {
  return (node) => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const initial = node.querySelector<HTMLElement>("[data-autofocus]") ?? focusables(node)[0] ?? node;
    initial.focus();
    if (initial instanceof HTMLInputElement || initial instanceof HTMLTextAreaElement) initial.select();

    const onKeydown = (event: KeyboardEvent) => {
      if (event.key !== "Tab") return;
      const items = focusables(node);
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const first = items[0]!;
      const last = items[items.length - 1]!;
      const active = document.activeElement;
      if (event.shiftKey && (active === first || !node.contains(active))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && (active === last || !node.contains(active))) {
        event.preventDefault();
        first.focus();
      }
    };
    node.addEventListener("keydown", onKeydown);
    return () => {
      node.removeEventListener("keydown", onKeydown);
      // Something else may already have taken focus on purpose (the composer after ⌘L).
      const active = document.activeElement;
      if (previous && previous.isConnected && (active === null || active === document.body || node.contains(active))) {
        previous.focus();
      }
    };
  };
}
