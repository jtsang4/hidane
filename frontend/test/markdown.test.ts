import { fireEvent, render, screen } from "@testing-library/svelte";
import { marked } from "marked";
import { get } from "svelte/store";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Markdown from "../src/components/Markdown.svelte";
import i18n from "../src/i18n/index.js";
import { clearToasts, toastStore } from "../src/lib/toast.js";

// jsdom cannot lay out a diagram: Mermaid is replaced by a stand-in that "draws" what the real one could
// return from hostile input, so these tests cover what hidane does with its output.
const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  parse: vi.fn(async (source: string) => (source.includes("broken") ? false : { diagramType: "flowchart-v2" })),
  render: vi.fn(async (id: string, source: string) => ({
    svg:
      `<svg id="${id}" aria-roledescription="flowchart-v2" style="max-width: 400px;" onload="alert(1)">` +
      `<script>alert(1)</script><style>#${id} .node{stroke-width:1px}</style>` +
      `<a href="javascript:alert(1)"><g class="node"><foreignObject width="80" height="20"><div><p>${source.split("\n").at(-1)?.trim()}</p>` +
      `<img src="x" onerror="alert(1)"></div></foreignObject></g></a></svg>`,
  })),
}));
vi.mock("mermaid", () => ({ default: mermaid }));

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

  describe("mermaid blocks", () => {
    beforeEach(async () => {
      await i18n.changeLanguage("zh");
      // jsdom has no canvas; the theme then takes the tokens as written.
      vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    });

    it("draws a diagram in place of its code, sanitized, keeping the source to copy", async () => {
      const writeText = vi.fn<(text: string) => Promise<void>>(async () => undefined);
      Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
      const source = "flowchart LR\n  A --> B";
      const { container } = render(Markdown, { props: { content: `Plan:\n\n\`\`\`mermaid\n${source}\n\`\`\`\n\n\`\`\`sh\nmake test\n\`\`\`` } });

      const svg = await vi.waitFor(() => {
        const drawn = container.querySelector<SVGSVGElement>("svg[aria-roledescription]");
        expect(drawn).not.toBeNull();
        return drawn!;
      });
      expect(svg.textContent).toContain("A --> B");
      expect(container.querySelectorAll("pre")).toHaveLength(1);
      expect(container.querySelector("pre")?.textContent).toContain("make test");
      // Strict level asked of Mermaid, and nothing executable let through regardless.
      expect(mermaid.initialize).toHaveBeenCalledWith(expect.objectContaining({ securityLevel: "strict", theme: "base", startOnLoad: false }));
      expect(container.querySelector("script, [onload], [onerror]")).toBeNull();
      expect(container.querySelector("a")?.getAttribute("href") ?? "").not.toContain("javascript:");
      // Shrinks to fit only so far, then scrolls.
      expect(svg.style.minWidth).toBe("340px");

      const buttons = screen.getAllByRole("button", { name: "复制代码" });
      expect(buttons).toHaveLength(2);
      await fireEvent.click(buttons[0]!);
      expect(writeText).toHaveBeenCalledWith(source);
    });

    it("leaves a block Mermaid cannot draw as code", async () => {
      const { container } = render(Markdown, { props: { content: "```mermaid\nflowchart broken ((\n```" } });
      await vi.waitFor(() => expect(mermaid.parse).toHaveBeenCalledWith("flowchart broken ((", { suppressErrors: true }));
      await Promise.resolve();
      expect(container.querySelector("svg[aria-roledescription]")).toBeNull();
      expect(container.querySelector("pre code.language-mermaid")?.textContent).toContain("flowchart broken ((");
    });
  });
});
