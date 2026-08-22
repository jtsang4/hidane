import { Hono } from "hono";
import { serve, type ServerType } from "@hono/node-server";
import { appendEvent } from "../kernel/events.js";
import { sql } from "../kernel/db.js";

/**
 * Passive connector: webhook ingress + health endpoint for deployment probes.
 * Connectors only capture and normalize; they never judge or call agents.
 */
export function buildApp(): Hono {
  const app = new Hono();

  app.get("/health", async (c) => {
    try {
      await sql()`SELECT 1`;
      return c.json({ ok: true, db: "up" });
    } catch (err) {
      return c.json({ ok: false, db: "down", error: String(err) }, 503);
    }
  });

  app.post("/webhook/:name", async (c) => {
    const name = c.req.param("name");
    let body: Record<string, unknown> = {};
    try {
      body = (await c.req.json()) as Record<string, unknown>;
    } catch {
      body = { raw: await c.req.text() };
    }
    const event = await appendEvent({
      source: `connector:webhook:${name}`,
      kind: "connector.webhook",
      payload: { name, body },
    });
    return c.json({ ok: true, eventId: event.id, seq: event.seq });
  });

  return app;
}

export function startHttp(port: number): ServerType {
  const app = buildApp();
  return serve({ fetch: app.fetch, port });
}
