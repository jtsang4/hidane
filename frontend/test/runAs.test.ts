import { describe, expect, it } from "vitest";
import type { AgentCatalog } from "../src/lib/api.js";
import { effortOptions, loadDraftRunAs, normalizeRunAs, runAsSummary, sameRunAs, saveDraftRunAs, withAgent } from "../src/lib/runAs.js";

function memoryStorage(): Storage {
  const data = new Map<string, string>();
  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => void data.set(key, value),
    removeItem: (key) => void data.delete(key),
    clear: () => data.clear(),
    key: () => null,
    get length() {
      return data.size;
    },
  };
}

const codex: AgentCatalog = {
  agent: "codex",
  source: "cli",
  efforts: ["minimal", "low", "medium", "high", "xhigh", "max", "ultra"],
  models: [{ id: "gpt-fake-mini", label: "Mini", efforts: ["low", "medium"], defaultEffort: "low" }],
};

describe("run-as choice", () => {
  it("remembers the choice for new tasks, and forgets it when the settings are followed again", () => {
    const storage = memoryStorage();
    saveDraftRunAs({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "high" }, storage);
    expect(loadDraftRunAs(storage)).toEqual({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "high" });
    saveDraftRunAs(null, storage);
    expect(loadDraftRunAs(storage)).toBeNull();
  });

  it("drops what the CLI cannot take instead of sending it", () => {
    expect(normalizeRunAs({ agent: "claude", effort: "ultra" })).toEqual({ agent: "claude", provider: "", model: "", effort: "" });
    expect(normalizeRunAs({ agent: "gpt" as never })).toBeNull();
    expect(loadDraftRunAs({ getItem: () => "not json" })).toBeNull();
  });

  it("switching agent starts from that CLI's defaults", () => {
    expect(withAgent("pi")).toEqual({ agent: "pi", provider: "", model: "", effort: "" });
    expect(withAgent("")).toBeNull();
  });

  it("offers the efforts the chosen model takes, else every effort the CLI accepts", () => {
    expect(effortOptions({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "" }, codex)).toEqual(["", "low", "medium"]);
    expect(effortOptions({ agent: "codex", provider: "", model: "other", effort: "" }, codex)).toContain("ultra");
    expect(effortOptions({ agent: "pi", provider: "", model: "", effort: "" }, undefined)).toContain("off");
    // A value chosen elsewhere stays visible.
    expect(effortOptions({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "xhigh" }, codex)).toContain("xhigh");
  });

  it("says what a task runs on in one line", () => {
    const label = (effort: string) => ({ high: "高" })[effort] ?? effort;
    expect(runAsSummary({ agent: "codex", provider: "", model: "gpt-5.5", effort: "high" }, label, "默认模型")).toBe("Codex · gpt-5.5 · 高");
    expect(runAsSummary({ agent: "claude", provider: "", model: "", effort: "" }, label, "默认模型")).toBe("Claude Code · 默认模型");
    expect(sameRunAs(null, undefined)).toBe(true);
    expect(sameRunAs({ agent: "pi", provider: "", model: "", effort: "" }, { agent: "pi", provider: "", model: "", effort: "off" })).toBe(false);
  });
});
