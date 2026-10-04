/** Shared between playwright.config.ts and the specs. */

/** Another checkout running its E2E at the same time sets this to keep clear of ours. */
const base = Number(process.env.HIDANE_E2E_PORT ?? 2797);
/** The normal backend: fake agents answer immediately. */
export const PORT = base;
/** A second backend whose fake workers sleep, so a run can be caught mid-flight. */
export const SLOW_PORT = base + 1;
export const SLOW_DELAY_MS = 8000;

export const BASE_URL = `http://127.0.0.1:${PORT}`;
export const SLOW_URL = `http://127.0.0.1:${SLOW_PORT}`;

/** Must match what e2e/serve.mjs puts in the server's environment. */
export const TOKEN = "e2e-token";
export const WEBHOOK_SECRET = "e2e-secret";
