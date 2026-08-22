import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("../src/agents/primary.js", () => ({
  handleUserMessage: vi.fn(async () => ({ action: "reply", reply: "stub" })),
}));
vi.mock("../src/agents/manager.js", () => ({
  handleThreadMessage: vi.fn(async () => "stub"),
}));
// The Feishu SDK client would hit the network on delivery; stub the outbound API.
vi.mock("@larksuiteoapi/node-sdk", async (importOriginal) => {
  const actual = (await importOriginal()) as typeof import("@larksuiteoapi/node-sdk");
  class StubClient {
    im = {
      message: {
        create: vi.fn(async () => ({ data: { message_id: "om_stub_root" } })),
        reply: vi.fn(async () => ({ data: { message_id: "om_stub_reply" } })),
      },
    };
  }
  return { ...actual, Client: StubClient };
});

import { buildApp } from "../src/connectors/http.js";
import { extractText, feishuEnabled } from "../src/connectors/feishu.js";
import { config } from "../src/config.js";
import { createBinding, findByChannelRef, findByWorkItem } from "../src/kernel/bindings.js";
import { listEvents } from "../src/kernel/events.js";

function enableFeishu() {
  config.feishuAppId = "cli_test";
  config.feishuAppSecret = "secret_test";
}

afterEach(() => {
  config.feishuAppId = undefined;
  config.feishuAppSecret = undefined;
  config.feishuVerificationToken = undefined;
  config.feishuEncryptKey = undefined;
  vi.clearAllMocks();
});

describe("feishu connector (official SDK)", () => {
  it("is disabled without credentials and 503s the endpoint", async () => {
    expect(feishuEnabled()).toBe(false);
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({ type: "url_verification", challenge: "abc" }),
    });
    expect(res.status).toBe(503);
  });

  it("answers the url_verification challenge via the SDK dispatcher", async () => {
    enableFeishu();
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ type: "url_verification", challenge: "xyz123" }),
    });
    expect(res.status).toBe(200);
    expect(((await res.json()) as { challenge: string }).challenge).toBe("xyz123");
  });

  it("dispatches a user message to connector.feishu and the main-thread fast lane", async () => {
    enableFeishu();
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        schema: "2.0",
        header: {
          event_id: "evt_1",
          event_type: "im.message.receive_v1",
          token: "",
          create_time: "0",
          tenant_key: "t",
          app_id: "cli_test",
        },
        event: {
          sender: { sender_type: "user", sender_id: { open_id: "ou_1" } },
          message: {
            message_id: "om_1",
            chat_id: "oc_1",
            chat_type: "p2p",
            message_type: "text",
            create_time: "0",
            content: JSON.stringify({ text: "你好 hidane" }),
          },
        },
      }),
    });
    expect(res.status).toBe(200);
    await new Promise((r) => setTimeout(r, 80));
    const captured = await listEvents({ kind: "connector.feishu" });
    expect(captured).toHaveLength(1);
    expect(captured[0]!.payload["chatId"]).toBe("oc_1");
    expect(captured[0]!.payload["text"]).toBe("你好 hidane");

    const { handleUserMessage } = await import("../src/agents/primary.js");
    expect(handleUserMessage).toHaveBeenCalledWith("你好 hidane", "connector:feishu");
  });

  it("deduplicates retried events by event_id (Feishu retries until 200)", async () => {
    enableFeishu();
    const app = buildApp();
    const payload = JSON.stringify({
      schema: "2.0",
      header: { event_id: "evt_retry_1", event_type: "im.message.receive_v1", token: "" },
      event: {
        sender: { sender_type: "user" },
        message: {
          message_id: "om_r",
          chat_id: "oc_r",
          chat_type: "p2p",
          message_type: "text",
          create_time: "0",
          content: JSON.stringify({ text: "重复推送测试" }),
        },
      },
    });
    const send = () =>
      app.request("/feishu/events", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: payload,
      });
    expect((await send()).status).toBe(200);
    expect((await send()).status).toBe(200);
    expect((await send()).status).toBe(200);
    await new Promise((r) => setTimeout(r, 100));
    // Exactly one capture and one LLM chain despite three deliveries.
    expect(await listEvents({ kind: "connector.feishu" })).toHaveLength(1);
    const { handleUserMessage } = await import("../src/agents/primary.js");
    expect(handleUserMessage).toHaveBeenCalledTimes(1);
  });

  it("ignores bot echoes (sender_type != user)", async () => {
    enableFeishu();
    const app = buildApp();
    await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        schema: "2.0",
        header: { event_id: "evt_2", event_type: "im.message.receive_v1", token: "" },
        event: {
          sender: { sender_type: "app" },
          message: {
            message_id: "om_2",
            chat_id: "oc_1",
            chat_type: "p2p",
            message_type: "text",
            create_time: "0",
            content: JSON.stringify({ text: "echo" }),
          },
        },
      }),
    });
    await new Promise((r) => setTimeout(r, 60));
    expect(await listEvents({ kind: "connector.feishu" })).toHaveLength(0);
  });

  it("extractText parses text payloads and strips mentions", () => {
    expect(extractText(JSON.stringify({ text: "@_user_1 帮我做件事" }))).toBe("帮我做件事");
    expect(extractText("not json")).toBe("");
  });

  it("binding lookups route by chat/root and by work item", async () => {
    await createBinding({ channel: "feishu", kind: "main", chatId: "oc_9" });
    const wi = await createBinding({
      channel: "feishu",
      kind: "work_item",
      workItemId: "wi_abc",
      chatId: "oc_9",
      rootId: "om_root",
    });
    expect((await findByChannelRef("feishu", "oc_9", "om_root"))?.workItemId).toBe("wi_abc");
    expect((await findByChannelRef("feishu", "oc_9", null))?.kind).toBe("main");
    expect((await findByWorkItem("feishu", "wi_abc"))?.id).toBe(wi.id);
  });
});
