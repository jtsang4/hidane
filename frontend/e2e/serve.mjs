// Starts the real Go backend (`hidane-nogui serve`) for Playwright, on a fresh
// HIDANE_HOME whose settings point every role at the fake agent CLIs.
//
//   node e2e/serve.mjs <port> [FAKEAGENT_DELAY_MS]
//
// Build inputs: `make build-nogui fakeagent` (bin/hidane-nogui, bin/fake/*).
import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const port = process.argv[2] ?? "2797";
const delayMs = process.argv[3] ?? "";
const binary = join(repo, "bin", "hidane-nogui");
const fakes = join(repo, "bin", "fake");

for (const path of [binary, join(fakes, "claude"), join(fakes, "codex"), join(fakes, "pi")]) {
  if (!existsSync(path)) {
    console.error(`[e2e serve] missing ${path} — run \`make build-nogui fakeagent\` first`);
    process.exit(1);
  }
}

const home = mkdtempSync(join(tmpdir(), `hidane-e2e-${port}-`));
const role = { agent: "claude", provider: "", model: "", effort: "low" };
const settings = {
  providers: [],
  roles: { primary: role, manager: role, worker: role, distiller: role },
  binaries: { claude: join(fakes, "claude"), codex: join(fakes, "codex"), pi: join(fakes, "pi") },
};
writeFileSync(join(home, "settings.json"), `${JSON.stringify(settings, null, 2)}\n`, { mode: 0o600 });

const env = {
  ...process.env,
  HIDANE_HOME: home,
  HIDANE_API_TOKEN: "e2e-token",
  HIDANE_WEBHOOK_SECRET: "e2e-secret",
};
if (delayMs) env.FAKEAGENT_DELAY_MS = delayMs;
// A developer's own session must not leak into the fakes the server spawns.
delete env.CLAUDECODE;

console.log(`[e2e serve] home ${home}${delayMs ? `, FAKEAGENT_DELAY_MS=${delayMs}` : ""}`);
const child = spawn(binary, ["serve", "--addr", `127.0.0.1:${port}`], { env, stdio: ["ignore", "inherit", "inherit"] });

let stopping = false;
function stop(signal) {
  if (stopping) return;
  stopping = true;
  child.kill(signal);
}
for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) process.on(signal, () => stop("SIGTERM"));
child.on("exit", (code, signal) => {
  if (!process.env.HIDANE_E2E_KEEP_HOME) rmSync(home, { recursive: true, force: true });
  else console.log(`[e2e serve] kept ${home}`);
  process.exit(stopping ? 0 : (code ?? (signal ? 1 : 0)));
});
