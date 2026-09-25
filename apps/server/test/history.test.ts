import { beforeEach, describe, expect, it, vi } from "vitest";

const stubs = vi.hoisted(() => ({
  promptRole: vi.fn(),
}));

vi.mock("../src/agents/sdk.js", () => ({
  openPrimarySession: vi.fn(async () => ({ dispose: vi.fn() })),
  getManagerSession: vi.fn(async () => ({})),
  promptRole: stubs.promptRole,
}));
vi.mock("../src/agents/distiller.js", () => ({
  recallForPrimary: vi.fn(async () => ""),
  recallForManager: vi.fn(async () => ""),
}));

import { buildApp } from "../src/connectors/http.js";
import { appendEvent, getEvent, listEvents, type HidaneEvent } from "../src/kernel/events.js";
import { pendingMessages } from "../src/kernel/mailbox.js";
import { createWorkItem } from "../src/kernel/workItems.js";
import { Runtime } from "../src/kernel/runtime.js";
import { primaryTurn } from "../src/agents/primary.js";
import { redactMessage, submitMessage } from "../src/agents/ingress.js";
import { renderDay, today } from "../src/projections/worklog.js";
import {
  conversationDays,
  recentConversation,
  searchConversation,
} from "../src/projections/conversation.js";

function answer(effects: unknown[]) {
  stubs.promptRole.mockResolvedValueOnce({ ok: true, text: JSON.stringify({ effects }), durationMs: 1 });
}

async function say(text: string) {
  return submitMessage({ text, source: "connector:web" });
}

async function reply(to: HidaneEvent, text: string, extra: Record<string, unknown> = {}) {
  return appendEvent({
    source: "agent:primary",
    kind: "agent.reply",
    threadId: "main",
    payload: { text, of: to.id, root: to.id, ...extra },
  });
}

async function getJson<T>(path: string): Promise<{ status: number; body: T }> {
  const res = await buildApp().request(path);
  return { status: res.status, body: (await res.json()) as T };
}

describe("hiding a message", () => {
  it("masks the message and every forwarded copy for every reader, without rewriting the row", async () => {
    const item = await createWorkItem("部署", "connector:web");
    const message = await submitMessage({ text: "密码是 hunter2", source: "connector:web", target: item.id });
    const copies = await listEvents({ threadId: item.threadId, kind: "user.message" });
    expect(copies[0]?.payload["text"]).toBe("密码是 hunter2");

    const res = await buildApp().request(`/api/messages/${message.id}/redact`, { method: "POST" });
    expect(res.status).toBe(200);

    const read = await getEvent(message.id);
    expect(read?.payload["redacted"]).toBe(true);
    expect(read?.payload["text"]).toBe("");
    // Structure survives so the conversation still groups.
    expect(read?.payload["target"]).toBe(item.id);
    const copy = (await listEvents({ threadId: item.threadId, kind: "user.message" }))[0];
    expect(copy?.payload["redacted"]).toBe(true);
    expect(copy?.payload["root"]).toBe(message.id);

    const { body } = await getJson<{ events: HidaneEvent[] }>("/api/events?page=1&conversation=1");
    expect(JSON.stringify(body.events)).not.toContain("hunter2");
    expect(body.events.some((e) => e.kind === "message.redacted" && e.payload["of"] === message.id)).toBe(true);
    expect(await renderDay(today())).not.toContain("hunter2");
    expect((await searchConversation({ query: "hunter2" })).events).toHaveLength(0);

    // The log keeps the original: hiding is a view, not a rewrite.
    const { sql } = await import("../src/kernel/db.js");
    const [raw] = await sql()`SELECT payload->>'text' AS text FROM events WHERE id = ${message.id}`;
    expect(raw?.["text"]).toBe("密码是 hunter2");
  });

  it("records once, and only for the person's own main-thread messages", async () => {
    const message = await say("oops");
    expect(await redactMessage(message.id, "connector:web")).not.toBeNull();
    expect(await redactMessage(message.id, "connector:web")).toBeNull();
    expect(await listEvents({ kind: "message.redacted" })).toHaveLength(1);

    const answer = await reply(message, "ok");
    const res = await buildApp().request(`/api/messages/${answer.id}/redact`, { method: "POST" });
    expect(res.status).toBe(404);
    expect((await buildApp().request(`/api/messages/ev_nope/redact`, { method: "POST" })).status).toBe(404);
  });
});

