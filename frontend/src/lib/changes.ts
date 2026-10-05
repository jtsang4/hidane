/**
 * A task's unified diff, cut per file so each file's change opens on its own.
 * The path comes from the section's own lines: `+++ b/…` names the file after
 * the change, `--- a/…` a deleted one, `rename to` a rename without edits.
 */
export function patchSections(patch: string): Map<string, string> {
  const sections = new Map<string, string>();
  const chunks = patch.split(/^(?=diff --git )/m).filter((chunk) => chunk.startsWith("diff --git "));
  for (const chunk of chunks) {
    const path = pathOf(chunk);
    if (path) sections.set(path, chunk.replace(/\n$/, ""));
  }
  return sections;
}

function pathOf(chunk: string): string | null {
  const lines = chunk.split("\n");
  const after = lines.find((line) => line.startsWith("+++ b/"));
  if (after) return after.slice("+++ b/".length);
  const renamed = lines.find((line) => line.startsWith("rename to "));
  if (renamed) return renamed.slice("rename to ".length);
  const before = lines.find((line) => line.startsWith("--- a/"));
  if (before) return before.slice("--- a/".length);
  // `diff --git a/P b/P` with no content lines (a mode change, an empty file).
  const header = (lines[0] ?? "").slice("diff --git ".length);
  const n = (header.length - "a/ b/".length) / 2;
  return Number.isInteger(n) && n > 0 ? header.slice(header.length - n) : null;
}

export type PatchLine = { text: string; kind: "add" | "del" | "hunk" | "meta" | "context" };

/** How each line of a file's section reads: added, removed, a hunk header, or file metadata. */
export function patchLines(section: string): PatchLine[] {
  let inHunk = false;
  return section.split("\n").map((text) => {
    if (text.startsWith("@@")) {
      inHunk = true;
      return { text, kind: "hunk" };
    }
    if (!inHunk) return { text, kind: "meta" };
    if (text.startsWith("+")) return { text, kind: "add" };
    if (text.startsWith("-")) return { text, kind: "del" };
    return { text, kind: "context" };
  });
}
