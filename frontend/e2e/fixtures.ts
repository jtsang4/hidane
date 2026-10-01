import { test as base, expect, type APIRequestContext, type APIResponse, type Locator, type Page } from "@playwright/test";
import type { AgentKind, HidaneEvent, Role, RoleConfig, Settings, WorkItem } from "../src/lib/api.js";
import { TOKEN, WEBHOOK_SECRET } from "./env.js";

export { expect };

export const AUTH = { authorization: `Bearer ${TOKEN}` } as const;
export const ROLES: readonly Role[] = ["primary", "manager", "worker", "distiller"];

/** A short suffix that keeps one test's texts apart from every other test on the shared server. */
export function unique(prefix: string): string {
  return `${prefix}-${Math.random().toString(36).slice(2, 8)}`;
}

async function json<T>(response: APIResponse, what: string, status = 200): Promise<T> {
  expect(response.status(), `${what}: ${await response.text()}`).toBe(status);
  return (await response.json()) as T;
}

/** The backend's HTTP API, authenticated. Evidence for what the UI claims. */
export class Api {
  constructor(readonly request: APIRequestContext) {}

  get<T>(path: string): Promise<T> {
    return this.request.get(path, { headers: AUTH }).then((r) => json<T>(r, `GET ${path}`));
  }

  send<T>(method: "POST" | "PUT" | "PATCH" | "DELETE", path: string, data?: unknown, status = 200): Promise<T> {
    return this.request
      .fetch(path, { method, headers: AUTH, ...(data === undefined ? {} : { data }) })
      .then((r) => json<T>(r, `${method} ${path}`, status));
  }

  /** `GET /api/events?<query>` — the unpaged form, oldest first. */
  async events(query = ""): Promise<HidaneEvent[]> {
    return (await this.get<{ events: HidaneEvent[] }>(`/api/events${query ? `?${query}` : ""}`)).events;
  }

  /** Everything appended after (and including) one event, oldest first. */
  async eventsFrom(event: HidaneEvent): Promise<HidaneEvent[]> {
    return this.events(`after=${event.seq - 1}`);
  }

  async event(id: string): Promise<HidaneEvent> {
    const found = (await this.events(`tail=500`)).find((event) => event.id === id);
    expect(found, `event ${id} in the log`).toBeDefined();
    return found as HidaneEvent;
  }

  async workItems(): Promise<WorkItem[]> {
    return (await this.get<{ items: WorkItem[] }>("/api/work-items?all")).items;
  }

  settings(): Promise<Settings> {
    return this.get<Settings>("/api/settings");
  }

  /** Every role on one CLI with its own login: the baseline most tests assume. */
  async setAllRoles(agent: AgentKind): Promise<void> {
    const config: RoleConfig = { agent, provider: "", model: "", effort: "low" };
    const roles = Object.fromEntries(ROLES.map((role) => [role, config]));
    await this.send("PUT", "/api/settings/roles", { roles });
  }

  /** Back to the state serve.mjs started with: claude everywhere, no providers. */
  async resetSettings(): Promise<void> {
    await this.setAllRoles("claude");
    for (const provider of (await this.settings()).providers) {
      await this.send("DELETE", `/api/providers/${provider.id}`);
    }
  }

  /** Signed exactly as the webhook connector checks it: HMAC-SHA256 over the raw body. */
  async webhook(name: string, body: string, signature?: string): Promise<APIResponse> {
    return this.request.post(`/webhook/${name}`, {
      headers: { "content-type": "application/json", "x-hidane-signature": signature ?? (await sign(body, WEBHOOK_SECRET)) },
      data: body,
    });
  }
}

export async function sign(body: string, secret: string): Promise<string> {
  const encoder = new TextEncoder();
  const key = await crypto.subtle.importKey("raw", encoder.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const mac = new Uint8Array(await crypto.subtle.sign("HMAC", key, encoder.encode(body)));
  return `sha256=${[...mac].map((byte) => byte.toString(16).padStart(2, "0")).join("")}`;
}

/** Wait until the log holds an event matching `pick`, polling the API. */
export async function waitForEvent(
  api: Api,
  query: string,
  pick: (event: HidaneEvent) => boolean,
  message: string,
  timeout = 30_000,
): Promise<HidaneEvent> {
  let found: HidaneEvent | undefined;
  await expect
    .poll(
      async () => {
        found = (await api.events(query)).find(pick);
        return found !== undefined;
      },
      { message, timeout, intervals: [250, 500, 1000] },
    )
    .toBe(true);
  return found as HidaneEvent;
}

/** One question and every answer grouped under it in the conversation. */
export function turn(page: Page, rootId: string): Locator {
  return page.locator(`section[data-root="${rootId}"]`);
}

/** Type into the conversation composer, send, and return the accepted message id. */
export async function say(page: Page, text: string): Promise<string> {
  const composer = page.getByPlaceholder("说点什么…");
  await composer.fill(text);
  const accepted = page.waitForResponse((response) => response.url().endsWith("/api/chat") && response.request().method() === "POST");
  await page.getByRole("button", { name: "发送", exact: true }).click();
  const response = await accepted;
  expect(response.status()).toBe(202);
  const body = (await response.json()) as { messageId: string };
  expect(body.messageId).toMatch(/^ev_/);
  return body.messageId;
}

type Fixtures = {
  /** Put the API token where the SPA's token gate looks for it before any script runs. */
  withToken: boolean;
  /** Browser errors a test expects (matched against "pageerror: …" / "console: …"). */
  allowedErrors: RegExp[];
  browserErrors: string[];
  api: Api;
};

export const test = base.extend<Fixtures>({
  withToken: [true, { option: true }],
  allowedErrors: [[], { option: true }],

  page: async ({ page, withToken }, use) => {
    if (withToken) {
      await page.addInitScript((token) => window.localStorage.setItem("hidane-token", token), TOKEN);
    }
    // Every confirm() in the app guards a destructive action the test means to take.
    page.on("dialog", (dialog) => void dialog.accept());
    await use(page);
  },

  browserErrors: [
    async ({ page, allowedErrors }, use) => {
      const errors: string[] = [];
      page.on("pageerror", (error) => errors.push(`pageerror: ${error.message}`));
      page.on("console", (message) => {
        if (message.type() === "error") errors.push(`console: ${message.text()}`);
      });
      await use(errors);
      const unexpected = errors.filter((error) => !allowedErrors.some((pattern) => pattern.test(error)));
      expect(unexpected, "unexpected browser errors").toEqual([]);
    },
    { auto: true },
  ],

  api: async ({ request }, use) => {
    await use(new Api(request));
  },
});
