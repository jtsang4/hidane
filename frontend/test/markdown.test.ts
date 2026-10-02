import { render } from "@testing-library/svelte";
import { marked } from "marked";
import { describe, expect, it, vi } from "vitest";
import Markdown from "../src/components/Markdown.svelte";

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
});
