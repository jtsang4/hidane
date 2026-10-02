import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, expect, it, vi } from "vitest";
import i18n from "../src/i18n/index.js";
import type { HidaneEvent, WorkItem } from "../src/lib/api.js";
import { filterCommands, stepSelection } from "../src/lib/palette.js";
import CommandPalette, { type PaletteCommand } from "../src/components/CommandPalette.svelte";

describe("filterCommands", () => {
  const entries = [
    { label: "前往会话", keywords: ["Go to Chat", "/"] },
    { label: "前往任务", keywords: ["Go to Tasks", "/items"] },
    { label: "设置：角色", keywords: ["Roles", "Settings"] },
    { label: "设置：通用", keywords: ["General", "Settings"] },
  ];

  it("keeps everything for an empty query", () => {
    expect(filterCommands(entries, "  ")).toEqual(entries);
  });

  it("matches labels and keywords in either language, every term required", () => {
    expect(filterCommands(entries, "settings").map((e) => e.label)).toEqual(["设置：角色", "设置：通用"]);
    expect(filterCommands(entries, "settings roles").map((e) => e.label)).toEqual(["设置：角色"]);
    expect(filterCommands(entries, "TASKS").map((e) => e.label)).toEqual(["前往任务"]);
    expect(filterCommands(entries, "nothing")).toEqual([]);
  });

  it("ranks a label prefix above a keyword match", () => {
    const ranked = filterCommands([{ label: "Open settings", keywords: [] }, { label: "Settings: Roles", keywords: [] }], "sett");
    expect(ranked.map((e) => e.label)).toEqual(["Settings: Roles", "Open settings"]);
  });
});

describe("stepSelection", () => {
  it("wraps at both ends and handles empty lists", () => {
    expect(stepSelection(0, 1, 3)).toBe(1);
    expect(stepSelection(2, 1, 3)).toBe(0);
    expect(stepSelection(0, -1, 3)).toBe(2);
    expect(stepSelection(-1, 1, 3)).toBe(0);
    expect(stepSelection(-1, -1, 3)).toBe(2);
    expect(stepSelection(0, 1, 0)).toBe(-1);
  });
});

describe("CommandPalette", () => {
  const item: WorkItem = {
    id: "wi_deploy",
    title: "Deploy the site",
    status: "open",
    workspace: "/tmp/wi_deploy",
    threadId: "th_1",
    parentId: null,
    deadlineAt: null,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
  };
  const hit: HidaneEvent = {
    seq: 7,
    id: "ev_said",
    ts: "2026-10-01T00:00:00Z",
    source: "connector:web",
    kind: "user.message",
    threadId: "main",
    workItemId: null,
    executionId: null,
    payload: { text: "please deploy it tonight" },
  };

  function setup() {
    const ran: string[] = [];
    const commands: PaletteCommand[] = ["chat", "items", "roles"].map((id) => ({
      id,
      label: id === "roles" ? "Settings: Roles" : `Go to ${id}`,
      keywords: [],
      group: id === "roles" ? "settings" : "go",
      run: () => ran.push(id),
    }));
    const search = vi.fn((query: string) => Promise.resolve({ events: query.includes("deploy") ? [hit] : [], items: query.includes("deploy") ? [item] : [], titles: {} }));
    const onclose = vi.fn();
    const onopenitem = vi.fn();
    const onopenmessage = vi.fn();
    render(CommandPalette, { props: { commands, onclose, onopenitem, onopenmessage, search, debounceMs: 0 } });
    const input = screen.getByRole("combobox");
    return { ran, search, onclose, onopenitem, onopenmessage, input };
  }

  const settle = async () => {
    await new Promise((resolve) => setTimeout(resolve, 5));
    await tick();
  };

  it("filters commands as you type and runs the selected one with Enter", async () => {
    await i18n.changeLanguage("en");
    const { ran, onclose, input } = setup();
    expect(input).toHaveFocus();
    expect(screen.getAllByRole("option")).toHaveLength(3);
    await fireEvent.input(input, { target: { value: "go" } });
    expect(screen.getAllByRole("option").map((o) => o.textContent?.trim())).toEqual(["Go to chat", "Go to items"]);
    expect(screen.getAllByRole("option")[0]).toHaveAttribute("aria-selected", "true");

    await fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(screen.getAllByRole("option")[1]).toHaveAttribute("aria-selected", "true");
    expect(input.getAttribute("aria-activedescendant")).toBe(screen.getAllByRole("option")[1]!.id);
    // Wraps round to the top.
    await fireEvent.keyDown(input, { key: "ArrowDown" });
    expect(screen.getAllByRole("option")[0]).toHaveAttribute("aria-selected", "true");
    await fireEvent.keyDown(input, { key: "ArrowUp" });
    await fireEvent.keyDown(input, { key: "Enter" });
    expect(onclose).toHaveBeenCalled();
    await tick();
    expect(ran).toEqual(["items"]);
  });

  it("searches the conversation and work items, and opens what is chosen", async () => {
    await i18n.changeLanguage("en");
    const { search, onopenitem, onopenmessage, input } = setup();
    await fireEvent.input(input, { target: { value: "deploy" } });
    await settle();
    expect(search).toHaveBeenLastCalledWith("deploy");
    expect(screen.getByRole("option", { name: /Deploy the site/ })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /please deploy it tonight/ })).toBeInTheDocument();
    // No command matches "deploy": the task is first.
    await fireEvent.keyDown(input, { key: "Enter" });
    await tick();
    expect(onopenitem).toHaveBeenCalledWith("wi_deploy");

    await fireEvent.click(screen.getByRole("option", { name: /please deploy it tonight/ }));
    await tick();
    expect(onopenmessage).toHaveBeenCalledWith("ev_said");
  });

  it("closes with Esc and says when nothing matches", async () => {
    await i18n.changeLanguage("en");
    const { onclose, input } = setup();
    await fireEvent.input(input, { target: { value: "zzz" } });
    await settle();
    expect(screen.getByText("No matches.")).toBeInTheDocument();
    await fireEvent.keyDown(input, { key: "Escape" });
    expect(onclose).toHaveBeenCalled();
  });
});
