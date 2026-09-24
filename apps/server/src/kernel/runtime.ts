import { sql } from "./db.js";
import { appendEvent, commitCursor, type HidaneEvent } from "./events.js";
import { onEventAppended } from "./notify.js";
import {
  mailboxCursor,
  mailboxesWithPending,
  pendingMessages,
} from "./mailbox.js";

/**
 * One turn of one agent loop: every message that piled up in its mailbox since
 * the last turn, handled together. A handler decides and returns; it never
 * waits on long work — that is dispatched, and its outcome comes back later as
 * another message.
 */
export type TurnHandler = (address: string, messages: HidaneEvent[]) => Promise<void>;

interface IdleTask {
  name: string;
  run: () => Promise<unknown>;
  minIntervalMs: number;
  /** Past this the task runs even when the system is busy, so it cannot starve. */
  maxIntervalMs: number;
  lastRun: number;
  running: boolean;
}

interface TimerTask {
  name: string;
  run: () => Promise<unknown>;
  everyMs: number;
  lastRun: number;
  running: boolean;
}

export interface RuntimeOptions {
  /** Concurrent turns across all mailboxes; one mailbox never runs two. */
  maxConcurrentTurns?: number;
  /** Fallback poll when a notification is missed. */
  pollMs?: number;
}

/** Arbitrary constant; one runtime per database. */
const RUNTIME_LOCK_KEY = 271_801;

/**
 * The event loop. Mailboxes are the task queues, turns are run to completion,
 * and priority is: interrupt-lane mailboxes, then normal, then idle tasks only
 * once nothing is pending or running.
 */
export class Runtime {
  private readonly handlers: { prefix: string; handler: TurnHandler }[] = [];
  private readonly active = new Map<string, Promise<void>>();
  private readonly idleTasks: IdleTask[] = [];
  private readonly timers: TimerTask[] = [];
  private readonly maxConcurrentTurns: number;
  private readonly pollMs: number;
  private ticking: Promise<void> | undefined;
  private again = false;
  private stopped = true;
  private unlisten: (() => void) | undefined;
  private pollTimer: NodeJS.Timeout | undefined;
  private readonly unhandled = new Set<string>();

  constructor(opts: RuntimeOptions = {}) {
    this.maxConcurrentTurns = opts.maxConcurrentTurns ?? 4;
    this.pollMs = opts.pollMs ?? 5000;
  }

  /** Handle every mailbox whose address equals `prefix` or starts with it. */
  register(prefix: string, handler: TurnHandler): void {
    this.handlers.push({ prefix, handler });
  }

  registerIdle(
    name: string,
    run: () => Promise<unknown>,
    opts: { minIntervalMs: number; maxIntervalMs: number },
  ): void {
    this.idleTasks.push({ name, run, ...opts, lastRun: Date.now(), running: false });
  }

  /** Housekeeping that must run on a clock regardless of load (deadlines, pumps). */
  registerTimer(name: string, run: () => Promise<unknown>, everyMs: number): void {
    this.timers.push({ name, run, everyMs, lastRun: 0, running: false });
  }

  start(): void {
    this.stopped = false;
    this.unlisten = onEventAppended(() => this.kick());
    this.pollTimer = setInterval(() => this.kick(), this.pollMs);
    this.kick();
  }

  async stop(): Promise<void> {
    this.stopped = true;
    this.unlisten?.();
    clearInterval(this.pollTimer);
    await Promise.allSettled([...this.active.values()]);
  }

  status(): { activeTurns: string[] } {
    return { activeTurns: [...this.active.keys()] };
  }

  /** Request a scheduling pass; coalesces bursts of notifications into one. */
  kick(): void {
    if (this.stopped) return;
    if (this.ticking) {
      this.again = true;
      return;
    }
    this.ticking = (async () => {
      do {
        this.again = false;
        try {
          await this.tick();
        } catch (err) {
          console.error("runtime tick failed:", err);
        }
      } while (this.again && !this.stopped);
    })().finally(() => {
      this.ticking = undefined;
    });
  }

