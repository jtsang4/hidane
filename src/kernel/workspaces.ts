import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { workspacesDir } from "../config.js";

/** Every work item owns exactly one workspace directory (1..1). */
export function workspacePath(workItemId: string): string {
  return join(workspacesDir(), workItemId);
}

/** Create the workspace directory (and its session trace dir) if missing. */
export async function ensureWorkspace(workItemId: string): Promise<string> {
  const dir = workspacePath(workItemId);
  await mkdir(join(dir, ".hidane", "sessions"), { recursive: true });
  return dir;
}
