import { beforeEach, describe, expect, it, vi } from "vitest";

const stubs = vi.hoisted(() => ({
  promptRole: vi.fn(),
  handleThreadMessage: vi.fn(async () => "manager answer"),
  hasActiveWorker: vi.fn(() => false),
  activeExecutionId: vi.fn(() => undefined as string | undefined),
  cancelActiveWorker: vi.fn(async () => true),
}));

vi.mock("../src/agents/sdk.js", () => ({
  getPrimarySession: vi.fn(async () => ({})),
  promptRole: stubs.promptRole,
}));
vi.mock("../src/agents/distiller.js", () => ({
  recallForPrimary: vi.fn(async () => ""),
}));
vi.mock("../src/agents/manager.js", () => ({
  handleThreadMessage: stubs.handleThreadMessage,
}));
vi.mock("../src/agents/rpcWorker.js", () => ({
  hasActiveWorker: stubs.hasActiveWorker,
  activeExecutionId: stubs.activeExecutionId,
  cancelActiveWorker: stubs.cancelActiveWorker,
}));

import { handleUserMessage } from "../src/agents/primary.js";
import { listEvents } from "../src/kernel/events.js";
import { createWorkItem, getWorkItem, listWorkItems, setWorkItemStatus } from "../src/kernel/workItems.js";

describe("primary routed replies", () => {
  beforeEach(() => {
    stubs.promptRole.mockReset();
    stubs.handleThreadMessage.mockReset();
    stubs.handleThreadMessage.mockResolvedValue("manager answer");
    stubs.hasActiveWorker.mockReset();
    stubs.hasActiveWorker.mockReturnValue(false);
    stubs.activeExecutionId.mockReset();
    stubs.activeExecutionId.mockReturnValue(undefined);
    stubs.cancelActiveWorker.mockReset();
    stubs.cancelActiveWorker.mockResolvedValue(true);
  });

  it("mirrors a new work-item answer to the main thread", async () => {
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "new_work_item",
        title: "Answer a question",
        brief: "Answer the question",
      }),
      durationMs: 1,
    });

    const outcome = await handleUserMessage("question", "connector:web");
    expect(outcome.reply).toBe("manager answer");

    const replies = await listEvents({ threadId: "main", kind: "agent.reply" });
    expect(replies).toHaveLength(1);
    expect(replies[0]?.payload["text"]).toBe("manager answer");
    expect(replies[0]?.workItemId).toBe(outcome.workItemId);
  });

  it("mirrors an existing work-item answer to the main thread", async () => {
    const item = await createWorkItem("Existing question");
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "route_to_work_item",
        work_item_id: item.id,
        message: "Follow up",
      }),
      durationMs: 1,
    });

    await handleUserMessage("follow up", "connector:web");

    const replies = await listEvents({ threadId: "main", kind: "agent.reply" });
    expect(replies).toHaveLength(1);
    expect(replies[0]?.payload["text"]).toBe("manager answer");
    expect(replies[0]?.workItemId).toBe(item.id);
  });

  it("lets the Primary close all open work items as a direct state operation", async () => {
    const first = await createWorkItem("First open item");
    const second = await createWorkItem("Second open item");
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "set_work_item_status",
        all_open: true,
        status: "closed",
      }),
      durationMs: 1,
    });

    const outcome = await handleUserMessage("把所有已有工作项关停", "connector:web");

    expect(outcome.action).toBe("set_work_item_status");
    expect(outcome.workItemIds).toEqual([first.id, second.id]);
    expect(await listWorkItems("open")).toHaveLength(0);
    expect((await getWorkItem(first.id)).status).toBe("closed");
    expect((await getWorkItem(second.id)).status).toBe("closed");

    const changes = await listEvents({ kind: "work_item.status_changed" });
    expect(changes).toHaveLength(2);
    expect(changes.every((event) => event.source === "agent:primary")).toBe(true);
    const replies = await listEvents({ threadId: "main", kind: "agent.reply" });
    expect(replies[0]?.payload["text"]).toContain("2 个工作项");
  });

  it("can reopen an explicitly selected non-open work item", async () => {
    const item = await createWorkItem("Already done");
    await setWorkItemStatus(item.id, "done", "test");
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "set_work_item_status",
        work_item_ids: [item.id],
        status: "open",
      }),
      durationMs: 1,
    });

    const outcome = await handleUserMessage("把它重新打开", "connector:web");

    expect(outcome.action).toBe("set_work_item_status");
    expect((await getWorkItem(item.id)).status).toBe("open");
    expect(await listEvents({ kind: "work_item.status_changed" })).toHaveLength(2);
  });

  it("creates a deferred work item without starting Manager/Worker", async () => {
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "new_work_item",
        title: "Deferred item",
        brief: "等我确认后再开始",
        dispatch: false,
      }),
      durationMs: 1,
    });

    const outcome = await handleUserMessage("先记录这个工作项，不要开始", "connector:web");

    expect(outcome.action).toBe("new_work_item");
    expect(outcome.workItemId).toBeDefined();
    expect(stubs.handleThreadMessage).not.toHaveBeenCalled();
    const messages = await listEvents({ workItemId: outcome.workItemId, kind: "user.message" });
    expect(messages[0]?.payload).toMatchObject({ deferred: true, text: "等我确认后再开始" });
  });

  it("cancels explicitly selected running executions without routing to a Manager", async () => {
    const item = await createWorkItem("Long-running item");
    stubs.hasActiveWorker.mockImplementation((id: string) => id === item.id);
    stubs.activeExecutionId.mockReturnValue("ex_running");
    stubs.promptRole.mockResolvedValue({
      ok: true,
      text: JSON.stringify({
        action: "cancel_work_item_execution",
        work_item_ids: [item.id],
      }),
      durationMs: 1,
    });

    const outcome = await handleUserMessage("停止这个正在运行的任务", "connector:web");

    expect(outcome.action).toBe("cancel_work_item_execution");
    expect(outcome.workItemIds).toEqual([item.id]);
    expect(stubs.cancelActiveWorker).toHaveBeenCalledWith(item.id);
    expect(stubs.handleThreadMessage).not.toHaveBeenCalled();
    const cancelled = await listEvents({ kind: "execution.cancelled" });
    expect(cancelled).toHaveLength(1);
    expect(cancelled[0]).toMatchObject({
      source: "agent:primary",
      workItemId: item.id,
      executionId: "ex_running",
    });
  });
});
