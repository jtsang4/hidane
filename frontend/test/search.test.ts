import { describe, expect, it } from "vitest";
import type { HidaneEvent } from "../src/lib/api.js";
import { matchesQuery } from "../src/lib/search.js";

const event = {
  seq: 42,
  id: "ev_1",
  ts: "2026-08-23T00:00:00.000Z",
  source: "connector:feishu",
  kind: "connector.feishu",
  threadId: "main",
  workItemId: "wi_abc",
  executionId: null,
  payload: { text: "讲解一下这张图片的内容", imageCount: 1 },
} as HidaneEvent;

describe("matchesQuery", () => {
  it("matches payload content, not just indexed columns", () => {
    expect(matchesQuery(event, "图片")).toBe(true);
    expect(matchesQuery(event, "imageCount")).toBe(true);
  });

  it("matches kind, source, seq and work item", () => {
    expect(matchesQuery(event, "feishu")).toBe(true);
    expect(matchesQuery(event, "42")).toBe(true);
    expect(matchesQuery(event, "wi_abc")).toBe(true);
  });

  it("is case-insensitive and ANDs every term", () => {
    expect(matchesQuery(event, "FEISHU 图片")).toBe(true);
    expect(matchesQuery(event, "feishu nonexistent")).toBe(false);
  });

  it("an empty query matches everything", () => {
    expect(matchesQuery(event, "   ")).toBe(true);
  });
});
