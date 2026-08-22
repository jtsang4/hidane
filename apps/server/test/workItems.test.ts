import { describe, expect, it } from "vitest";
import { stat } from "node:fs/promises";
import {
  createWorkItem,
  getWorkItem,
  listWorkItems,
  setWorkItemStatus,
} from "../src/kernel/workItems.js";
import { listEvents } from "../src/kernel/events.js";

describe("work items", () => {
  it("creates work item with thread, workspace dir and creation event", async () => {
    const item = await createWorkItem("Test goal");
    expect(item.id).toMatch(/^wi_/);
    expect(item.threadId).toMatch(/^th_/);
    expect(item.status).toBe("open");

    const dir = await stat(item.workspace);
    expect(dir.isDirectory()).toBe(true);

    const created = await listEvents({ kind: "work_item.created" });
    expect(created).toHaveLength(1);
    expect(created[0]!.workItemId).toBe(item.id);
    expect(created[0]!.threadId).toBe(item.threadId);
  });

  it("status transitions are recorded as events", async () => {
    const item = await createWorkItem("Another goal");
    await setWorkItemStatus(item.id, "done");
    const updated = await getWorkItem(item.id);
    expect(updated.status).toBe("done");

    const changes = await listEvents({ kind: "work_item.status_changed" });
    expect(changes).toHaveLength(1);
    expect(changes[0]!.payload["to"]).toBe("done");

    const open = await listWorkItems("open");
    expect(open.find((i) => i.id === item.id)).toBeUndefined();
  });
});
