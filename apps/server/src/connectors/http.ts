import { createHmac, timingSafeEqual } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { Hono, type Context, type Next } from "hono";
import { serve, type ServerType } from "@hono/node-server";
import { serveStatic } from "@hono/node-server/serve-static";
import { appendEvent } from "../kernel/events.js";
import { sql } from "../kernel/db.js";
import { config } from "../config.js";
import { registerApi } from "../api/routes.js";

/**
 * Passive connector: webhook ingress + health endpoint for deployment probes.
 * Connectors only capture and normalize; they never judge or call agents.
 *
 * Security model: /health is open; /webhook/:name requires an HMAC signature
 * when HIDANE_WEBHOOK_SECRET is set; /api/* requires a bearer token when
 * HIDANE_API_TOKEN is set. Production always sets both.
 */

function safeEqual(a: string, b: string): boolean {
  const ba = Buffer.from(a);
  const bb = Buffer.from(b);
  return ba.length === bb.length && timingSafeEqual(ba, bb);
}

export function signWebhook(body: string, secret: string): string {
  return `sha256=${createHmac("sha256", secret).update(body).digest("hex")}`;
}

export async function requireApiToken(c: Context, next: Next): Promise<Response | void> {
  if (!config.apiToken) return next();
  const header = c.req.header("authorization") ?? "";
  // EventSource cannot set headers, so SSE passes ?token= instead.
  const token = header.startsWith("Bearer ")
    ? header.slice(7)
    : (c.req.query("token") ?? "");
  if (!token || !safeEqual(token, config.apiToken)) {
    return c.json({ ok: false, error: "unauthorized" }, 401);
  }
  return next();
}

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
    const raw = await c.req.text();
    if (config.webhookSecret) {
      const signature = c.req.header("x-hidane-signature") ?? "";
      if (!signature || !safeEqual(signature, signWebhook(raw, config.webhookSecret))) {
        return c.json({ ok: false, error: "invalid signature" }, 401);
      }
    }
    let body: Record<string, unknown>;
    try {
      body = JSON.parse(raw) as Record<string, unknown>;
    } catch {
      body = { raw };
    }
    const event = await appendEvent({
      source: `connector:webhook:${name}`,
      kind: "connector.webhook",
      payload: { name, body },
    });
    return c.json({ ok: true, eventId: event.id, seq: event.seq });
  });

  app.use("/api/*", requireApiToken);
  registerApi(app);

  // Serve the built web app when present (production single-container mode).
  const webDist = resolve(process.cwd(), config.webDist);
  if (existsSync(join(webDist, "index.html"))) {
    app.use("*", serveStatic({ root: config.webDist }));
    const indexHtml = readFileSync(join(webDist, "index.html"), "utf8");
    app.get("*", (c) => c.html(indexHtml));
  }

  return app;
}

export function startHttp(port: number): ServerType {
  const app = buildApp();
  return serve({ fetch: app.fetch, port });
}
