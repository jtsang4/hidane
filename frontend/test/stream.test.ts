import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "../src/lib/api.js";
import {
  FRAME_KINDS,
  WAILS_FRAME_EVENT,
  frameFromWails,
  liveTransport,
  openLiveStream,
  type EventSourceLike,
  type FrameHandlers,
  type FrameKind,
} from "../src/lib/stream.js";

class FakeEventSource implements EventSourceLike {
  static instances: FakeEventSource[] = [];
  listeners = new Map<string, Array<(event: MessageEvent) => void>>();
  closed = false;
  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }
  addEventListener(type: string, listener: (event: MessageEvent) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), listener]);
  }
  close(): void {
    this.closed = true;
  }
  emit(type: string, data: string): void {
    for (const listener of this.listeners.get(type) ?? []) listener(new MessageEvent(type, { data }));
  }
}

const createEventSource = (url: string) => new FakeEventSource(url);

function fakeRuntime() {
  const callbacks = new Map<string, (event: unknown) => void>();
  const off = vi.fn();
  const module = {
    Events: {
      On: vi.fn((name: string, callback: (event: unknown) => void) => {
        callbacks.set(name, callback);
        return () => {
          callbacks.delete(name);
          off(name);
        };
      }),
    },
  };
  return { module, callbacks, off, fire: (event: unknown) => callbacks.get(WAILS_FRAME_EVENT)?.(event) };
}

