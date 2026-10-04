import { fireEvent, render, screen } from "@testing-library/svelte";
import { marked } from "marked";
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Markdown from "../src/components/Markdown.svelte";
import i18n from "../src/i18n/index.js";
import { clearToasts, toastStore } from "../src/lib/toast.js";

describe("Markdown", () => {
  it("renders GFM while stripping executable HTML attributes", () => {
    const { container } = render(Markdown, {
      props: { content: "<img src=x onerror=alert(1)>\n\n**safe**" },
    });

    expect(container.querySelector("[onerror]")).toBeNull();
    expect(container.textContent).toContain("safe");
  });

  it("streams escaped text without parsing Markdown until the reply finishes", async () => {
    const parse = vi.spyOn(marked, "parse");
    const { container, rerender } = render(Markdown, { props: { content: "**hello", streaming: true } });
    try {
      for (let i = 1; i <= 100; i++) {
        await rerender({ content: `**hello ${"x".repeat(i * 320)}**<img src=x onerror=alert(1)>`, streaming: true });
      }
      expect(parse).not.toHaveBeenCalled();
      expect(container.querySelector("img")).toBeNull();
      await rerender({ content: "**finished**\n\n| A | B |\n|---|---|\n| 1 | 2 |", streaming: false });
      expect(parse).toHaveBeenCalledTimes(1);
      expect(container.querySelector("strong")?.textContent).toBe("finished");
      expect(container.querySelector("table")).not.toBeNull();
    } finally {
      parse.mockRestore();
    }
  });

  describe("code blocks", () => {
    let writeText: ReturnType<typeof vi.fn<(text: string) => Promise<void>>>;

    beforeEach(async () => {
      await i18n.changeLanguage("zh");
      writeText = vi.fn<(text: string) => Promise<void>>(async () => undefined);
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    });

    afterEach(() => {
      vi.useRealTimers();
      clearToasts();
    });

    it("copies a block's code as written, says so, then turns back", async () => {
      vi.useFakeTimers();
      const code = "if a < b {\n  echo \"<b>\"\n}";
      render(Markdown, { props: { content: `Run:\n\n\`\`\`sh\n${code}\n\`\`\`\n\nthen \`inline\`.` } });

      // One per block: inline code gets none.
      expect(screen.getAllByRole("button", { name: "复制代码" })).toHaveLength(1);
      await fireEvent.click(screen.getByRole("button", { name: "复制代码" }));
      await vi.waitFor(() => expect(screen.getByRole("button", { name: "已复制" })).toBeInTheDocument());
      expect(writeText).toHaveBeenCalledWith(code);

      await vi.advanceTimersByTimeAsync(1500);
      expect(screen.getByRole("button", { name: "复制代码" })).toBeInTheDocument();
    });

    it("rebuilds the buttons with the content and shows a failed copy as a toast", async () => {
      writeText.mockRejectedValue(new Error("denied"));
      const { container, rerender } = render(Markdown, { props: { content: "```\none\n```" } });
      await rerender({ content: "```\ntwo\n```\n\n```\nthree\n```" });

      const buttons = screen.getAllByRole("button", { name: "复制代码" });
      expect(buttons).toHaveLength(2);
      expect(container.querySelectorAll("pre")).toHaveLength(2);
      await fireEvent.click(buttons[1]!);
      expect(writeText).toHaveBeenCalledWith("three");
      await vi.waitFor(() => expect(get(toastStore).map((t) => t.message).join()).toContain("denied"));
      expect(screen.queryByRole("button", { name: "已复制" })).toBeNull();
    });
  });
});
