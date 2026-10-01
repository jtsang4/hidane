import { render } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import Markdown from "../src/components/Markdown.svelte";

describe("Markdown", () => {
  it("renders GFM while stripping executable HTML attributes", () => {
    const { container } = render(Markdown, {
      props: { content: "<img src=x onerror=alert(1)>\n\n**safe**" },
    });

    expect(container.querySelector("[onerror]")).toBeNull();
    expect(container.textContent).toContain("safe");
  });
});
