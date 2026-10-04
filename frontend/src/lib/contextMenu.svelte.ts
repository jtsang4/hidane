import type { Component } from "svelte";

/**
 * One menu at a time, rendered by `MenuHost`. The same request opens from a
 * right click (at the pointer) or from a "⋯" button (under the button), so a
 * context menu and its hover twin can never drift apart.
 */
export interface MenuEntry {
  id: string;
  label: string;
  icon?: Component<{ size?: number; class?: string }> | undefined;
  danger?: boolean;
}

export interface MenuRequest {
  key: number;
  x: number;
  y: number;
  /** `end`: the menu's right edge sits at `x` (under a "⋯" button at the right of a row). */
  align: "start" | "end";
  label: string;
  items: MenuEntry[];
  onselect: (id: string) => void;
  /** Where focus goes back when the menu closes without a choice. */
  returnFocus: HTMLElement | null;
}

export const menuState = $state<{ current: MenuRequest | null }>({ current: null });

let nextKey = 1;

export type MenuPlacement = Pick<MenuRequest, "x" | "y" | "align" | "returnFocus">;

/** At the pointer, for a right click. */
export function atPointer(event: MouseEvent): MenuPlacement {
  return { x: event.clientX, y: event.clientY, align: "start", returnFocus: null };
}

/** Under a button, right-aligned with it, for a "⋯" trigger. */
export function belowElement(element: HTMLElement): MenuPlacement {
  const rect = element.getBoundingClientRect();
  return { x: rect.right, y: rect.bottom + 4, align: "end", returnFocus: element };
}

export function openMenu(placement: MenuPlacement, label: string, items: MenuEntry[], onselect: (id: string) => void): void {
  if (items.length === 0) return;
  menuState.current = { ...placement, key: nextKey++, label, items, onselect };
}

export function closeMenu(): void {
  menuState.current = null;
}

export function menuOpen(): boolean {
  return menuState.current !== null;
}

/**
 * WebKit (Safari, and the desktop app's WKWebView) selects the word under the
 * pointer on a right click, before `contextmenu` fires — so "is text
 * selected?" must be asked about the moment before the press, or every right
 * click on a message would look like a request for the system's Copy menu.
 */
let pressed: { range: Range | null } | null = null;

function selectedRange(selection: Selection | null): Range | null {
  return selection && !selection.isCollapsed && selection.rangeCount > 0 && selection.toString().trim() ? selection.getRangeAt(0).cloneRange() : null;
}

export function watchContextSelection(doc: Document = document): () => void {
  const onDown = (event: MouseEvent) => {
    if (event.button === 2) pressed = { range: selectedRange(doc.getSelection()) };
  };
  doc.addEventListener("mousedown", onDown, true);
  return () => doc.removeEventListener("mousedown", onDown, true);
}

/**
 * Whether a right click on `element` should get the system menu instead of
 * ours: text inside it was already selected, so Copy is what is wanted.
 */
export function keepsSystemMenu(element: Element, doc: Document = document): boolean {
  const record = pressed;
  pressed = null;
  if (record) {
    if (record.range?.intersectsNode(element)) return true;
    // The press itself selected a word: undo that, our menu is opening instead.
    if (selectedRange(doc.getSelection())) doc.getSelection()?.removeAllRanges();
    return false;
  }
  // The context-menu key: there was no press, judge the selection as it is.
  return selectedRange(doc.getSelection())?.intersectsNode(element) ?? false;
}
