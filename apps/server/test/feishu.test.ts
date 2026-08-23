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
      messageResource: {
        // Mirrors a real SDK failure: an opaque axios message, with Feishu's
        // actual reason only reachable through the streamed response body.
        get: vi.fn(async () => {
          const err = new Error("Request failed with status code 400") as Error & {
            response: { data: string };
          };
          err.response = { data: JSON.stringify({ code: 234003, msg: "File not in msg." }) };
          throw err;
        }),
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
  config.feishuVerificationToken = "verify_test";
}

/** Every event body must now carry the verification token to be accepted. */
function signed(body: Record<string, unknown>): string {
  const header = { ...(body["header"] as Record<string, unknown>), token: "verify_test" };
  return JSON.stringify({ ...body, header });
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
      body: signed({
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
    // third arg: inbound images (empty for a plain text message)
    expect(handleUserMessage).toHaveBeenCalledWith("你好 hidane", "connector:feishu", []);
  });

  it("deduplicates retried events by event_id (Feishu retries until 200)", async () => {
    enableFeishu();
    const app = buildApp();
    const payload = signed({
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
      body: signed({
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

  it("rejects a forged event: the SDK does not enforce the token on plaintext v2 events", async () => {
    enableFeishu();
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        schema: "2.0",
        header: { event_id: "evt_forged", event_type: "im.message.receive_v1", token: "wrong" },
        event: {
          sender: { sender_type: "user" },
          message: {
            message_id: "om_f",
            chat_id: "oc_f",
            chat_type: "p2p",
            message_type: "text",
            create_time: "0",
            content: JSON.stringify({ text: "run something expensive" }),
          },
        },
      }),
    });
    expect(res.status).toBe(401);
    await new Promise((r) => setTimeout(r, 60));
    // Nothing captured, and above all no model call: this endpoint runs
    // worker executions, so an open door costs money and grants execution.
    expect(await listEvents({ kind: "connector.feishu" })).toHaveLength(0);
    const { handleUserMessage } = await import("../src/agents/primary.js");
    expect(handleUserMessage).not.toHaveBeenCalled();
    // The rejection is recorded rather than silent.
    expect(await listEvents({ kind: "agent.error" })).toHaveLength(1);
  });

  it("fails closed when neither verification token nor encrypt key is set", async () => {
    config.feishuAppId = "cli_test";
    config.feishuAppSecret = "secret_test"; // deliberately no token / encrypt key
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        schema: "2.0",
        header: { event_id: "evt_open", event_type: "im.message.receive_v1" },
        event: {
          sender: { sender_type: "user" },
          message: {
            message_id: "om_o",
            chat_id: "oc_o",
            chat_type: "p2p",
            message_type: "text",
            create_time: "0",
            content: JSON.stringify({ text: "hi" }),
          },
        },
      }),
    });
    expect(res.status).toBe(401);
    const errors = await listEvents({ kind: "agent.error" });
    expect(String(errors[0]!.payload["error"])).toContain("FEISHU_VERIFICATION_TOKEN");
  });

  it("accepts an encrypted envelope on the encrypt key alone", async () => {
    const { verifyEventAuth } = await import("../src/connectors/feishu.js");
    config.feishuEncryptKey = "k";
    expect(verifyEventAuth({ encrypt: "base64blob" })).toBe("ok");
    // Plaintext still needs the token even when an encrypt key exists.
    expect(verifyEventAuth({ header: { token: "x" } })).toBe("unconfigured");
    config.feishuVerificationToken = "t";
    expect(verifyEventAuth({ header: { token: "t" } })).toBe("ok");
    expect(verifyEventAuth({ header: { token: "x" } })).toBe("reject");
    expect(verifyEventAuth({ token: "t" })).toBe("ok"); // schema 1.0 shape
  });

  it("extracts image keys from image and post messages (vision model input)", async () => {
    const { imageKeys } = await import("../src/connectors/feishu.js");
    expect(imageKeys("image", JSON.stringify({ image_key: "img_v3_abc" }))).toEqual([
      "img_v3_abc",
    ]);
    expect(
      imageKeys(
        "post",
        JSON.stringify({
          title: "t",
          content: [
            [{ tag: "text", text: "看这张" }, { tag: "img", image_key: "img_1" }],
            [{ tag: "img", image_key: "img_2" }],
          ],
        }),
      ),
    ).toEqual(["img_1", "img_2"]);
    expect(imageKeys("text", JSON.stringify({ text: "hi" }))).toEqual([]);
    expect(imageKeys("image", "not json")).toEqual([]);
  });

  it("captures an image message even when the download fails, and still answers", async () => {
    enableFeishu();
    const app = buildApp();
    await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: signed({
        schema: "2.0",
        header: { event_id: "evt_img_fail", event_type: "im.message.receive_v1", token: "" },
        event: {
          sender: { sender_type: "user" },
          message: {
            message_id: "om_img",
            chat_id: "oc_img",
            chat_type: "p2p",
            message_type: "image",
            create_time: "0",
            content: JSON.stringify({ image_key: "img_v3_broken" }),
          },
        },
      }),
    });
    await new Promise((r) => setTimeout(r, 100));

    // The message must never vanish: capture first, judge never.
    const captured = await listEvents({ kind: "connector.feishu" });
    expect(captured).toHaveLength(1);
    expect(captured[0]!.payload["imageCount"]).toBe(0);
    // Feishu's own reason survives, not just the opaque axios message.
    const failures = captured[0]!.payload["imageFailures"] as string[];
    expect(failures).toHaveLength(1);
    expect(failures[0]).toContain("File not in msg.");
    expect(failures[0]).toContain("234003");
    // ...and the failure is visible rather than swallowed.
    const errors = await listEvents({ kind: "agent.error" });
    expect(String(errors[0]!.payload["error"])).toContain("failed to download 1 image");
    // The agent is still woken, with text that admits the image is unreadable.
    const { handleUserMessage } = await import("../src/agents/primary.js");
    expect(handleUserMessage).toHaveBeenCalledTimes(1);
    expect(String((handleUserMessage as ReturnType<typeof vi.fn>).mock.calls[0]![0])).toContain(
      "下载失败",
    );
  });

  it("records unsupported message types without waking the model", async () => {
    enableFeishu();
    const app = buildApp();
    await app.request("/feishu/events", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: signed({
        schema: "2.0",
        header: { event_id: "evt_sticker", event_type: "im.message.receive_v1", token: "" },
        event: {
          sender: { sender_type: "user" },
          message: {
            message_id: "om_sticker",
            chat_id: "oc_s",
            chat_type: "p2p",
            message_type: "sticker",
            create_time: "0",
            content: JSON.stringify({ file_key: "sticker_1" }),
          },
        },
      }),
    });
    await new Promise((r) => setTimeout(r, 80));
    expect(await listEvents({ kind: "connector.feishu" })).toHaveLength(1);
    const { handleUserMessage } = await import("../src/agents/primary.js");
    expect(handleUserMessage).not.toHaveBeenCalled();
  });

  it("sniffs image content type from magic bytes, not the response header", async () => {
    const { sniffMime } = await import("../src/connectors/feishu.js");
    expect(sniffMime(Buffer.from([0xff, 0xd8, 0xff, 0xe0]), "image/png")).toBe("image/jpeg");
    expect(
      sniffMime(Buffer.from("89504e470d0a1a0a", "hex"), "application/octet-stream"),
    ).toBe("image/png");
    expect(sniffMime(Buffer.from("GIF89a-rest"), "image/png")).toBe("image/gif");
    expect(sniffMime(Buffer.from("unknown bytes"), "image/png")).toBe("image/png");
  });

  it("extractText parses text payloads and strips mentions", () => {
    expect(extractText(JSON.stringify({ text: "@_user_1 帮我做件事" }))).toBe("帮我做件事");
    expect(extractText("not json")).toBe("");
  });

  it("records the p2p chat as the main binding so schedules can deliver outbound", async () => {
    enableFeishu();
    const app = buildApp();
    const message = (eventId: string, text: string) =>
      app.request("/feishu/events", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: signed({
          schema: "2.0",
          header: { event_id: eventId, event_type: "im.message.receive_v1" },
          event: {
            sender: { sender_type: "user" },
            message: {
              message_id: `om_${eventId}`,
              chat_id: "oc_main_bind",
              chat_type: "p2p",
              message_type: "text",
              create_time: "0",
              content: JSON.stringify({ text }),
            },
          },
        }),
      });
    await message("evt_bind_1", "第一条");
    await new Promise((r) => setTimeout(r, 80));
    const binding = await findByChannelRef("feishu", "oc_main_bind", null);
    expect(binding?.kind).toBe("main");
    // Idempotent: a second message must not create a second row.
    await message("evt_bind_2", "第二条");
    await new Promise((r) => setTimeout(r, 80));
    const { findMainBinding } = await import("../src/kernel/bindings.js");
    expect((await findMainBinding("feishu"))?.chatId).toBe("oc_main_bind");
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
