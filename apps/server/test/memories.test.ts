import { describe, expect, it } from "vitest";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  appendMemory,
  parseMemories,
  promoteToFile,
  readMemoryFile,
} from "../src/kernel/memories.js";
import { meaningfulEvents } from "../src/agents/distiller.js";
import { listEvents, type HidaneEvent } from "../src/kernel/events.js";

async function tmpMemoryPath(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "hidane-mem-"));
  return join(dir, "MEMORY.md");
}

describe("layered memory files", () => {
  it("creates the file with header and kind section on first append", async () => {
    const path = await tmpMemoryPath();
    const entry = await appendMemory(path, "global", {
      kind: "preference",
      content: "部署端口使用非常见端口",
    });
    const text = await readFile(path, "utf8");
    expect(text).toContain("# hidane memory (global)");
    expect(text).toContain("## preference");
    expect(text).toContain("部署端口使用非常见端口");
    expect(text).toContain(entry.id);
  });

  it("appends into the existing section, not a duplicate section", async () => {
    const path = await tmpMemoryPath();
    await appendMemory(path, "global", { kind: "fact", content: "one" });
    await appendMemory(path, "global", { kind: "decision", content: "two" });
    await appendMemory(path, "global", { kind: "fact", content: "three" });
    const text = await readFile(path, "utf8");
    expect(text.match(/## fact/g)).toHaveLength(1);
    const parsed = parseMemories(text);
    expect(parsed.map((m) => `${m.kind}:${m.content}`)).toEqual([
      "fact:one",
      "fact:three",
      "decision:two",
    ]);
  });

  it("round-trips entries through parseMemories", async () => {
    const path = await tmpMemoryPath();
    await appendMemory(path, "global", { kind: "lesson", content: "外部 loader 必须 reload" });
    const parsed = parseMemories(await readMemoryFile(path));
    expect(parsed).toHaveLength(1);
    expect(parsed[0]!.kind).toBe("lesson");
    expect(parsed[0]!.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it("promoteToFile writes the file and records memory.promoted to the log", async () => {
    const dir = await mkdtemp(join(tmpdir(), "hidane-ws-"));
    const saved = await promoteToFile(
      { kind: "decision", content: "验收用 Agent 驱动" },
      "work_item",
      { workspace: dir, workItemId: "wi_test1" },
      "agent:distiller",
    );
    const text = await readFile(join(dir, "MEMORY.md"), "utf8");
    expect(text).toContain("验收用 Agent 驱动");
    const events = await listEvents({ kind: "memory.promoted" });
    expect(events).toHaveLength(1);
    expect(events[0]!.payload["memoryId"]).toBe(saved.id);
    expect(events[0]!.workItemId).toBe("wi_test1");
  });
});

describe("distiller filtering", () => {
  it("keeps durable-material kinds and drops connector noise", () => {
    let seq = 0;
    const ev = (kind: string, source = "s"): HidaneEvent => ({
      seq: ++seq,
      id: `ev_${seq}`,
      ts: new Date().toISOString(),
      source,
      kind,
      threadId: null,
      workItemId: null,
      executionId: null,
      payload: {},
    });
    const kept = meaningfulEvents([
      ev("user.message"),
      ev("connector.heartbeat"),
      ev("triage.decision"),
      ev("agent.reply"),
      ev("side_effect.intent"),
      ev("execution.finished"),
      ev("route.decision"),
    ]);
    expect(kept.map((e) => e.kind)).toEqual([
      "user.message",
      "agent.reply",
      "execution.finished",
    ]);
  });
});
