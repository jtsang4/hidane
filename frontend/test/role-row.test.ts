import { fireEvent, render, screen } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import i18n from "../src/i18n/index.js";
import { zh } from "../src/i18n/resources.js";
import type { AgentTestResult, ProviderView, RoleConfig } from "../src/lib/api.js";
import RoleRow from "../src/components/settings/RoleRow.svelte";
import { choose } from "./choose.js";

const text = zh.translation.settings;

const deepseek: ProviderView = {
  id: "deepseek",
  label: "DeepSeek",
  anthropicBaseUrl: "https://api.deepseek.com/anthropic",
  openaiBaseUrl: "",
  piProvider: "deepseek",
  models: ["deepseek-chat"],
  hasApiKey: true,
  apiKeyHint: "…abcd",
};

const saved: RoleConfig = { agent: "pi", provider: "deepseek", model: "deepseek-chat", effort: "low" };

function renderRow(overrides: { onsave?: (config: RoleConfig) => Promise<unknown>; ontest?: () => Promise<AgentTestResult> } = {}) {
  const onsave = overrides.onsave ?? vi.fn(() => Promise.resolve());
  const ontest = overrides.ontest ?? vi.fn(() => Promise.resolve({ ok: true, text: "pong", error: "", durationMs: 1234, agent: "pi", model: "deepseek-chat" }));
  render(RoleRow, { props: { role: "distiller", config: saved, providers: [deepseek], onsave, ontest } });
  return { onsave, ontest };
}

const testButton = () => screen.getByRole("button", { name: text.roles.test });
const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

beforeEach(async () => {
  await i18n.changeLanguage("zh");
});

describe("RoleRow", () => {
  it("saves each choice as it is made, but keeps an incompatible pair unsaved with the reason", async () => {
    const { onsave } = renderRow();
    expect(screen.queryByRole("alert")).toBeNull();

    // DeepSeek has no OpenAI Responses endpoint, so Codex cannot use it: shown, not saved.
    await choose(screen.getByLabelText(text.roles.agent), text.kinds.codex);
    expect(screen.getByRole("alert")).toHaveTextContent(text.compat.codex);
    expect(screen.getByText(text.save.unsaved)).toBeInTheDocument();
    expect(onsave).not.toHaveBeenCalled();
    expect(testButton()).toBeDisabled();

    // Back to the CLI's own login: compatible, so the change saves at once.
    await choose(screen.getByLabelText(text.roles.provider), text.ownLogin);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(onsave).toHaveBeenCalledTimes(1);
    expect(onsave).toHaveBeenCalledWith({ agent: "codex", provider: "", model: "deepseek-chat", effort: "low" });
  });

  it("commits the model on blur or Enter, not per keystroke", async () => {
    const { onsave } = renderRow();
    const model = screen.getByLabelText(text.roles.model);
    await fireEvent.input(model, { target: { value: " deepseek-reasoner " } });
    expect(onsave).not.toHaveBeenCalled();
    // A half-typed model is not what Test would use.
    expect(testButton()).toBeDisabled();
    await fireEvent.blur(model);
    expect(onsave).toHaveBeenCalledWith({ agent: "pi", provider: "deepseek", model: "deepseek-reasoner", effort: "low" });

    await fireEvent.input(model, { target: { value: "deepseek-chat-2" } });
    await fireEvent.keyDown(model, { key: "Enter" });
    expect(onsave).toHaveBeenLastCalledWith({ agent: "pi", provider: "deepseek", model: "deepseek-chat-2", effort: "low" });
  });

  it("says Saving…, then Saved, or the server's refusal inline", async () => {
    let finish: (value?: unknown) => void = () => undefined;
    renderRow({ onsave: () => new Promise((resolve) => (finish = resolve)) });
    await choose(screen.getByLabelText(text.roles.effort), text.effort.high);
    expect(screen.getByText(text.save.saving)).toBeInTheDocument();
    finish();
    await flush();
    // The parent re-renders with the saved config; here it stays dirty, so "Saved" waits for that.
    expect(screen.queryByText(text.save.saving)).toBeNull();
  });

  it("a change made while a save is in flight is saved after it, not lost", async () => {
    const calls: RoleConfig[] = [];
    let finish: () => void = () => undefined;
    const onsave = vi.fn((config: RoleConfig) => {
      calls.push(config);
      return calls.length === 1 ? new Promise<void>((resolve) => (finish = resolve)) : Promise.resolve();
    });
    const ontest = vi.fn(() => Promise.resolve({ ok: true, text: "", error: "", durationMs: 1, agent: "pi", model: "" }));
    const { rerender } = render(RoleRow, { props: { role: "distiller", config: saved, providers: [deepseek], onsave, ontest } });
    await choose(screen.getByLabelText(text.roles.effort), text.effort.high);
    await choose(screen.getByLabelText(text.roles.provider), text.ownLogin);
    expect(calls).toHaveLength(1);
    // The first save lands: the parent passes the saved configuration back.
    await rerender({ config: { ...saved, effort: "high" } });
    finish();
    await flush();
    await flush();
    expect(calls).toEqual([
      { ...saved, effort: "high" },
      { ...saved, effort: "high", provider: "" },
    ]);
    await rerender({ config: { ...saved, effort: "high", provider: "" } });
    expect(screen.getByLabelText(text.roles.provider)).toHaveTextContent(text.ownLogin);
    expect(screen.getByText(text.save.saved)).toBeInTheDocument();
  });

  it("shows a refused save inline and keeps the choice on screen", async () => {
    renderRow({ onsave: () => Promise.reject(new Error("roles: provider gone")) });
    await choose(screen.getByLabelText(text.roles.effort), text.effort.high);
    await flush();
    expect(screen.getByText(/roles: provider gone/)).toBeInTheDocument();
    expect(screen.getByLabelText(text.roles.effort)).toHaveTextContent(text.effort.high);
  });

  it("tests the saved configuration and shows the round-trip result", async () => {
    const { ontest } = renderRow();
    await fireEvent.click(testButton());
    expect(ontest).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("pong")).toBeInTheDocument();
    expect(screen.getByText(/1234 ms/)).toBeInTheDocument();
  });

  it("shows a failed test's error", async () => {
    renderRow({
      ontest: () => Promise.resolve({ ok: false, text: "", error: "401 invalid api key", durationMs: 80, agent: "pi", model: "" }),
    });
    await fireEvent.click(testButton());
    expect(await screen.findByText("401 invalid api key")).toBeInTheDocument();
  });
});
