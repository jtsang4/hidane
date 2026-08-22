import { describe, expect, it } from "vitest";
import { buildApp } from "../src/connectors/http.js";
import { listEvents } from "../src/kernel/events.js";
import { triageOnce } from "../src/connectors/triageLoop.js";
import { appendEvent } from "../src/kernel/events.js";

describe("http connector", () => {
  it("health reports db up", async () => {
    const app = buildApp();
    const res = await app.request("/health");
    expect(res.status).toBe(200);
    const body = (await res.json()) as { ok: boolean; db: string };
    expect(body.ok).toBe(true);
    expect(body.db).toBe("up");
  });

  it("webhook ingress appends a connector event (capture only, no judgment)", async () => {
    const app = buildApp();
    const res = await app.request("/webhook/test-source", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ hello: "world" }),
    });
    expect(res.status).toBe(200);
    const events = await listEvents({ kind: "connector.webhook" });
    expect(events).toHaveLength(1);
    expect(events[0]!.source).toBe("connector:webhook:test-source");
    const body = events[0]!.payload["body"] as Record<string, unknown>;
    expect(body["hello"]).toBe("world");
  });

  it("triage records heartbeat without waking anything and commits its cursor", async () => {
    await appendEvent({
      source: "connector:timer",
      kind: "connector.heartbeat",
      payload: {},
    });
    const res = await triageOnce();
    expect(res.handled).toBeGreaterThan(0);
    expect(res.woke).toBe(0);
    const decisions = await listEvents({ kind: "triage.decision" });
    expect(decisions).toHaveLength(1);
    expect(decisions[0]!.payload["action"]).toBe("record");

    // second pass sees nothing new except its own decision event
    const res2 = await triageOnce();
    expect(res2.woke).toBe(0);
  });
});
