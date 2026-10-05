// What screenshots.mjs and readme-shots.mjs share: the real app (serve.mjs)
// on a port of their own, its API, and the desktop app's UI in a browser.
import { spawn } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const here = dirname(fileURLToPath(import.meta.url));
export const token = "e2e-token";

/**
 * Serves the app on HIDANE_E2E_PORT − offset (the specs take n and n+1) while
 * `run(base, api)` runs, once /health answers. The page errors `run` returns
 * fail the process.
 */
export async function withServer({ offset, env = process.env }, run) {
  const port = String(Number(process.env.HIDANE_E2E_PORT || 2797) - offset);
  const base = `http://127.0.0.1:${port}`;
  const server = spawn(process.execPath, [join(here, "serve.mjs"), port], { stdio: ["ignore", "ignore", "inherit"], env });
  const stop = () => server.kill("SIGTERM");
  process.on("exit", stop);
  const api = async (path, init = {}) => {
    const res = await fetch(base + path, {
      ...init,
      headers: { authorization: `Bearer ${token}`, "content-type": "application/json", ...(init.headers ?? {}) },
    });
    if (!res.ok) throw new Error(`${path}: ${res.status} ${await res.text()}`);
    return res.json();
  };
  try {
    await ready(base);
    const failures = await run(base, api);
    if (failures.length > 0) {
      console.error(failures.join("\n"));
      process.exitCode = 1;
    }
  } finally {
    stop();
  }
}

async function ready(base) {
  for (let i = 0; i < 300; i++) {
    try {
      if ((await fetch(`${base}/health`)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error("server did not start");
}

/** The desktop app's UI in a plain browser: boot.js says desktop, the host endpoints answer. */
export async function pretendDesktop(context, version) {
  await context.route("**/boot.js", (route) =>
    route.fulfill({ contentType: "application/javascript", body: `window.hidaneBoot = {"desktop":true,"auth":false,"version":"${version}"};` }),
  );
  // No Wails runtime: the live stream falls back to SSE, as the specs exercise.
  await context.route("**/wails/runtime.js", (route) => route.fulfill({ contentType: "application/javascript", body: "export {};" }));
  await context.route("**/api/desktop/**", (route) => route.fulfill({ contentType: "application/json", body: `{"ok":true}` }));
}
