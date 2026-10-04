import { boot } from "./boot.js";
import { defaultImportRuntime, eventsFrom } from "./stream.js";

/**
 * App-wide commands. The desktop menu sends them as `hidane:command` Wails
 * events; every mode also binds the same shortcuts in the page, so the browser
 * build behaves the same and nothing depends on the menu being present.
 */
const COMMANDS = [
  "open-settings",
  "new-task",
  "focus-composer",
  "go:chat",
  "go:items",
  "go:schedules",
  "go:memory",
  "go:log",
  "search",
  "toggle-sidebar",
] as const;
export type Command = (typeof COMMANDS)[number];

/** The name the desktop menu emits commands under. */
export const WAILS_COMMAND_EVENT = "hidane:command";

function isCommand(value: unknown): value is Command {
  return typeof value === "string" && (COMMANDS as readonly string[]).includes(value);
}

/** Every shortcut is the platform modifier (⌘ on macOS, Ctrl elsewhere) plus one key. */
export const SHORTCUTS: readonly { command: Command; key: string }[] = [
  { command: "open-settings", key: "," },
  { command: "new-task", key: "n" },
  { command: "focus-composer", key: "l" },
  { command: "go:chat", key: "1" },
  { command: "go:items", key: "2" },
  { command: "go:schedules", key: "3" },
  { command: "go:memory", key: "4" },
  { command: "go:log", key: "5" },
  { command: "search", key: "k" },
  { command: "toggle-sidebar", key: "b" },
];

export function isMacPlatform(nav: Pick<Navigator, "platform" | "userAgent"> | undefined = typeof navigator === "undefined" ? undefined : navigator): boolean {
  if (!nav) return false;
  return /Mac|iPhone|iPad/i.test(nav.platform || nav.userAgent);
}

export interface KeyLike {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
  isComposing?: boolean;
  repeat?: boolean;
}

export function commandForKey(event: KeyLike, mac: boolean): Command | null {
  if (event.isComposing || event.repeat || event.altKey || event.shiftKey) return null;
  const modifier = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  if (!modifier) return null;
  const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
  return SHORTCUTS.find((shortcut) => shortcut.key === key)?.command ?? null;
}

export function shortcutKey(command: Command): string | null {
  return SHORTCUTS.find((shortcut) => shortcut.command === command)?.key ?? null;
}

/** "⌘K" on macOS, "Ctrl+K" elsewhere. */
export function formatShortcut(key: string, mac: boolean): string {
  const shown = key.length === 1 ? key.toUpperCase() : key;
  return mac ? `⌘${shown}` : `Ctrl+${shown}`;
}

/** Where a command came from: the page's own keydown, or the native menu. */
export type CommandSource = "key" | "menu";

/**
 * A desktop key press can arrive twice — once from the menu's key equivalent
 * and once as a page keydown — and for the toggles a double delivery cancels
 * itself out. The same command from the *other* source within `windowMs` of
 * the last run is that echo and is dropped; two presses on the same source are
 * two presses (auto-repeat is already filtered out by `commandForKey`).
 */
export function createCommandRunner(
  run: (command: Command) => void,
  options: { windowMs?: number; now?: () => number } = {},
): (command: Command, source?: CommandSource) => boolean {
  const windowMs = options.windowMs ?? 200;
  const now = options.now ?? (() => Date.now());
  const last = new Map<Command, { at: number; source: CommandSource }>();
  return (command, source = "key") => {
    const at = now();
    const previous = last.get(command);
    if (previous && previous.source !== source && at - previous.at < windowMs) return false;
    last.set(command, { at, source });
    run(command);
    return true;
  };
}

function commandOf(value: unknown): Command | null {
  if (isCommand(value)) return value;
  if (typeof value === "string") {
    try {
      return commandOf(JSON.parse(value) as unknown);
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  const command = (value as { command?: unknown }).command;
  return isCommand(command) ? command : null;
}

/**
 * Normalise what a Wails `Events.On` callback receives into a command: the
 * event's `.data` is `{ command }`, or an array whose first element is.
 */
export function commandFromWails(event: unknown): Command | null {
  const direct = commandOf(event);
  if (direct) return direct;
  if (typeof event !== "object" || event === null) return null;
  const payload = (event as { data?: unknown }).data;
  return commandOf(Array.isArray(payload) ? payload[0] : payload);
}

export interface CommandSourceOptions {
  /** Defaults to `boot().desktop`; only then is the Wails runtime asked for menu commands. */
  desktop?: boolean;
  mac?: boolean;
  target?: Pick<Window, "addEventListener" | "removeEventListener">;
  importRuntime?: () => Promise<unknown>;
}

/**
 * Deliver commands from page shortcuts and, in the desktop app, the native
 * menu to `dispatch`. Returns a function that removes both sources.
 */
export function installCommands(dispatch: (command: Command, source: CommandSource) => void, options: CommandSourceOptions = {}): () => void {
  const mac = options.mac ?? isMacPlatform();
  const target = options.target ?? window;
  let closed = false;
  let off: (() => void) | null = null;

  const onKeydown = (event: Event) => {
    const command = commandForKey(event as KeyboardEvent, mac);
    if (!command) return;
    event.preventDefault();
    dispatch(command, "key");
  };
  // Capture: a focused field that stops propagation must not swallow app shortcuts.
  target.addEventListener("keydown", onKeydown, true);

  if (options.desktop ?? boot().desktop) {
    (options.importRuntime ?? defaultImportRuntime)()
      .then((mod) => {
        if (closed) return;
        const events = eventsFrom(mod);
        if (!events) return;
        const stop = events.On(WAILS_COMMAND_EVENT, (event) => {
          const command = commandFromWails(event);
          if (command) dispatch(command, "menu");
        });
        off = typeof stop === "function" ? (stop as () => void) : null;
      })
      // Without the runtime (a browser pretending to be the app) the shortcuts still work.
      .catch(() => undefined);
  }

  return () => {
    closed = true;
    target.removeEventListener("keydown", onKeydown, true);
    off?.();
    off = null;
  };
}
