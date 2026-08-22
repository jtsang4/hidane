import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { listEvents, type HidaneEvent } from "../kernel/events.js";
import { listWorkItems } from "../kernel/workItems.js";
import { worklogsDir } from "../config.js";

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return d.toTimeString().slice(0, 8);
}

function line(e: HidaneEvent): string {
  const text = e.payload["text"] ?? e.payload["note"] ?? e.payload["summary"];
  const detail =
    typeof text === "string" && text.length > 0
      ? text.length > 500
        ? `${text.slice(0, 500)}…`
        : text
      : JSON.stringify(e.payload).slice(0, 200);
  return `- \`${fmtTime(e.ts)}\` **${e.kind}** (${e.source}) ${detail.replaceAll("\n", " ")}`;
}

/**
 * Daily worklog is a projection of the event log — derived, rebuildable,
 * never a second source of truth.
 */
export async function renderDay(day: string): Promise<string> {
  const events = await listEvents({ day });
  const items = await listWorkItems();
  const byThread = new Map<string, HidaneEvent[]>();
  for (const e of events) {
    const key = e.threadId ?? "(no-thread)";
    const arr = byThread.get(key) ?? [];
    arr.push(e);
    byThread.set(key, arr);
  }

  const parts: string[] = [`# Worklog ${day}`, ""];
  parts.push(`Total events: ${events.length}`, "");

  const main = byThread.get("main");
  if (main) {
    parts.push("## Main thread", "", ...main.map(line), "");
    byThread.delete("main");
  }
  for (const item of items) {
    const threadEvents = byThread.get(item.threadId);
    if (!threadEvents) continue;
    parts.push(
      `## ${item.id} — ${item.title} (${item.status})`,
      "",
      ...threadEvents.map(line),
      "",
    );
    byThread.delete(item.threadId);
  }
  for (const [threadId, rest] of byThread) {
    parts.push(`## thread ${threadId}`, "", ...rest.map(line), "");
  }
  return parts.join("\n");
}

/** Write the projection to worklogs/YYYY/MM/YYYY-MM-DD.md and return the path. */
export async function writeDay(day: string): Promise<string> {
  const md = await renderDay(day);
  const [year, month] = [day.slice(0, 4), day.slice(5, 7)];
  const dir = join(worklogsDir(), year, month);
  await mkdir(dir, { recursive: true });
  const path = join(dir, `${day}.md`);
  await writeFile(path, md, "utf8");
  return path;
}

export function today(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