  /**
   * Run until every mailbox is empty and no turn is in flight. For tests and
   * one-shot processes; the daemon uses start().
   */
  async drain(maxRounds = 50): Promise<void> {
    for (let round = 0; round < maxRounds; round++) {
      await this.tick();
      if (this.active.size === 0) {
        const pending = await mailboxesWithPending();
        if (pending.every((p) => !this.handlerFor(p.address))) return;
        continue;
      }
      await Promise.allSettled([...this.active.values()]);
    }
    throw new Error("runtime did not drain");
  }

  /** One scheduling pass and the turns it started. */
  async step(): Promise<void> {
    await this.tick();
    await Promise.allSettled([...this.active.values()]);
  }

  private handlerFor(address: string): TurnHandler | undefined {
    return this.handlers.find((h) => address === h.prefix || address.startsWith(h.prefix))
      ?.handler;
  }

  private async tick(): Promise<void> {
    const now = Date.now();
    for (const timer of this.timers) {
      if (timer.running || now - timer.lastRun < timer.everyMs) continue;
      timer.running = true;
      timer.lastRun = now;
      void timer
        .run()
        .catch((err) => console.error(`timer ${timer.name} failed:`, err))
        .finally(() => {
          timer.running = false;
        });
    }

    const pending = await mailboxesWithPending();
    for (const mailbox of pending) {
      if (this.active.size >= this.maxConcurrentTurns) break;
      if (this.active.has(mailbox.address)) continue;
      const handler = this.handlerFor(mailbox.address);
      if (!handler) {
        if (!this.unhandled.has(mailbox.address)) {
          this.unhandled.add(mailbox.address);
          console.error(`no turn handler for mailbox ${mailbox.address}`);
        }
        continue;
      }
      const turn = this.runTurn(mailbox.address, handler).finally(() => {
        this.active.delete(mailbox.address);
        this.kick();
      });
      this.active.set(mailbox.address, turn);
    }

    const idle = this.active.size === 0 && pending.every((p) => !this.handlerFor(p.address));
    for (const task of this.idleTasks) {
      if (task.running) continue;
      const since = now - task.lastRun;
      const due = (idle && since >= task.minIntervalMs) || since >= task.maxIntervalMs;
      if (!due) continue;
      task.running = true;
      task.lastRun = now;
      void task
        .run()
        .catch((err) => console.error(`idle task ${task.name} failed:`, err))
        .finally(() => {
          task.running = false;
        });
    }
  }

  private async runTurn(address: string, handler: TurnHandler): Promise<void> {
    const messages = await pendingMessages(address);
    const last = messages[messages.length - 1];
    if (!last) return;
    try {
      await handler(address, messages);
    } catch (err) {
      // At-least-once delivery, not at-least-forever: a turn that throws is
      // recorded and its messages are consumed, or one poison message would
      // wedge the mailbox and re-spend on every retry.
      await appendEvent({
        source: "kernel:runtime",
        kind: "agent.error",
        threadId: "main",
        payload: {
          error: `turn failed for ${address}: ${err instanceof Error ? err.message : String(err)}`,
          mailbox: address,
          of: last.id,
        },
      }).catch(() => {});
    }
    await commitCursor(mailboxCursor(address), last.seq);
  }
}

/**
 * Hold the single-runtime lock for the life of the process. Two daemons against
 * one database would otherwise run the same turn twice.
 */
export async function acquireRuntimeLock(): Promise<(() => Promise<void>) | null> {
  const conn = await sql().reserve();
  const rows = await conn`SELECT pg_try_advisory_lock(${RUNTIME_LOCK_KEY}) AS ok`;
  if (!(rows[0] as { ok: boolean }).ok) {
    conn.release();
    return null;
  }
  return async () => {
    await conn`SELECT pg_advisory_unlock(${RUNTIME_LOCK_KEY})`.catch(() => {});
    conn.release();
  };
}
