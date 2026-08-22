/**
 * Agent-driven acceptance: an agent reads acceptance/scenarios.md, exercises
 * the REAL system end to end, and writes an evidence-based verdict report.
 *
 * Scenarios are natural language — cheap to change as requirements evolve, and
 * able to express semantic checks (e.g. "the reply matches what actually
 * happened") that assertion scripts cannot.
 *
 * Run with: pnpm e2e   (requires dev postgres + pi authed)
 */
import { readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { execa } from "execa";

const REPO = join(import.meta.dirname, "..");
const REPORT = join(REPO, ".acceptance-report.json");

const CHARTER = `
You are the acceptance tester for hidane, a persistent personal agent runtime.
Execute EVERY scenario in the provided document against the real system.

Rules:
- Evidence or it didn't happen: every verdict must cite actual command output,
  file content, or database/API responses you observed in THIS run. Never mark
  PASS from assumptions or from reading source code.
- Prefer end-user surfaces (CLI, HTTP) to drive; corroborate through the
  database, filesystem and event log.
- Semantic checks matter: judge whether replies/artifacts genuinely match what
  happened, not just that commands exited 0.
- If the environment blocks a scenario, mark it BLOCKED with the exact reason.
- Clean up any background process you started.
- When finished, write the report to .acceptance-report.json in the repo root:
  {"scenarios":[{"id":1,"name":"...","verdict":"PASS|FAIL|BLOCKED","evidence":"<concrete observed evidence, concise>"}],
   "summary":{"pass":N,"fail":N,"blocked":N},"notes":"<anything worth flagging>"}
  Then print exactly: ACCEPTANCE DONE
`.trim();

interface Report {
  scenarios: { id: number; name: string; verdict: string; evidence: string }[];
  summary: { pass: number; fail: number; blocked: number };
  notes?: string;
}

async function main(): Promise<void> {
  await rm(REPORT, { force: true });
  const scenarios = await readFile(join(REPO, "acceptance", "scenarios.md"), "utf8");

  console.log("== agent-driven acceptance: spawning tester agent ==\n");
  const result = await execa(
    "pi",
    [
      "-p",
      "--no-extensions",
      "--no-skills",
      "--thinking",
      process.env["HIDANE_ACCEPT_THINKING"] ?? "medium",
      "--session-dir",
      join(REPO, ".acceptance-sessions"),
      "--append-system-prompt",
      CHARTER,
      `Acceptance scenarios to execute now:\n\n${scenarios}`,
    ],
    {
      cwd: REPO,
      stdin: "ignore",
      timeout: 1800 * 1000,
      reject: false,
      env: { PI_OFFLINE: "1" },
      stdout: "inherit",
      stderr: "inherit",
    },
  );

  if (result.failed) {
    console.error(`\ntester agent failed: ${result.timedOut ? "timeout" : result.exitCode}`);
    process.exit(1);
  }

  let report: Report;
  try {
    report = JSON.parse(await readFile(REPORT, "utf8")) as Report;
  } catch {
    console.error("\nno valid .acceptance-report.json produced — treating as failure");
    process.exit(1);
  }

  console.log("\n== acceptance report ==");
  for (const s of report.scenarios) {
    console.log(`[${s.verdict}] #${s.id} ${s.name}\n    evidence: ${s.evidence}`);
  }
  if (report.notes) console.log(`notes: ${report.notes}`);
  const { pass, fail, blocked } = report.summary;
  console.log(`\nsummary: ${pass} pass, ${fail} fail, ${blocked} blocked`);
  process.exit(fail > 0 || blocked > 0 ? 1 : 0);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
