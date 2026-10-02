import { describe, expect, it } from "vitest";
import { ApiError, type ProviderPreset, type ProviderView, type Role, type RoleConfig } from "../src/lib/api.js";
import { routeFor } from "../src/lib/router.svelte.js";
import {
  compatibility,
  draftFromPreset,
  draftFromProvider,
  draftProblem,
  emptyDraft,
  errorText,
  parseModels,
  providerInput,
  providerPatch,
  roleCompatibility,
  rolesUsingProvider,
} from "../src/lib/settings.js";

const provider = (patch: Partial<ProviderView> = {}): ProviderView => ({
  id: "deepseek",
  label: "DeepSeek",
  anthropicBaseUrl: "",
  openaiBaseUrl: "",
  piProvider: "",
  models: [],
  hasApiKey: true,
  apiKeyHint: "…abcd",
  ...patch,
});

const roles = (patch: Partial<Record<Role, Partial<RoleConfig>>> = {}): Record<Role, RoleConfig> => {
  const base: RoleConfig = { agent: "claude", provider: "", model: "", effort: "low" };
  return {
    primary: { ...base, ...patch.primary },
    manager: { ...base, ...patch.manager },
    worker: { ...base, agent: "codex", ...patch.worker },
    distiller: { ...base, agent: "pi", ...patch.distiller },
  };
};

describe("compatibility", () => {
  it("always accepts the CLI's own login", () => {
    expect(compatibility("claude", "")).toBeNull();
    expect(compatibility("codex", "")).toBeNull();
    expect(compatibility("pi", "")).toBeNull();
  });

  it("requires the endpoint each CLI speaks", () => {
    const none = provider();
    expect(compatibility("claude", none)).toBe("settings.compat.claude");
    expect(compatibility("codex", none)).toBe("settings.compat.codex");
    expect(compatibility("pi", none)).toBe("settings.compat.pi");

    expect(compatibility("claude", provider({ anthropicBaseUrl: "https://a" }))).toBeNull();
    expect(compatibility("codex", provider({ openaiBaseUrl: "https://o" }))).toBeNull();
    expect(compatibility("pi", provider({ piProvider: "deepseek" }))).toBeNull();
    // Having the wrong endpoint is not enough.
    expect(compatibility("codex", provider({ anthropicBaseUrl: "https://a", piProvider: "x" }))).toBe("settings.compat.codex");
    expect(compatibility("claude", provider({ anthropicBaseUrl: "   " }))).toBe("settings.compat.claude");
  });

  it("resolves a role's provider id, flagging one that no longer exists", () => {
    const list = [provider({ piProvider: "deepseek" })];
    expect(roleCompatibility({ agent: "pi", provider: "deepseek" }, list)).toBeNull();
    expect(roleCompatibility({ agent: "claude", provider: "deepseek" }, list)).toBe("settings.compat.claude");
    expect(roleCompatibility({ agent: "claude", provider: "gone" }, list)).toBe("settings.compat.unknownProvider");
    expect(roleCompatibility({ agent: "claude", provider: "" }, [])).toBeNull();
  });
});

describe("rolesUsingProvider", () => {
  it("lists roles in their fixed order", () => {
    const assigned = roles({ worker: { provider: "deepseek" }, primary: { provider: "deepseek" }, manager: { provider: "kimi" } });
    expect(rolesUsingProvider(assigned, "deepseek")).toEqual(["primary", "worker"]);
    expect(rolesUsingProvider(assigned, "kimi")).toEqual(["manager"]);
    expect(rolesUsingProvider(assigned, "none")).toEqual([]);
  });
});

