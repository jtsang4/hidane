import { fireEvent, render, screen } from "@testing-library/svelte";
import { tick } from "svelte";
import { beforeEach, describe, expect, it, onTestFinished, vi } from "vitest";
import i18n from "../src/i18n/index.js";
import { confirmAction, confirmState, settleConfirm } from "../src/lib/confirm.svelte.js";
import ConfirmDialog from "../src/components/ConfirmDialog.svelte";
import ConfirmHost from "../src/components/ConfirmHost.svelte";

beforeEach(async () => {
  await i18n.changeLanguage("zh");
  while (confirmState.current) settleConfirm(false);
});

describe("ConfirmDialog", () => {
  function open(props: Partial<{ destructive: boolean; body: string }> = {}) {
    const onresult = vi.fn();
    render(ConfirmDialog, { props: { title: "删除这条规则？", body: props.body ?? "bash, write", confirmLabel: "删除", destructive: props.destructive ?? true, onresult } });
    return onresult;
  }

  it("is an accessible alert dialog that starts on the confirm button", () => {
    open();
    const dialog = screen.getByRole("alertdialog", { name: "删除这条规则？" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleDescription("bash, write");
    expect(screen.getByRole("button", { name: "删除" })).toHaveFocus();
    expect(screen.getByRole("button", { name: "删除" }).className).toContain("bg-danger");
  });

  it("Esc cancels, Enter confirms, the buttons answer for themselves", async () => {
    const onresult = open();
    const dialog = screen.getByRole("alertdialog");
    await fireEvent.keyDown(dialog, { key: "Escape" });
    expect(onresult).toHaveBeenLastCalledWith(false);
    await fireEvent.keyDown(dialog, { key: "Enter" });
    expect(onresult).toHaveBeenLastCalledWith(true);
    await fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(onresult).toHaveBeenLastCalledWith(false);
    await fireEvent.click(screen.getByRole("button", { name: "删除" }));
    expect(onresult).toHaveBeenLastCalledWith(true);
  });

  it("keeps Tab inside the dialog", async () => {
    // The trap asks `tabbable`, which counts an element without client rects as hidden — every element, in jsdom.
    const rects = vi.spyOn(Element.prototype, "getClientRects").mockReturnValue([new DOMRect(0, 0, 1, 1)] as unknown as DOMRectList);
    onTestFinished(() => rects.mockRestore());
    open();
    const cancel = screen.getByRole("button", { name: "取消" });
    const confirm = screen.getByRole("button", { name: "删除" });
    await fireEvent.keyDown(confirm, { key: "Tab" });
    expect(cancel).toHaveFocus();
    await fireEvent.keyDown(cancel, { key: "Tab", shiftKey: true });
    expect(confirm).toHaveFocus();
  });

  it("is not destructive unless asked", () => {
    open({ destructive: false });
    expect(screen.getByRole("button", { name: "删除" }).className).not.toContain("bg-danger");
  });
});

describe("confirmAction", () => {
  it("resolves with the person's answer, one dialog at a time", async () => {
    render(ConfirmHost);
    const first = confirmAction({ title: "停止这个任务？", confirmLabel: "停止", destructive: true });
    const second = confirmAction({ title: "归档这个任务？", confirmLabel: "归档" });
    await tick();
    expect(screen.getAllByRole("alertdialog")).toHaveLength(1);
    expect(screen.getByRole("alertdialog", { name: "停止这个任务？" })).toBeInTheDocument();

    await fireEvent.click(screen.getByRole("button", { name: "取消" }));
    await expect(first).resolves.toBe(false);
    await tick();
    expect(screen.getByRole("alertdialog", { name: "归档这个任务？" })).toBeInTheDocument();

    await fireEvent.click(screen.getByRole("button", { name: "归档" }));
    await expect(second).resolves.toBe(true);
    await tick();
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("leaves no native dialog in the source", async () => {
    const { readFileSync, readdirSync, statSync } = await import("node:fs");
    const { join } = await import("node:path");
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const name of readdirSync(dir)) {
        const path = join(dir, name);
        if (statSync(path).isDirectory()) walk(path);
        else if (/\.(svelte|ts)$/.test(name)) files.push(path);
      }
    };
    walk(join(process.cwd(), "src"));
    const offenders = files.filter((file) => /(^|[^.\w])(window\.)?(confirm|alert|prompt)\(/.test(readFileSync(file, "utf8").replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, "")));
    expect(offenders).toEqual([]);
  });
});
