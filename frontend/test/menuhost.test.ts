import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, expect, it, vi } from "vitest";
import { closeMenu, menuState, openMenu } from "../src/lib/contextMenu.svelte.js";
import MenuHost from "../src/components/MenuHost.svelte";

const items = [
  { id: "copy-text", label: "复制文本" },
  { id: "hide", label: "隐藏", danger: true },
];

describe("MenuHost", () => {
  it("opens focused on the first item, moves with the arrows and runs the chosen item", async () => {
    render(MenuHost);
    const onselect = vi.fn();
    openMenu({ x: 10, y: 10, align: "start", returnFocus: null }, "消息操作", items, onselect);
    await tick();
    const menu = screen.getByRole("menu", { name: "消息操作" });
    const entries = screen.getAllByRole("menuitem");
    expect(entries.map((entry) => entry.textContent?.trim())).toEqual(["复制文本", "隐藏"]);
    expect(entries[0]).toHaveFocus();
    await fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(entries[1]).toHaveFocus();
    await fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(entries[0]).toHaveFocus();
    await fireEvent.click(entries[1]!);
    expect(menuState.current).toBeNull();
    await tick();
    expect(onselect).toHaveBeenCalledWith("hide");
  });

  it("Esc closes it without a choice and gives focus back to the ⋯ button", async () => {
    render(MenuHost);
    const trigger = document.createElement("button");
    document.body.append(trigger);
    const onselect = vi.fn();
    openMenu({ x: 10, y: 10, align: "end", returnFocus: trigger }, "消息操作", items, onselect);
    await tick();
    await fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(trigger).toHaveFocus();
    expect(onselect).not.toHaveBeenCalled();
    trigger.remove();
  });

  it("opens nothing for an empty list", () => {
    closeMenu();
    openMenu({ x: 0, y: 0, align: "start", returnFocus: null }, "x", [], () => undefined);
    expect(menuState.current).toBeNull();
  });
});

describe("right click on selected text", () => {
  it("keeps the system menu only when the text was selected before the press", async () => {
    const { keepsSystemMenu, watchContextSelection } = await import("../src/lib/contextMenu.svelte.js");
    const stop = watchContextSelection();
    const message = document.createElement("p");
    message.textContent = "please deploy it tonight";
    document.body.append(message);
    const select = () => {
      const range = document.createRange();
      range.selectNodeContents(message);
      window.getSelection()?.removeAllRanges();
      window.getSelection()?.addRange(range);
    };

    // Nothing selected; the press selects a word (as WebKit does): ours, and the word is let go.
    window.getSelection()?.removeAllRanges();
    message.dispatchEvent(new MouseEvent("mousedown", { button: 2, bubbles: true }));
    select();
    expect(keepsSystemMenu(message)).toBe(false);
    expect(window.getSelection()?.isCollapsed).toBe(true);

    // Selected beforehand: the system's Copy menu.
    select();
    message.dispatchEvent(new MouseEvent("mousedown", { button: 2, bubbles: true }));
    expect(keepsSystemMenu(message)).toBe(true);

    stop();
    message.remove();
  });
});
