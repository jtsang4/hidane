import { describe, expect, it } from "vitest";
import type { AgentCatalog } from "../src/lib/api.js";
import {
  effortOptions,
  favoriteLabel,
  fitEffort,
  loadDraftRunAs,
  loadFavorites,
  MAX_FAVORITES,
  normalizeRunAs,
  runAsSummary,
  sameRunAs,
  saveDraftRunAs,
  saveFavorites,
  toggleFavorite,
  withAgent,
} from "../src/lib/runAs.js";

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

  it("drops an effort the newly chosen model or CLI does not take, back to the default", () => {
    // The catalog knows the model: only its own efforts are kept.
    expect(fitEffort({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "ultra" }, codex)).toBe("");
    expect(fitEffort({ agent: "codex", provider: "", model: "gpt-fake-mini", effort: "medium" }, codex)).toBe("medium");
    // A model the catalog does not list, or one behind a provider, is held to the CLI's efforts.
    expect(fitEffort({ agent: "codex", provider: "", model: "other", effort: "ultra" }, codex)).toBe("ultra");
    expect(fitEffort({ agent: "codex", provider: "deepseek", model: "gpt-fake-mini", effort: "ultra" }, codex)).toBe("ultra");
    expect(fitEffort({ agent: "claude", provider: "", model: "", effort: "ultra" }, undefined)).toBe("");
  });

  it("says what a task runs on in one line", () => {
    const label = (effort: string) => ({ high: "高" })[effort] ?? effort;
    expect(runAsSummary({ agent: "codex", provider: "", model: "gpt-5.5", effort: "high" }, label, "默认模型")).toBe("Codex · gpt-5.5 · 高");
    expect(runAsSummary({ agent: "claude", provider: "", model: "", effort: "" }, label, "默认模型")).toBe("Claude Code · 默认模型");
    expect(sameRunAs(null, undefined)).toBe(true);
    expect(sameRunAs({ agent: "pi", provider: "", model: "", effort: "" }, { agent: "pi", provider: "", model: "", effort: "off" })).toBe(false);
  });

  it("keeps favorites as whole combinations: newest first, toggled off by the same click", () => {
    const storage = memoryStorage();
    const codex = { agent: "codex", provider: "", model: "gpt-fake-mini", effort: "medium" } as const;
    const pi = { agent: "pi", provider: "deepseek", model: "deepseek-v4", effort: "high" } as const;
    let list = toggleFavorite([], codex);
    list = toggleFavorite(list, pi);
    expect(list).toEqual([pi, codex]);
    saveFavorites(list, storage);
    expect(loadFavorites(storage)).toEqual([pi, codex]);
    // The effort is part of the combination: a different one is a different favorite.
    expect(toggleFavorite(list, { ...codex, effort: "low" })).toHaveLength(3);
    expect(toggleFavorite(list, codex)).toEqual([pi]);
  });

  it("reads back only favorites the CLIs can take, once each, and never more than the cap", () => {
    const storage = memoryStorage();
    const many = Array.from({ length: MAX_FAVORITES + 3 }, (_, n) => ({ agent: "claude", provider: "", model: `m${n}`, effort: "" }));
    storage.setItem(
      "hidane.runAsFavorites",
      JSON.stringify([{ agent: "gpt" }, { agent: "claude", model: "m0", effort: "ultra" }, { agent: "claude", model: "m0" }, ...many]),
    );
    const list = loadFavorites(storage);
    expect(list[0]).toEqual({ agent: "claude", provider: "", model: "m0", effort: "" });
    expect(list.filter((entry) => entry.model === "m0")).toHaveLength(1);
    expect(list).toHaveLength(MAX_FAVORITES);
    expect(loadFavorites({ getItem: () => "{}" })).toEqual([]);
    expect(loadFavorites({ getItem: () => "not json" })).toEqual([]);
  });

  it("names a favorite's provider, unlike the one-line summary", () => {
    const label = (effort: string) => ({ high: "高" })[effort] ?? effort;
    const provider = (id: string) => ({ deepseek: "DeepSeek" })[id] ?? id;
    expect(favoriteLabel({ agent: "pi", provider: "deepseek", model: "deepseek-v4", effort: "high" }, provider, label, "默认模型")).toBe("pi · DeepSeek · deepseek-v4 · 高");
    expect(favoriteLabel({ agent: "codex", provider: "", model: "", effort: "" }, provider, label, "默认模型")).toBe("Codex · 默认模型");
  });
});
