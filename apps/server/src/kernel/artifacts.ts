import { readdir, readFile, stat } from "node:fs/promises";
import { join, relative, resolve, sep } from "node:path";

/**
 * Read access to a work item's workspace.
 *
 * Worker output only ever existed on the server: to read a file the agent had
 * produced, the user had to ask it to paste the contents into chat — which is
 * how an 8000-character reply came to be sent in the first place. Artifacts
 * being directly readable removes that whole detour.
 */

/** Noise that is never a work product. */
const SKIP_DIRS = new Set([".git", "node_modules", ".hidane", "__pycache__", ".venv"]);
const MAX_ENTRIES = 500;
/** Serve text inline only up to this; beyond it the file is a download. */
export const MAX_INLINE_BYTES = 512 * 1024;

export interface ArtifactEntry {
  /** Workspace-relative path, always with "/" separators. */
  path: string;
  size: number;
  modifiedAt: string;
}

/**
 * Resolve a workspace-relative request path to an absolute one, or null when
 * it would escape the workspace.
 *
 * This is the security boundary of the whole feature: the path comes straight
 * from a URL, so `../../etc/passwd` and absolute paths must both be refused.
 * Comparing resolved paths (rather than inspecting the input for "..") is what
 * makes symlinked and encoded variants fail too.
 */
export function resolveInside(workspace: string, requested: string): string | null {
  const root = resolve(workspace);
  const target = resolve(root, requested);
  if (target === root) return target;
  return target.startsWith(root + sep) ? target : null;
}

/** Flat listing of the workspace's files, newest first. */
export async function listArtifacts(workspace: string): Promise<ArtifactEntry[]> {
  const root = resolve(workspace);
  const entries: ArtifactEntry[] = [];

  const walk = async (dir: string): Promise<void> => {
    if (entries.length >= MAX_ENTRIES) return;
    let dirents;
    try {
      dirents = await readdir(dir, { withFileTypes: true });
    } catch {
      return; // unreadable directory is not an error worth failing the list for
    }
    for (const dirent of dirents) {
      if (entries.length >= MAX_ENTRIES) return;
      if (dirent.name.startsWith(".") && dirent.isDirectory()) continue;
      if (SKIP_DIRS.has(dirent.name)) continue;
      const full = join(dir, dirent.name);
      if (dirent.isDirectory()) {
        await walk(full);
        continue;
      }
      if (!dirent.isFile()) continue; // skip sockets, symlinks to nowhere, etc.
      try {
        const info = await stat(full);
        entries.push({
          path: relative(root, full).split(sep).join("/"),
          size: info.size,
          modifiedAt: info.mtime.toISOString(),
        });
      } catch {
        // vanished between readdir and stat — nothing to report
      }
    }
  };

  await walk(root);
  return entries.sort((a, b) => b.modifiedAt.localeCompare(a.modifiedAt));
}

const TEXT_EXTENSIONS = new Set([
  "md", "txt", "json", "yaml", "yml", "toml", "csv", "log", "html", "xml",
  "js", "ts", "tsx", "jsx", "py", "sh", "rs", "go", "java", "c", "h", "cpp",
  "sql", "css", "env", "ini", "conf", "gitignore",
]);

export function looksTextual(path: string): boolean {
  const name = path.split("/").pop() ?? "";
  const ext = name.includes(".") ? name.split(".").pop()!.toLowerCase() : "";
  // Extensionless files in a workspace are usually scripts or notes.
  return ext === "" || TEXT_EXTENSIONS.has(ext);
}

export interface ArtifactContent {
  path: string;
  size: number;
  text?: string;
  /** Set when the file is binary or too large to inline. */
  reason?: "binary" | "too-large";
}

export async function readArtifact(
  workspace: string,
  requested: string,
): Promise<ArtifactContent | null> {
  const target = resolveInside(workspace, requested);
  if (!target) return null;
  let info;
  try {
    info = await stat(target);
  } catch {
    return null;
  }
  if (!info.isFile()) return null;
  const path = requested.split(sep).join("/");
  if (!looksTextual(path)) return { path, size: info.size, reason: "binary" };
  if (info.size > MAX_INLINE_BYTES) return { path, size: info.size, reason: "too-large" };
  return { path, size: info.size, text: await readFile(target, "utf8") };
}