describe("conversation history paging", () => {
  it("opens a window around any event and walks both ways from it", async () => {
    const said: HidaneEvent[] = [];
    for (let i = 0; i < 10; i++) said.push(await say(`m${i}`));
    const target = said[5]!;
    const { body } = await getJson<{
      events: HidaneEvent[];
      hasMore: boolean;
      hasNewer: boolean;
      newestSeq: number;
    }>(`/api/events?page=1&conversation=1&around=${target.id}&limit=4`);
    expect(body.events.map((e) => e.payload["text"])).toEqual(["m3", "m4", "m5", "m6"]);
    expect(body.hasMore).toBe(true);
    expect(body.hasNewer).toBe(true);

    const next = await getJson<{ events: HidaneEvent[]; hasNewer: boolean }>(
      `/api/events?page=1&conversation=1&after=${body.newestSeq}&limit=4`,
    );
    expect(next.body.events.map((e) => e.payload["text"])).toEqual(["m7", "m8", "m9"]);
    expect(next.body.hasNewer).toBe(false);

    expect((await getJson(`/api/events?page=1&conversation=1&around=ev_missing`)).status).toBe(404);
    expect((await getJson(`/api/events?page=1&conversation=1&after=abc`)).status).toBe(400);
  });

  it("names every work item a page mentions, however long ago it closed", async () => {
    const item = await createWorkItem("很久以前的任务", "connector:web");
    const message = await say("hi");
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "done", root: message.id },
    });
    const { body } = await getJson<{ titles: Record<string, string> }>("/api/events?page=1&conversation=1");
    expect(body.titles[item.id]).toBe("很久以前的任务");
  });

  it("can leave out answers to webhooks and schedules", async () => {
    const message = await say("mine");
    await reply(message, "for you");
    await appendEvent({
      source: "agent:primary",
      kind: "agent.reply",
      threadId: "main",
      payload: { text: "webhook says hi", root: "ev_ext", rootKind: "external", rootText: "push" },
    });
    const all = await getJson<{ events: HidaneEvent[] }>("/api/events?page=1&conversation=1");
    const mine = await getJson<{ events: HidaneEvent[] }>("/api/events?page=1&conversation=1&origin=person");
    expect(all.body.events).toHaveLength(3);
    expect(mine.body.events.map((e) => e.payload["text"])).toEqual(["mine", "for you"]);
  });
});

describe("conversation search", () => {
  it("matches every term over the whole history, newest first, and pages", async () => {
    const old = await say("部署 staging 服务器的方案");
    await reply(old, "方案：先部署 staging，再上 prod");
    for (let i = 0; i < 5; i++) await say(`无关 ${i}`);
    await say("prod 部署完成了吗");

    const hits = await searchConversation({ query: "部署 staging" });
    expect(hits.events.map((e) => e.payload["text"])).toEqual([
      "方案：先部署 staging，再上 prod",
      "部署 staging 服务器的方案",
    ]);

    const first = await getJson<{ events: HidaneEvent[]; hasMore: boolean }>(
      `/api/conversation/search?q=${encodeURIComponent("部署")}&limit=2`,
    );
    expect(first.body.events).toHaveLength(2);
    expect(first.body.hasMore).toBe(true);
    const second = await getJson<{ events: HidaneEvent[]; hasMore: boolean }>(
      `/api/conversation/search?q=${encodeURIComponent("部署")}&limit=2&before=${first.body.events.at(-1)!.seq}`,
    );
    expect(second.body.events.map((e) => e.id)).toEqual([old.id]);
    expect(second.body.hasMore).toBe(false);
    expect((await getJson(`/api/conversation/search?q=`)).status).toBe(400);
  });

  it("treats LIKE wildcards literally and skips what the conversation never shows", async () => {
    const m = await say("进度 100% 完成");
    await say("进度 1000 完成");
    const item = await createWorkItem("子任务", "connector:web");
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "子任务 100% 汇报", root: m.id, child: true },
    });
    // A thread copy without a root is an old runtime's duplicate.
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "旧副本 100%" },
    });
    const hits = await searchConversation({ query: "100%" });
    expect(hits.events.map((e) => e.payload["text"])).toEqual(["进度 100% 完成"]);
  });

  it("finds work items of any status by title", async () => {
    const item = await createWorkItem("迁移数据库", "connector:web");
    const { sql } = await import("../src/kernel/db.js");
    await sql()`UPDATE work_items SET status = 'closed' WHERE id = ${item.id}`;
    const { body } = await getJson<{ items: { id: string }[] }>(
      `/api/conversation/search?q=${encodeURIComponent("迁移")}`,
    );
    expect(body.items.map((i) => i.id)).toEqual([item.id]);
  });
});

