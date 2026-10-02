import { defineConfig, devices } from "@playwright/test";
import { BASE_URL, PORT, SLOW_DELAY_MS, SLOW_PORT, SLOW_URL } from "./e2e/env.js";

/**
 * E2E against the real Go backend (`bin/hidane-nogui serve`) with the fake
 * agent CLIs from `cmd/fakeagent`. Build both first: `make build-nogui fakeagent`.
 *
 * One server is shared by every test, so tests run serially and keep to their
 * own unique texts instead of assuming an empty log.
 */
export default defineConfig<{ desktop: boolean }>({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: BASE_URL,
    locale: "zh-CN",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"] }, testIgnore: /desktop\.spec\.ts/ },
    // The closest stand-in for the macOS WKWebView the desktop app runs in.
    { name: "webkit", use: { ...devices["Desktop Safari"] }, testIgnore: /desktop\.spec\.ts/ },
    // The desktop app's UI (`/boot.js` says desktop) against the same backend.
    // No token gate there, so token-gate.spec.ts is the browser's alone.
    { name: "chromium-desktop", use: { ...devices["Desktop Chrome"], desktop: true }, testIgnore: /token-gate\.spec\.ts/ },
    { name: "webkit-desktop", use: { ...devices["Desktop Safari"], desktop: true }, testIgnore: /token-gate\.spec\.ts/ },
  ],
  webServer: [
    {
      command: `node e2e/serve.mjs ${PORT}`,
      url: `${BASE_URL}/health`,
      reuseExistingServer: false,
      timeout: 30_000,
      stdout: "pipe",
      stderr: "pipe",
      // serve.mjs stops the server and removes its temporary HIDANE_HOME on SIGTERM.
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
    },
    {
      command: `node e2e/serve.mjs ${SLOW_PORT} ${SLOW_DELAY_MS}`,
      url: `${SLOW_URL}/health`,
      reuseExistingServer: false,
      timeout: 30_000,
      stdout: "pipe",
      stderr: "pipe",
      // serve.mjs stops the server and removes its temporary HIDANE_HOME on SIGTERM.
      gracefulShutdown: { signal: "SIGTERM", timeout: 5_000 },
    },
  ],
});
