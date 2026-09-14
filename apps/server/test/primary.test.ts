import { beforeEach, describe, expect, it, vi } from "vitest";

const stubs = vi.hoisted(() => ({
  promptRole: vi.fn(),
  handleThreadMessage: vi.fn(async () => "manager answer"),
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

import { handleUserMessage } from "../src/agents/primary.js";
import { listEvents } from "../src/kernel/events.js";
import { createWorkItem } from "../src/kernel/workItems.js";

describe("primary routed replies", () => {
  beforeEach(() => {
    stubs.promptRole.mockReset();
    stubs.handleThreadMessage.mockReset();
    stubs.handleThreadMessage.mockResolvedValue("manager answer");
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
});