describe("provider drafts", () => {
  const preset: ProviderPreset = {
    id: "deepseek",
    label: "DeepSeek",
    anthropicBaseUrl: "https://api.deepseek.com/anthropic",
    openaiBaseUrl: "",
    piProvider: "deepseek",
    models: ["deepseek-chat", "deepseek-reasoner"],
    docsUrl: "https://api-docs.deepseek.com",
  };

  it("prefills from a preset and avoids taken ids", () => {
    const draft = draftFromPreset(preset);
    expect(draft).toMatchObject({
      id: "deepseek",
      label: "DeepSeek",
      anthropicBaseUrl: "https://api.deepseek.com/anthropic",
      piProvider: "deepseek",
      apiKey: "",
      clearKey: false,
      docsUrl: "https://api-docs.deepseek.com",
    });
    expect(parseModels(draft.models)).toEqual(preset.models);
    expect(draftFromPreset(preset, ["deepseek"]).id).toBe("deepseek-2");
    expect(draftFromPreset(preset, ["deepseek", "deepseek-2"]).id).toBe("deepseek-3");
  });

  it("parses comma- and newline-separated models", () => {
    expect(parseModels(" a, b\nc,,\n a \n")).toEqual(["a", "b", "c"]);
    expect(parseModels("")).toEqual([]);
  });

  it("builds a create body without an empty id", () => {
    const draft = { ...emptyDraft(), label: " Kimi ", piProvider: " moonshot ", apiKey: " sk-1 ", models: "kimi-k2" };
    expect(providerInput(draft)).toEqual({
      label: "Kimi",
      anthropicBaseUrl: "",
      openaiBaseUrl: "",
      piProvider: "moonshot",
      apiKey: "sk-1",
      models: ["kimi-k2"],
    });
    expect(providerInput({ ...draft, id: "kimi" }).id).toBe("kimi");
  });

  it("keeps the stored key unless a new one is typed or clear is chosen", () => {
    const draft = draftFromProvider(provider({ piProvider: "deepseek", models: ["m1", "m2"] }));
    expect(draft.apiKey).toBe("");
    expect(draft.models).toBe("m1\nm2");
    expect(providerPatch(draft)).not.toHaveProperty("apiKey");
    expect(providerPatch({ ...draft, apiKey: "sk-new" }).apiKey).toBe("sk-new");
    expect(providerPatch({ ...draft, apiKey: "sk-new", clearKey: true }).apiKey).toBe("");
    expect(providerPatch(draft)).toMatchObject({ label: "DeepSeek", piProvider: "deepseek", models: ["m1", "m2"] });
  });

  it("names what is missing before a provider can be saved", () => {
    expect(draftProblem(emptyDraft())).toBe("settings.providers.needLabel");
    expect(draftProblem({ ...emptyDraft(), label: "x" })).toBe("settings.providers.needEndpoint");
    expect(draftProblem({ ...emptyDraft(), label: "x", openaiBaseUrl: "https://o" })).toBeNull();
  });
});

describe("errorText", () => {
  it("prefers the server's error field", () => {
    expect(errorText(new ApiError(409, '409 {"ok":false,"error":"provider is used by worker"}'))).toBe(
      "provider is used by worker",
    );
    expect(errorText(new ApiError(500, "500 boom"))).toBe("boom");
    expect(errorText(new ApiError(0, "Failed to fetch"))).toBe("Failed to fetch");
    expect(errorText(new Error("plain"))).toBe("plain");
  });

  it("names a host action refused outside the desktop app", () => {
    expect(errorText(new ApiError(404, '404 {"ok":false,"error":"desktop app only"}'))).toBe("只有桌面应用能做这件事");
  });
});

describe("settings route", () => {
  it("opens settings at General and maps each section", () => {
    expect(routeFor("/settings")).toEqual({ name: "redirect", to: "/settings/general" });
    expect(routeFor("/settings/")).toEqual({ name: "redirect", to: "/settings/general" });
    expect(routeFor("/settings/roles")).toEqual({ name: "settings", section: "roles" });
    expect(routeFor("/settings/nope")).toEqual({ name: "redirect", to: "/settings/general" });
  });

  it("sends the pages that moved into settings to their section", () => {
    expect(routeFor("/policies")).toEqual({ name: "redirect", to: "/settings/rules" });
    expect(routeFor("/status")).toEqual({ name: "redirect", to: "/settings/status" });
    expect(routeFor("/events")).toEqual({ name: "redirect", to: "/settings/events" });
  });
});
