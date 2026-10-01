/** Shared between playwright.config.ts and the specs. */

/** The normal backend: fake agents answer immediately. */
export const PORT = 2797;
/** A second backend whose fake agents sleep each turn, so a run can be caught mid-flight. */
export const SLOW_PORT = 2798;
export const SLOW_DELAY_MS = 8000;

export const BASE_URL = `http://127.0.0.1:${PORT}`;
export const SLOW_URL = `http://127.0.0.1:${SLOW_PORT}`;

/** Must match what e2e/serve.mjs puts in the server's environment. */
export const TOKEN = "e2e-token";
export const WEBHOOK_SECRET = "e2e-secret";
