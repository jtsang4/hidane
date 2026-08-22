import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { config } from "../config.js";
import { genId } from "./ids.js";
import { appendEvent } from "./events.js";

/**
 * Memory lives in layered markdown FILES — the canonical current state,
 * human-readable and agent-native. The event log records every change
 * (memory.candidate / memory.promoted), so history and audit stay in the log;
 * the files are the working set. No database table: that would be a second
 * source of truth.
 *
 * Layers mirror context scopes:
 *   global    → <HIDANE_HOME>/memory/MEMORY.md
 *   work item → <workspace>/MEMORY.md   (workers see it in their cwd)
 */

export type MemoryKind = "fact" | "preference" | "decision" | "lesson";

export interface MemoryEntry {
  kind: MemoryKind;
  content: string;
  date: string;
  id: string;
}

export const MEMORY_KINDS: MemoryKind[] = ["fact", "preference", "decision", "lesson"];

const HEADER = (scope: string) =>
  `# hidane memory (${scope})\n\nDistilled long-term memory. Edit freely — this file is the source of truth; history lives in the event log.\n`;

export function globalMemoryPath(): string {
  return join(config.home, "memory", "MEMORY.md");
}

export function workItemMemoryPath(workspace: string): string {
  return join(workspace, "MEMORY.md");
}

export async function readMemoryFile(path: string): Promise<string> {
  try {
    return await readFile(path, "utf8");
  } catch {
    return "";
  }
}

function todayStr(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Append one promoted memory under its kind section, creating file/section as needed. */
export async function appendMemory(
  path: string,
  scope: string,
  entry: { kind: MemoryKind; content: string },
): Promise<MemoryEntry> {
  const id = genId("mem", 6);
  const date = todayStr();
  let text = await readMemoryFile(path);
  if (text.trim() === "") text = HEADER(scope);
  const section = `## ${entry.kind}`;
  const line = `- (${date}) ${entry.content} <!-- ${id} -->`;
  if (text.includes(`${section}\n`) || text.endsWith(section)) {
    // Insert at the end of the existing section (before the next heading).
    const start = text.indexOf(section);
    const rest = text.slice(start + section.length);
    const nextHeading = rest.search(/\n## /);
    const insertAt =
      start + section.length + (nextHeading === -1 ? rest.length : nextHeading);
    text = `${text.slice(0, insertAt).replace(/\n*$/, "\n")}${line}\n${text.slice(insertAt).replace(/^\n*/, "\n")}`;
  } else {
    text = `${text.replace(/\n*$/, "\n")}\n${section}\n\n${line}\n`;
  }
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, text, "utf8");
  return { kind: entry.kind, content: entry.content, date, id };
}

/** Parse memory bullets back out of a file (for listing and dedup context). */
export function parseMemories(text: string): MemoryEntry[] {
  const entries: MemoryEntry[] = [];
  let kind: MemoryKind = "fact";
  for (const line of text.split("\n")) {
    const heading = line.match(/^## (\w+)/);
    if (heading && (MEMORY_KINDS as string[]).includes(heading[1]!)) {
      kind = heading[1] as MemoryKind;
      continue;
    }
    const bullet = line.match(/^- \((\d{4}-\d{2}-\d{2})\) (.*?)(?: <!-- (\S+) -->)?$/);
    if (bullet) {
      entries.push({
        kind,
        date: bullet[1]!,
        content: bullet[2]!.trim(),
        id: bullet[3] ?? "",
      });
    }
  }
  return entries;
}

/**
 * Promote a memory: write it into the right layer file and record the fact.
 * Files are write-through targets, exactly like every other state change.
 */
export async function promoteToFile(
  entry: { kind: MemoryKind; content: string },
  scope: "global" | "work_item",
  opts: { workspace?: string | undefined; workItemId?: string | undefined },
  source: string,
): Promise<MemoryEntry> {
  const path =
    scope === "work_item" && opts.workspace
      ? workItemMemoryPath(opts.workspace)
      : globalMemoryPath();
  const saved = await appendMemory(path, scope === "work_item" ? `work item ${opts.workItemId ?? ""}` : "global", entry);
  await appendEvent({
    source,
    kind: "memory.promoted",
    workItemId: scope === "work_item" ? opts.workItemId : undefined,
    payload: { memoryId: saved.id, kind: entry.kind, scope, content: entry.content, path },
  });
  return saved;
}
