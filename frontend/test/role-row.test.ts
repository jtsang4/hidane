import { fireEvent, render, screen } from "@testing-library/svelte";
import { beforeEach, describe, expect, it, vi } from "vitest";
import i18n from "../src/i18n/index.js";
import { zh } from "../src/i18n/resources.js";
import type { AgentTestResult, ProviderView, RoleConfig } from "../src/lib/api.js";
import RoleRow from "../src/components/settings/RoleRow.svelte";

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

const saveButton = () => screen.getByRole("button", { name: text.roles.save });
const testButton = () => screen.getByRole("button", { name: text.roles.test });

beforeEach(async () => {
  await i18n.changeLanguage("zh");
});

describe("RoleRow", () => {
  it("warns about and refuses to save an incompatible agent × provider", async () => {
    const { onsave } = renderRow();
    expect(screen.queryByRole("alert")).toBeNull();

    // DeepSeek has no OpenAI Responses endpoint, so Codex cannot use it.
    await fireEvent.change(screen.getByLabelText(text.roles.agent), { target: { value: "codex" } });
    expect(screen.getByRole("alert")).toHaveTextContent(text.compat.codex);
    expect(saveButton()).toBeDisabled();

    // Back to the CLI's own login: always compatible, and now a saveable change.
    await fireEvent.change(screen.getByLabelText(text.roles.provider), { target: { value: "" } });
    expect(screen.queryByRole("alert")).toBeNull();
    expect(saveButton()).toBeEnabled();
    await fireEvent.click(saveButton());
    expect(onsave).toHaveBeenCalledWith({ agent: "codex", provider: "", model: "deepseek-chat", effort: "low" });
  });

  it("only tests the saved configuration and shows the round-trip result", async () => {
    const { ontest } = renderRow();
    expect(saveButton()).toBeDisabled();
    await fireEvent.click(testButton());
    expect(ontest).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("pong")).toBeInTheDocument();
    expect(screen.getByText(/1234 ms/)).toBeInTheDocument();

    await fireEvent.change(screen.getByLabelText(text.roles.effort), { target: { value: "high" } });
    expect(testButton()).toBeDisabled();
    expect(screen.getByText(text.roles.unsaved)).toBeInTheDocument();
  });

  it("shows a failed test's error", async () => {
    renderRow({
      ontest: () => Promise.resolve({ ok: false, text: "", error: "401 invalid api key", durationMs: 80, agent: "pi", model: "" }),
    });
    await fireEvent.click(testButton());
    expect(await screen.findByText("401 invalid api key")).toBeInTheDocument();
  });
});
