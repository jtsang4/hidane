import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

/**
 * hidane side-effect gate: hard-deny destructive shell patterns at the
 * tool_call boundary. This is the enforcement half of the two-phase
 * side-effect design — visibility never gates, execution does.
 * Extra literal patterns can be injected via HIDANE_GUARD_DENY (one per line).
 */
const DENY: RegExp[] = [
  /rm\s+(-[a-z]*[rf][a-z]*\s+)+\/(\s|$)/i,
  /\bsudo\b/,
  /\bmkfs\b/,
  /\bdd\s+if=/,
  /:\(\)\s*\{\s*:\|:&\s*\};:/,
  /\bshutdown\b|\breboot\b/,
  /git\s+push\s+.*--force/,
];

export default function guard(pi: ExtensionAPI): void {
  const extra = (process.env["HIDANE_GUARD_DENY"] ?? "")
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);

  pi.on("tool_call", async (event) => {
    if (event.toolName !== "bash") return undefined;
    const command = String(
      (event.input as { command?: string } | undefined)?.command ?? "",
    );
    for (const pattern of DENY) {
      if (pattern.test(command)) {
        return { block: true, reason: `blocked by hidane guard: ${pattern}` };
      }
    }
    for (const literal of extra) {
      if (command.includes(literal)) {
        return { block: true, reason: `blocked by hidane guard: ${literal}` };
      }
    }
    return undefined;
  });
}
