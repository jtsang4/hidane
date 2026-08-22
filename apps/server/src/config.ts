import { homedir } from "node:os";
import { join } from "node:path";

function env(name: string): string | undefined {
  const v = process.env[name];
  return v && v.length > 0 ? v : undefined;
}

export const config = {
  /** Postgres connection string. */
  databaseUrl:
    env("DATABASE_URL") ?? "postgres://hidane:hidane@localhost:2716/hidane",
  /** Root for workspaces, worklogs and agent session traces. */
  home: env("HIDANE_HOME") ?? join(homedir(), ".hidane"),
  /** HTTP port for the daemon (health + webhook connector). */
  port: Number(env("PORT") ?? 2718),
  /** Heartbeat connector interval in seconds. */
  heartbeatIntervalSec: Number(env("HIDANE_HEARTBEAT_SEC") ?? 300),
  /** Optional pi provider/model overrides; defaults come from pi settings. */
  piProvider: env("HIDANE_PI_PROVIDER"),
  piModel: env("HIDANE_PI_MODEL"),
  /** Thinking level for routing/planning calls (primary, manager). */
  routeThinking: env("HIDANE_ROUTE_THINKING") ?? "low",
  /** Thinking level for worker executions. */
  workerThinking: env("HIDANE_WORKER_THINKING") ?? "medium",
  /** Timeout (seconds) for a single worker execution. */
  workerTimeoutSec: Number(env("HIDANE_WORKER_TIMEOUT_SEC") ?? 600),
  /** Timeout (seconds) for routing/planning calls. */
  routeTimeoutSec: Number(env("HIDANE_ROUTE_TIMEOUT_SEC") ?? 180),
  /** Bearer token required on /api/* when set. Always set in production. */
  apiToken: env("HIDANE_API_TOKEN"),
  /** HMAC-SHA256 secret for webhook signatures when set. Always set in production. */
  webhookSecret: env("HIDANE_WEBHOOK_SECRET"),
};

export function workspacesDir(): string {
  return join(config.home, "workspaces");
}

export function worklogsDir(): string {
  return join(config.home, "worklogs");
}

export function sessionsDir(): string {
  return join(config.home, "sessions");
}
