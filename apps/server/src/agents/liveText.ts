import { EventEmitter } from "node:events";
import { genId } from "../kernel/ids.js";

/**
 * A frame of a reply that is still being written.
 *
 * These are deliberately NOT events. The log is append-only and records facts,
 * not thinking — half a sentence is neither. Frames live in this process only,
 * are superseded by the durable `agent.reply` the moment it lands, and are lost
 * on restart. That is the correct failure mode: the thing that replaces them is
 * the thing that survives.
 */
export interface LiveTextFrame {
  id: string;
  threadId: string;
  /** Text appended since the previous frame. */
  delta?: string;
  /** The whole reply so far — sent once, to a client that joined mid-run. */
  text?: string;
  /** The model has stopped. The durable event follows within a poll cycle. */
  done?: boolean;
}

export interface LiveText {
  readonly id: string;
  push(delta: string): void;
  /** Idempotent, so a caller may close defensively on an error path. */
  end(): void;
}

interface OpenStream {
  id: string;
  threadId: string;
  text: string;
}

const bus = new EventEmitter();
// One listener per connected SSE client. The default cap of 10 would warn at a
// perfectly ordinary number of open tabs.
bus.setMaxListeners(0);

const open = new Map<string, OpenStream>();

/** Past this the replay snapshot stops growing; deltas still flow. A runaway
 *  model must not be able to pin unbounded memory in the daemon. */
const SNAPSHOT_CAP = 32_000;

export function beginLiveText(threadId: string): LiveText {
  const id = genId("ls", 8);
  open.set(id, { id, threadId, text: "" });
  return {
    id,
    push(delta) {
      const stream = open.get(id);
      if (!stream || !delta) return;
      if (stream.text.length < SNAPSHOT_CAP) stream.text += delta;
      bus.emit("frame", { id, threadId: stream.threadId, delta } satisfies LiveTextFrame);
    },
    end() {
      const stream = open.get(id);
      if (!stream) return;
      open.delete(id);
      bus.emit("frame", {
        id,
        threadId: stream.threadId,
        done: true,
      } satisfies LiveTextFrame);
    },
  };
}

/** Replies already in flight, for a client that connects mid-run. */
export function liveTextSnapshot(): LiveTextFrame[] {
  return [...open.values()]
    .filter((stream) => stream.text.length > 0)
    .map((stream) => ({ id: stream.id, threadId: stream.threadId, text: stream.text }));
}

export function subscribeLiveText(listener: (frame: LiveTextFrame) => void): () => void {
  bus.on("frame", listener);
  return () => {
    bus.off("frame", listener);
  };
}
