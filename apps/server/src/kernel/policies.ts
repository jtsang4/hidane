import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { config } from "../config.js";
import { genId } from "./ids.js";

/**
 * Capture-phase rules. A worker's tool call passes every policy file from the
 * global one down through each ancestor workspace to its own before it runs,
 * and any of them can refuse it. Rules are data read by the guard, never
 * prompts: a model cannot talk its way past them.
 */
export interface PolicyRule {
  id: string;
  /** Case-insensitive regular expression over the command, or the target path. */
  pattern: string;
  reason: string;
  /** Tools the rule applies to; all mutating tools when omitted. */
  tools?: string[] | undefined;
}

export interface PolicyFile {
  rules: PolicyRule[];
}

export function globalPolicyPath(): string {
  return join(config.home, "POLICY.json");
}

export function workspacePolicyPath(workspace: string): string {
  return join(workspace, ".hidane", "POLICY.json");
}

export async function readPolicy(path: string): Promise<PolicyFile> {
  try {
    const parsed = JSON.parse(await readFile(path, "utf8")) as Partial<PolicyFile>;
    const rules = Array.isArray(parsed.rules) ? parsed.rules : [];
    return {
      rules: rules.filter(
        (r): r is PolicyRule =>
          typeof r?.id === "string" && typeof r.pattern === "string" && typeof r.reason === "string",
      ),
    };
  } catch {
    return { rules: [] };
  }
}

export async function writePolicy(path: string, policy: PolicyFile): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(policy, null, 2)}\n`);
}

/** Why a pattern cannot be used, or null when it compiles. */
export function validatePattern(pattern: string): string | null {
  if (!pattern.trim()) return "pattern required";
  try {
    new RegExp(pattern, "i");
    return null;
  } catch (err) {
    return err instanceof Error ? err.message : String(err);
  }
}

export async function addGlobalRule(input: {
  pattern: string;
  reason: string;
  tools?: string[] | undefined;
}): Promise<PolicyRule> {
  const policy = await readPolicy(globalPolicyPath());
  const rule: PolicyRule = {
    id: genId("pol", 6),
    pattern: input.pattern,
    reason: input.reason,
    ...(input.tools && input.tools.length > 0 ? { tools: input.tools } : {}),
  };
  policy.rules.push(rule);
  await writePolicy(globalPolicyPath(), policy);
  return rule;
}

export async function removeGlobalRule(id: string): Promise<boolean> {
  const policy = await readPolicy(globalPolicyPath());
  const rules = policy.rules.filter((r) => r.id !== id);
  if (rules.length === policy.rules.length) return false;
  await writePolicy(globalPolicyPath(), { rules });
  return true;
}
