/**
 * In-app confirmation. The desktop webview (WKWebView under Wails) implements
 * no JS `confirm()` panel — it returns false at once, so every destructive
 * action guarded by it silently did nothing. One host component renders the
 * request at the head of this queue.
 */
export interface ConfirmOptions {
  title: string;
  body?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  /** Styles the confirm button as dangerous. */
  destructive?: boolean;
}

export interface ConfirmRequest extends ConfirmOptions {
  id: number;
  resolve: (confirmed: boolean) => void;
}

export const confirmState = $state<{ current: ConfirmRequest | null }>({ current: null });

const queue: ConfirmRequest[] = [];
let nextId = 1;

export function confirmAction(options: ConfirmOptions): Promise<boolean> {
  return new Promise((resolve) => {
    const request: ConfirmRequest = { ...options, id: nextId++, resolve };
    if (confirmState.current) queue.push(request);
    else confirmState.current = request;
  });
}

/** Answer the visible request and show the next queued one, if any. */
export function settleConfirm(confirmed: boolean): void {
  const current = confirmState.current;
  if (!current) return;
  confirmState.current = queue.shift() ?? null;
  current.resolve(confirmed);
}

export function confirmOpen(): boolean {
  return confirmState.current !== null;
}
