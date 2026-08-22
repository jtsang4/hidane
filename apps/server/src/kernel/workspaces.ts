import { mkdir } from "node:fs/promises";
import { join } from "node:path";
import { execa } from "execa";
import { workspacesDir } from "../config.js";

/** Every work item owns exactly one workspace directory (1..1). */
export function workspacePath(workItemId: string): string {
  return join(workspacesDir(), workItemId);
}

export interface WorkspaceResult {
  path: string;
  provider: "dir" | "worktree";
  branch?: string | undefined;
  error?: string | undefined;
}

async function isGitRepo(repo: string): Promise<boolean> {
  const res = await execa("git", ["-C", repo, "rev-parse", "--git-dir"], {
    reject: false,
    stdin: "ignore",
  });
  return res.exitCode === 0;
}

/**
 * Create the workspace. Plain directory by default; when a local git repo is
 * given, the workspace becomes a worktree of it on a dedicated branch — the
 * carrier changes, the workspace contract (one dir per work item) does not.
 */
export async function ensureWorkspace(
  workItemId: string,
  repo?: string,
): Promise<WorkspaceResult> {
  const dir = workspacePath(workItemId);
  if (repo && (await isGitRepo(repo))) {
    const branch = `hidane/${workItemId}`;
    const res = await execa(
      "git",
      ["-C", repo, "worktree", "add", dir, "-b", branch],
      { reject: false, stdin: "ignore" },
    );
    await mkdir(join(dir, ".hidane", "sessions"), { recursive: true });
    if (res.exitCode === 0) {
      return { path: dir, provider: "worktree", branch };
    }
    return {
      path: dir,
      provider: "dir",
      error: `worktree add failed: ${(res.stderr || "").slice(0, 300)}`,
    };
  }
  await mkdir(join(dir, ".hidane", "sessions"), { recursive: true });
  return { path: dir, provider: "dir" };
}
