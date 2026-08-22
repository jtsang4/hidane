import { describe, expect, it } from "vitest";
import { readFile } from "node:fs/promises";
import { appendEvent } from "../src/kernel/events.js";
import { createWorkItem } from "../src/kernel/workItems.js";
import { renderDay, writeDay, today } from "../src/projections/worklog.js";

describe("worklog projection", () => {
  it("groups events by main thread and work item threads", async () => {
    await appendEvent({
      source: "connector:cli",
      kind: "user.message",
      threadId: "main",
      payload: { text: "hello main" },
    });
    const item = await createWorkItem("Projection goal");
    await appendEvent({
      source: "agent:manager",
      kind: "agent.reply",
      threadId: item.threadId,
      workItemId: item.id,
      payload: { text: "done deal" },
    });

    const md = await renderDay(today());
    expect(md).toContain("## Main thread");
    expect(md).toContain("hello main");
    expect(md).toContain(`## ${item.id} — Projection goal`);
    expect(md).toContain("done deal");
  });

  it("writes the projection file under worklogs/YYYY/MM/", async () => {
    await appendEvent({
      source: "connector:cli",
      kind: "user.message",
      threadId: "main",
      payload: { text: "file check" },
    });
    const path = await writeDay(today());
    expect(path).toMatch(/worklogs\/\d{4}\/\d{2}\/\d{4}-\d{2}-\d{2}\.md$/);
    const content = await readFile(path, "utf8");
    expect(content).toContain("file check");
  });
});