describe("conversation days", () => {
  it("indexes days in the reader's timezone, with where each day starts", async () => {
    const { sql } = await import("../src/kernel/db.js");
    const a = await say("late evening UTC");
    const b = await say("next morning");
    await sql()`UPDATE events SET ts = '2026-03-01T20:00:00Z' WHERE id = ${a.id}`;
    await sql()`UPDATE events SET ts = '2026-03-02T01:00:00Z' WHERE id = ${b.id}`;
    // 20:00Z is already March 2nd in Shanghai: one day, not two.
    expect(await conversationDays("Asia/Shanghai")).toEqual([{ day: "2026-03-02", count: 2, firstId: a.id }]);
    expect((await conversationDays("UTC")).map((d) => d.day)).toEqual(["2026-03-02", "2026-03-01"]);
    // An unknown zone is not an injection vector or an error — it is UTC.
    expect((await conversationDays("'; DROP TABLE events; --")).map((d) => d.day)).toEqual([
      "2026-03-02",
      "2026-03-01",
    ]);
  });
});

describe("the Primary's view of the conversation", () => {
  beforeEach(() => {
    stubs.promptRole.mockReset();
  });

  it("is bounded, leaves out hidden messages, and says where it begins", async () => {
    const said: HidaneEvent[] = [];
    for (let i = 0; i < 6; i++) {
      const m = await say(`话题 ${i}`);
      await reply(m, `回答 ${i}`);
      said.push(m);
    }
    await redactMessage(said[5]!.id, "connector:web");
    const recent = await recentConversation({ maxTurns: 3 });
    expect(recent.turns).toBe(3);
    expect(recent.fromId).toBe(said[2]!.id);
    expect(recent.text).toContain("话题 4");
    expect(recent.text).not.toContain("话题 5");
    expect(recent.text).not.toContain("话题 1");

    const { body } = await getJson<{ fromId: string }>("/api/conversation/context");
    expect(body.fromId).toBe((await recentConversation()).fromId);
  });

  it("reads the recent conversation each turn and can look further back without waiting", async () => {
    const early = await say("我们定的发布代号是 aurora");
    await reply(early, "记下了：aurora");
    // Drain the first message so it is history, not this turn's input.
    answer([{ type: "reply", of: early.id, reply: "好" }]);
    const runtime = new Runtime();
    runtime.register("primary", primaryTurn);
    await runtime.drain();

    const question = await say("发布代号是什么来着？");
    // First turn: recall. The Primary does not wait for the search's answer.
    answer([{ type: "recall", of: question.id, query: "代号" }]);
    await primaryTurn("primary", await pendingMessages("primary"));
    const firstPrompt = String(stubs.promptRole.mock.lastCall![1]);
    expect(firstPrompt).toContain("Recent conversation");
    expect(firstPrompt).toContain("aurora");
    expect(await listEvents({ kind: "agent.reply", payloadEquals: { key: "root", value: question.id } })).toHaveLength(0);

    const recalled = (await listEvents({ kind: "conversation.recalled" }))[0]!;
    expect(recalled.mailbox).toBe("primary");
    expect(recalled.payload["root"]).toBe(question.id);
    expect(String(recalled.payload["text"])).toContain("aurora");

    // Next turn answers the original message, and never recalls twice.
    answer([
      { type: "recall", of: recalled.id, query: "again" },
      { type: "reply", of: recalled.id, reply: "是 aurora" },
    ]);
    await primaryTurn("primary", [recalled]);
    const replies = await listEvents({ kind: "agent.reply", payloadEquals: { key: "root", value: question.id } });
    expect(replies.map((r) => r.payload["text"])).toEqual(["是 aurora"]);
    expect(await listEvents({ kind: "conversation.recalled" })).toHaveLength(1);
  });
});
