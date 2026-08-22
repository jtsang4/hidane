import { afterEach, describe, expect, it, vi } from "vitest";
import { buildApp, signWebhook } from "../src/connectors/http.js";
import { config } from "../src/config.js";
import { listEvents } from "../src/kernel/events.js";

afterEach(() => {
  vi.restoreAllMocks();
  config.webhookSecret = undefined;
  config.apiToken = undefined;
});

describe("webhook signature", () => {
  it("accepts a correctly signed webhook when secret is set", async () => {
    config.webhookSecret = "test-secret";
    const app = buildApp();
    const body = JSON.stringify({ hello: "signed" });
    const res = await app.request("/webhook/signed-source", {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-hidane-signature": signWebhook(body, "test-secret"),
      },
      body,
    });
    expect(res.status).toBe(200);
    const events = await listEvents({ kind: "connector.webhook" });
    expect(events).toHaveLength(1);
  });

  it("rejects missing or wrong signatures when secret is set", async () => {
    config.webhookSecret = "test-secret";
    const app = buildApp();
    const body = JSON.stringify({ hello: "unsigned" });

    const missing = await app.request("/webhook/x", { method: "POST", body });
    expect(missing.status).toBe(401);

    const wrong = await app.request("/webhook/x", {
      method: "POST",
      headers: { "x-hidane-signature": signWebhook(body, "other-secret") },
      body,
    });
    expect(wrong.status).toBe(401);

    const events = await listEvents({ kind: "connector.webhook" });
    expect(events).toHaveLength(0);
  });

  it("stays open when no secret configured (dev mode)", async () => {
    const app = buildApp();
    const res = await app.request("/webhook/dev", {
      method: "POST",
      body: JSON.stringify({ dev: true }),
    });
    expect(res.status).toBe(200);
  });

  it("health endpoint is always open", async () => {
    config.webhookSecret = "test-secret";
    config.apiToken = "test-token";
    const app = buildApp();
    const res = await app.request("/health");
    expect(res.status).toBe(200);
  });
});
