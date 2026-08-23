export interface Toast {
  id: number;
  message: string;
  tone: "danger" | "default";
}

/**
 * Minimal external store for transient messages.
 *
 * Failures used to be invisible: a rejected write or a dead server left the
 * page looking merely idle, which is indistinguishable from the runtime's
 * normal asynchronous silence. Anything that fails must say so.
 */
let toasts: Toast[] = [];
let nextId = 1;
const listeners = new Set<() => void>();

function emit(): void {
  for (const fn of listeners) fn();
}

export function getToasts(): Toast[] {
  return toasts;
}

export function subscribeToasts(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

export function pushToast(message: string, tone: Toast["tone"] = "danger"): number {
  const id = nextId++;
  // Repeating the same message (a poll failing every cycle) should not stack.
  toasts = [...toasts.filter((t) => t.message !== message), { id, message, tone }];
  emit();
  return id;
}

export function dismissToast(id: number): void {
  toasts = toasts.filter((t) => t.id !== id);
  emit();
}

export function clearToasts(): void {
  toasts = [];
  emit();
}
