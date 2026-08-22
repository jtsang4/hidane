import { copyFile, mkdir, readdir, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { sessionsDir, worklogsDir, workspacesDir } from "../config.js";
import { renderDay } from "./worklog.js";

/**
 * Daily archive: one directory per day holding everything an agent (or human)
 * needs to revisit that day — the worklog projection plus copies of every
 * agent session trace touched that day. Files are the consumption surface;
 * the database keeps only the event log spine and queryable state.
 *
 *   worklogs/YYYY/MM/DD/
 *     worklog.md
 *     sessions/<origin>-<file>.jsonl
 */

export function dayDir(day: string): string {
  const [y, m, d] = [day.slice(0, 4), day.slice(5, 7), day.slice(8, 10)];
  return join(worklogsDir(), y, m, d);
}

interface SessionSource {
  origin: string;
  dir: string;
}

async function sessionSources(): Promise<SessionSource[]> {
  const sources: SessionSource[] = [{ origin: "roles", dir: sessionsDir() }];
  try {
    for (const ws of await readdir(workspacesDir())) {
      sources.push({
        origin: ws,
        dir: join(workspacesDir(), ws, ".hidane", "sessions"),
      });
      sources.push({
        origin: `${ws}-manager`,
        dir: join(workspacesDir(), ws, ".hidane", "sessions", "manager"),
      });
    }
  } catch {
    // no workspaces yet
  }
  return sources;
}

export interface ArchiveResult {
  dir: string;
  sessions: number;
}

/** Idempotent: rewrites the worklog and re-copies session files for the day. */
export async function archiveDay(day: string): Promise<ArchiveResult> {
  const dir = dayDir(day);
  const sessionsOut = join(dir, "sessions");
  await mkdir(sessionsOut, { recursive: true });

  await writeFile(join(dir, "worklog.md"), await renderDay(day), "utf8");

  const dayStart = new Date(`${day}T00:00:00`).getTime();
  const dayEnd = dayStart + 24 * 3600 * 1000;
  let copied = 0;
  for (const source of await sessionSources()) {
    let files: string[];
    try {
      files = await readdir(source.dir);
    } catch {
      continue;
    }
    for (const file of files) {
      if (!file.endsWith(".jsonl")) continue;
      const full = join(source.dir, file);
      const info = await stat(full).catch(() => undefined);
      if (!info?.isFile()) continue;
      const mtime = info.mtime.getTime();
      if (mtime < dayStart || mtime >= dayEnd) continue;
      await copyFile(full, join(sessionsOut, `${source.origin}-${file}`));
      copied++;
    }
  }
  return { dir, sessions: copied };
}
