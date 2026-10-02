import { eventStreamUrl } from "./api.js";
import { boot } from "./boot.js";

/**
 * The four frames of the live channel, identical in both transports:
 * `hello` on connect, `hidane` for a durable log event, `stream` for the text
 * of a reply still being written (ephemeral), `ping` every 15s for liveness.
 */
export type FrameKind = "hello" | "hidane" | "stream" | "ping";
export const FRAME_KINDS: readonly FrameKind[] = ["hello", "hidane", "stream", "ping"];

/** Handlers receive the frame payload as JSON text, whichever transport delivered it. */
export type FrameHandlers = Partial<Record<FrameKind, (data: string) => void>>;

export interface EventSourceLike {
  addEventListener(type: string, listener: (event: MessageEvent) => void): void;
  close(): void;
}
export type EventSourceFactory = (url: string) => EventSourceLike;

/** The name the desktop host emits every frame under. */
export const WAILS_FRAME_EVENT = "hidane:frame";
const WAILS_RUNTIME_URL = "/wails/runtime.js";

export interface LiveStreamOptions {
  /** Defaults to `boot().desktop`. */
  desktop?: boolean;
  /** Defaults to `eventStreamUrl()` (carries the query token in auth mode). */
  url?: string;
  createEventSource?: EventSourceFactory;
  /** Loads the Wails runtime module; defaults to importing `/wails/runtime.js`. */
  importRuntime?: () => Promise<unknown>;
  /** Called when the desktop channel could not be opened and SSE is used instead. */
  onFallback?: (error: unknown) => void;
  /** Asks the desktop shell for the greeting once subscribed; defaults to POST /api/live/hello. */
  greet?: () => Promise<void> | void;
}

const isFrameKind = (value: unknown): value is FrameKind =>
  typeof value === "string" && (FRAME_KINDS as readonly string[]).includes(value);

function asFrame(value: unknown): { kind: FrameKind; data: string } | null {
  if (typeof value === "string") {
    try {
      return asFrame(JSON.parse(value) as unknown);
    } catch {
      return null;
    }
  }
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  const { event, data } = value as { event?: unknown; data?: unknown };
  if (!isFrameKind(event)) return null;
  return { kind: event, data: typeof data === "string" ? data : JSON.stringify(data ?? {}) };
}

/**
 * Normalise what a Wails `Events.On` callback receives into a frame.
 *
 * The callback gets a `WailsEvent` whose `.data` is the frame, or — when the
 * host emits variadic data — an array whose first element is the frame. The
 * frame's own `data` is an object, re-serialised so handlers see the same
 * text an SSE `MessageEvent` would carry.
 */
export function frameFromWails(event: unknown): { kind: FrameKind; data: string } | null {
  const direct = asFrame(event);
  if (direct) return direct;
  if (typeof event !== "object" || event === null) return null;
  const payload = (event as { data?: unknown }).data;
  return asFrame(Array.isArray(payload) ? payload[0] : payload);
}

export interface WailsEvents {
  On(name: string, callback: (event: unknown) => void): unknown;
  Off?: (name: string) => void;
}

export function eventsFrom(mod: unknown): WailsEvents | null {
  const pick = (value: unknown): WailsEvents | null => {
    if (typeof value !== "object" || value === null) return null;
    const events = (value as { Events?: unknown }).Events;
    return typeof events === "object" && events !== null && typeof (events as WailsEvents).On === "function"
      ? (events as WailsEvents)
      : null;
  };
  return pick(mod) ?? pick((mod as { default?: unknown } | null)?.default);
}

// A variable specifier keeps the bundler from trying to resolve a module that
// only exists when the Wails asset server is the host.
export const defaultImportRuntime = (): Promise<unknown> => import(/* @vite-ignore */ WAILS_RUNTIME_URL);

/**
 * Open the live channel and dispatch its frames to `handlers`.
 * Returns a close function; no handler runs after it is called.
 *
 * Browser mode is a plain `EventSource`. Desktop mode subscribes to Wails
 * events and falls back to `EventSource` if the runtime cannot be loaded.
 * Liveness and reconnects are the caller's concern (see `live.ts`): both
 * transports deliver the same `ping` every 15s.
 */
export type LiveTransport = "none" | "sse" | "wails";

let activeTransport: LiveTransport = "none";

/** Which transport the most recently opened stream ended up on. */
export function liveTransport(): LiveTransport {
  return activeTransport;
}

/**
 * Desktop frames are pushed, not requested: the shell cannot know when a page
 * subscribed, so the page asks for its greeting (and any reply already in
 * flight) once it is listening.
 */
async function defaultGreet(): Promise<void> {
  try {
    await fetch("/api/live/hello", { method: "POST" });
  } catch {
    // The next ping still proves liveness; a missed greeting only delays it.
  }
}

export function openLiveStream(handlers: FrameHandlers, options: LiveStreamOptions = {}): () => void {
  let closed = false;
  let stop: (() => void) | null = null;
  const dispatch = (kind: FrameKind, data: string) => {
    if (!closed) handlers[kind]?.(data);
  };

  const viaEventSource = () => {
    const create = options.createEventSource ?? ((url: string) => new EventSource(url));
    const source = create(options.url ?? eventStreamUrl());
    for (const kind of FRAME_KINDS) {
      source.addEventListener(kind, (event) => {
        dispatch(kind, typeof event.data === "string" ? event.data : "");
      });
    }
    stop = () => source.close();
    activeTransport = "sse";
  };

  if (!(options.desktop ?? boot().desktop)) {
    viaEventSource();
  } else {
    (options.importRuntime ?? defaultImportRuntime)()
      .then((mod) => {
        if (closed) return;
        const events = eventsFrom(mod);
        if (!events) throw new Error("Wails runtime exposes no Events.On");
        const off = events.On(WAILS_FRAME_EVENT, (event) => {
          const frame = frameFromWails(event);
          if (frame) dispatch(frame.kind, frame.data);
        });
        stop = typeof off === "function" ? (off as () => void) : () => events.Off?.(WAILS_FRAME_EVENT);
        activeTransport = "wails";
        void (options.greet ?? defaultGreet)();
      })
      .catch((error: unknown) => {
        if (closed) return;
        options.onFallback?.(error);
        viaEventSource();
      });
  }

  return () => {
    closed = true;
    stop?.();
    stop = null;
  };
}
