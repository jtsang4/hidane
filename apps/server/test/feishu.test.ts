import { afterEach, describe, expect, it, vi } from "vitest";
import { createCipheriv, createHash, randomBytes } from "node:crypto";

vi.mock("../src/agents/primary.js", () => ({
  handleUserMessage: vi.fn(async () => ({ action: "reply", reply: "stub" })),
}));
vi.mock("../src/agents/manager.js", () => ({
  handleThreadMessage: vi.fn(async () => "stub"),
}));

import { buildApp } from "../src/connectors/http.js";
import { decryptFeishu, extractText, isUserMessage } from "../src/connectors/feishu.js";
import { config } from "../src/config.js";
import { createBinding, findByChannelRef, findByWorkItem } from "../src/kernel/bindings.js";
import { listEvents } from "../src/kernel/events.js";

function encryptFeishu(payload: string, key: string): string {
  const iv = randomBytes(16);
  const cipher = createCipheriv("aes-256-cbc", createHash("sha256").update(key).digest(), iv);
  return Buffer.concat([iv, cipher.update(payload, "utf8"), cipher.final()]).toString("base64");
}

afterEach(() => {
  config.feishuVerificationToken = undefined;
  config.feishuEncryptKey = undefined;
  config.feishuAppId = undefined;
  config.feishuAppSecret = undefined;
});

describe("feishu connector", () => {
  it("answers the url_verification challenge (plain)", async () => {
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({ type: "url_verification", challenge: "abc", token: "t" }),
    });
    expect(((await res.json()) as { challenge: string }).challenge).toBe("abc");
  });

  it("answers the challenge through an encrypted envelope and rejects bad tokens", async () => {
    config.feishuEncryptKey = "ek";
    config.feishuVerificationToken = "vt";
    const app = buildApp();
    const good = JSON.stringify({ type: "url_verification", challenge: "xyz", token: "vt" });
    const res = await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({ encrypt: encryptFeishu(good, "ek") }),
    });
    expect(((await res.json()) as { challenge: string }).challenge).toBe("xyz");

    const bad = JSON.stringify({ type: "url_verification", challenge: "x", token: "wrong" });
    const rej = await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({ encrypt: encryptFeishu(bad, "ek") }),
    });
    expect(rej.status).toBe(401);
  });

  it("decrypt round-trips", () => {
    const secret = "my-encrypt-key";
    const text = JSON.stringify({ hello: "世界" });
    expect(decryptFeishu(encryptFeishu(text, secret), secret)).toBe(text);
  });

  it("extracts text and strips mentions; filters non-user senders", () => {
    const event = {
      sender: { sender_type: "user" },
      message: {
        message_type: "text",
        content: JSON.stringify({ text: "@_user_1 帮我做件事" }),
      },
    };
    expect(extractText(event)).toBe("帮我做件事");
    expect(isUserMessage(event)).toBe(true);
    expect(isUserMessage({ sender: { sender_type: "app" } })).toBe(false);
  });

  it("records connector.feishu events for user messages (capture before judgment)", async () => {
    config.feishuVerificationToken = "vt";
    config.feishuAppId = "app";
    config.feishuAppSecret = "secret";
    const app = buildApp();
    const res = await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({
        header: { event_id: "ev1", event_type: "im.message.receive_v1", token: "vt" },
        event: {
          sender: { sender_type: "user" },
          message: {
            message_id: "om_1",
            chat_id: "oc_1",
            chat_type: "p2p",
            message_type: "text",
            content: JSON.stringify({ text: "你好" }),
          },
        },
      }),
    });
    expect(res.status).toBe(200);
    await new Promise((r) => setTimeout(r, 50));
    const captured = await listEvents({ kind: "connector.feishu" });
    expect(captured).toHaveLength(1);
    expect(captured[0]!.payload["chatId"]).toBe("oc_1");

    // duplicate event_id is dropped
    await app.request("/feishu/events", {
      method: "POST",
      body: JSON.stringify({
        header: { event_id: "ev1", event_type: "im.message.receive_v1", token: "vt" },
        event: {},
      }),
    });
    await new Promise((r) => setTimeout(r, 30));
    expect(await listEvents({ kind: "connector.feishu" })).toHaveLength(1);
  });

  it("binding lookups route by chat/root and by work item", async () => {
    await createBinding({ channel: "feishu", kind: "main", chatId: "oc_9" });
    const wiBinding = await createBinding({
      channel: "feishu",
      kind: "work_item",
      workItemId: "wi_abc",
      chatId: "oc_9",
      rootId: "om_root",
    });
    expect((await findByChannelRef("feishu", "oc_9", "om_root"))?.workItemId).toBe("wi_abc");
    expect((await findByChannelRef("feishu", "oc_9", null))?.kind).toBe("main");
    expect((await findByWorkItem("feishu", "wi_abc"))?.id).toBe(wiBinding.id);
  });
});