function recorder(): { handlers: FrameHandlers; seen: Array<[FrameKind, string]> } {
  const seen: Array<[FrameKind, string]> = [];
  const handlers: FrameHandlers = {};
  for (const kind of FRAME_KINDS) handlers[kind] = (data) => seen.push([kind, data]);
  return { handlers, seen };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(() => {
  FakeEventSource.instances = [];
  localStorage.clear();
});
afterEach(() => {
  delete window.hidaneBoot;
});

describe("openLiveStream in browser mode", () => {
  it("opens the SSE url with the query token and forwards all four frames as text", () => {
    setToken("tok/en");
    const { handlers, seen } = recorder();
    const close = openLiveStream(handlers, { desktop: false, createEventSource });
    const [source] = FakeEventSource.instances;
    expect(source?.url).toBe(`/api/events/stream?token=${encodeURIComponent("tok/en")}`);
    source?.emit("hello", "{}");
    source?.emit("hidane", '{"seq":7,"kind":"user.message"}');
    source?.emit("stream", '{"id":"r1","threadId":"main","text":"hi"}');
    source?.emit("ping", "{}");
    expect(seen).toEqual([
      ["hello", "{}"],
      ["hidane", '{"seq":7,"kind":"user.message"}'],
      ["stream", '{"id":"r1","threadId":"main","text":"hi"}'],
      ["ping", "{}"],
    ]);
    close();
    expect(source?.closed).toBe(true);
    source?.emit("ping", "{}");
    expect(seen).toHaveLength(4);
  });

  it("follows boot() when desktop is not given", () => {
    const importRuntime = vi.fn(() => Promise.resolve({}));
    openLiveStream({}, { createEventSource, importRuntime })();
    expect(importRuntime).not.toHaveBeenCalled();
    expect(FakeEventSource.instances).toHaveLength(1);
  });
});

describe("openLiveStream in desktop mode", () => {
  it("subscribes to hidane:frame and re-serialises object payloads", async () => {
    const runtime = fakeRuntime();
    const { handlers, seen } = recorder();
    const close = openLiveStream(handlers, {
      desktop: true,
      createEventSource,
      importRuntime: () => Promise.resolve(runtime.module),
    });
    await flush();
    expect(runtime.module.Events.On).toHaveBeenCalledWith(WAILS_FRAME_EVENT, expect.any(Function));
    expect(FakeEventSource.instances).toHaveLength(0);

    // WailsEvent with the frame as `.data`…
    runtime.fire({ name: WAILS_FRAME_EVENT, data: { event: "hidane", data: { seq: 3, kind: "agent.reply" } } });
    // …or as the first element of a variadic `.data` array.
    runtime.fire({ name: WAILS_FRAME_EVENT, data: [{ event: "stream", data: { id: "r", threadId: "main", text: "x" } }] });
    runtime.fire({ name: WAILS_FRAME_EVENT, data: { event: "ping", data: {} } });
    runtime.fire({ name: WAILS_FRAME_EVENT, data: { event: "bogus", data: {} } });
    runtime.fire({ name: WAILS_FRAME_EVENT, data: null });

    expect(seen).toEqual([
      ["hidane", '{"seq":3,"kind":"agent.reply"}'],
      ["stream", '{"id":"r","threadId":"main","text":"x"}'],
      ["ping", "{}"],
    ]);
    expect(JSON.parse(seen[0]![1])).toEqual({ seq: 3, kind: "agent.reply" });

    close();
    expect(runtime.off).toHaveBeenCalledWith(WAILS_FRAME_EVENT);
  });

  it("uses boot().desktop by default", async () => {
    window.hidaneBoot = { desktop: true, auth: false, version: "t" };
    const runtime = fakeRuntime();
    const close = openLiveStream({}, { createEventSource, importRuntime: () => Promise.resolve(runtime.module) });
    await flush();
    expect(runtime.module.Events.On).toHaveBeenCalledTimes(1);
    close();
  });

  it("does not subscribe when closed before the runtime loads", async () => {
    const runtime = fakeRuntime();
    const close = openLiveStream({}, {
      desktop: true,
      createEventSource,
      importRuntime: () => Promise.resolve(runtime.module),
    });
    close();
    await flush();
    expect(runtime.module.Events.On).not.toHaveBeenCalled();
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it("falls back to SSE when the runtime cannot be imported", async () => {
    const onFallback = vi.fn();
    const { handlers, seen } = recorder();
    const close = openLiveStream(handlers, {
      desktop: true,
      createEventSource,
      importRuntime: () => Promise.reject(new Error("404 /wails/runtime.js")),
      onFallback,
    });
    await flush();
    expect(onFallback).toHaveBeenCalledTimes(1);
    const [source] = FakeEventSource.instances;
    expect(source?.url).toBe("/api/events/stream");
    source?.emit("ping", "{}");
    expect(seen).toEqual([["ping", "{}"]]);
    close();
    expect(source?.closed).toBe(true);
  });

  it("falls back to SSE when the module has no Events.On", async () => {
    openLiveStream({}, { desktop: true, createEventSource, importRuntime: () => Promise.resolve({ Events: {} }) });
    await flush();
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("accepts a runtime exported as default", async () => {
    const runtime = fakeRuntime();
    const { handlers, seen } = recorder();
    openLiveStream(handlers, {
      desktop: true,
      createEventSource,
      importRuntime: () => Promise.resolve({ default: runtime.module }),
    });
    await flush();
    runtime.fire({ data: { event: "hello", data: { seq: 1 } } });
    expect(seen).toEqual([["hello", '{"seq":1}']]);
  });
});

describe("transport and greeting", () => {
  it("reports sse in browser mode", () => {
    openLiveStream({}, { desktop: false, createEventSource })();
    expect(liveTransport()).toBe("sse");
  });

  it("reports wails and asks the shell for its greeting once subscribed", async () => {
    const { module } = fakeRuntime();
    const greet = vi.fn();
    const close = openLiveStream({}, { desktop: true, importRuntime: () => Promise.resolve(module), greet, createEventSource });
    await vi.waitFor(() => expect(greet).toHaveBeenCalledTimes(1));
    expect(liveTransport()).toBe("wails");
    close();
  });

  it("does not greet when the runtime cannot be loaded", async () => {
    const greet = vi.fn();
    const onFallback = vi.fn();
    openLiveStream({}, { desktop: true, importRuntime: () => Promise.reject(new Error("no runtime")), greet, onFallback, createEventSource });
    await vi.waitFor(() => expect(onFallback).toHaveBeenCalled());
    expect(greet).not.toHaveBeenCalled();
    expect(liveTransport()).toBe("sse");
  });
});

describe("frameFromWails", () => {
  it("accepts a bare frame, a wrapped frame, an array payload and JSON text", () => {
    const frame = { event: "ping", data: { at: 1 } };
    const expected = { kind: "ping", data: '{"at":1}' };
    expect(frameFromWails(frame)).toEqual(expected);
    expect(frameFromWails({ name: "hidane:frame", data: frame })).toEqual(expected);
    expect(frameFromWails({ name: "hidane:frame", data: [frame] })).toEqual(expected);
    expect(frameFromWails({ name: "hidane:frame", data: JSON.stringify(frame) })).toEqual(expected);
  });

  it("passes string payloads through and fills a missing payload", () => {
    expect(frameFromWails({ data: { event: "hidane", data: '{"seq":1}' } })).toEqual({ kind: "hidane", data: '{"seq":1}' });
    expect(frameFromWails({ data: { event: "hello" } })).toEqual({ kind: "hello", data: "{}" });
  });

  it("rejects anything that is not one of the four frames", () => {
    expect(frameFromWails(undefined)).toBeNull();
    expect(frameFromWails({ data: [] })).toBeNull();
    expect(frameFromWails({ data: { event: "other", data: {} } })).toBeNull();
    expect(frameFromWails({ data: "not json" })).toBeNull();
  });
});
