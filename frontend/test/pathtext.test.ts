import { render } from "@testing-library/svelte";
import { describe, expect, it } from "vitest";
import PathText from "../src/components/PathText.svelte";

describe("PathText", () => {
  it("offers a line break after each slash and nowhere inside a name", () => {
    const { container } = render(PathText, { props: { path: "/var/hidane/memory/MEMORY.md" } });
    expect(container.innerHTML.replace(/<!---->/g, "")).toBe("/<wbr>var/<wbr>hidane/<wbr>memory/<wbr>MEMORY.md");
  });

  it("copies as the path itself: the breaks add no characters", () => {
    const path = "C:/Users/me/hidane data/POLICY.json";
    const { container } = render(PathText, { props: { path } });
    expect(container.textContent).toBe(path);
  });
});
