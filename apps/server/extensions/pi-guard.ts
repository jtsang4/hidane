import { existsSync, readFileSync } from "node:fs";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

/**
 * hidane side-effect gate: the capture phase of the work tree. Every tool call
 * passes, in order, the built-in deny list, each policy file from the global
 * one down to this workspace's, and the pending-input check — any of them can
 * refuse it. Visibility never gates; execution does.
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

const MUTATING_TOOLS = new Set(["bash", "write", "edit"]);

/**
 * Shell commands that only look. Anything else is treated as a change, which
 * errs toward pausing: a read that waits one turn costs little, a write made
 * against superseded instructions may not be undoable.
 */
const READ_ONLY_COMMAND =
  /^\s*(ls|cat|head|tail|grep|rg|find|pwd|wc|file|stat|tree|du|which|echo|git\s+(status|log|diff|show|branch))\b[^;&|>]*$/;

interface PolicyRule {
  id: string;
  pattern: string;
  reason: string;
  tools?: string[];
}

function loadRules(): PolicyRule[] {
  const files = (process.env["HIDANE_POLICY_FILES"] ?? "").split("\n").filter(Boolean);
  const rules: PolicyRule[] = [];
  for (const file of files) {
    try {
      const parsed = JSON.parse(readFileSync(file, "utf8")) as { rules?: PolicyRule[] };
      for (const rule of parsed.rules ?? []) {
        if (typeof rule?.pattern === "string" && typeof rule.id === "string") rules.push(rule);
      }
    } catch {
      // A missing or unreadable file contributes no rules.
    }
  }
  return rules;
}

function subjectOf(toolName: string, input: Record<string, unknown> | undefined): string {
  if (toolName === "bash") return String(input?.["command"] ?? "");
  return String(input?.["path"] ?? input?.["file_path"] ?? "");
}

export default function guard(pi: ExtensionAPI): void {
  const extra = (process.env["HIDANE_GUARD_DENY"] ?? "")
    .split("\n")
    .map((s) => s.trim())
    .filter(Boolean);
  const pendingInputFile = process.env["HIDANE_PENDING_INPUT_FILE"];

  pi.on("tool_call", async (event) => {
    const input = event.input as Record<string, unknown> | undefined;
    const subject = subjectOf(event.toolName, input);

    if (event.toolName === "bash") {
      for (const pattern of DENY) {
        if (pattern.test(subject)) {
          return { block: true, reason: `blocked by hidane guard: ${pattern}` };
        }
      }
      for (const literal of extra) {
        if (subject.includes(literal)) {
          return { block: true, reason: `blocked by hidane guard: ${literal}` };
        }
      }
    }

    for (const rule of loadRules()) {
      const tools = rule.tools && rule.tools.length > 0 ? rule.tools : [...MUTATING_TOOLS];
      if (!tools.includes(event.toolName)) continue;
      let matches = false;
      try {
        matches = new RegExp(rule.pattern, "i").test(subject);
      } catch {
        matches = false;
      }
      if (matches) {
        return { block: true, reason: `blocked by hidane policy ${rule.id}: ${rule.reason}` };
      }
    }

    const mutating =
      MUTATING_TOOLS.has(event.toolName) &&
      !(event.toolName === "bash" && READ_ONLY_COMMAND.test(subject));
    if (mutating && pendingInputFile && existsSync(pendingInputFile)) {
      return {
        block: true,
        reason:
          "hidane: the user has sent new input that you have not read yet. Do not change anything now; it arrives before your next step — re-plan with it first.",
      };
    }
    return undefined;
  });
}
