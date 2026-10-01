import { afterEach, describe, expect, it, vi } from "vitest";
import { installDesktopLinks, linkAction } from "../src/lib/desktopLinks.js";

const ORIGIN = "wails://localhost/settings?x=1";

describe("linkAction", () => {
  it("sends other origins to the system browser", () => {
    expect(linkAction("https://example.com/x?y=1", ORIGIN)).toEqual({ kind: "external", url: "https://example.com/x?y=1" });
    expect(linkAction("mailto:a@b.c", ORIGIN)).toEqual({ kind: "external", url: "mailto:a@b.c" });
  });

  it("turns an artifact download into a reveal", () => {
    expect(linkAction("/api/work-items/wi_1/file?path=out%2Fa.txt&download", ORIGIN)).toEqual({
      kind: "reveal",
      workItemId: "wi_1",
      path: "out/a.txt",
    });
  });

  it("leaves in-app links and unknown schemes alone", () => {
    expect(linkAction("/?at=ev_1", ORIGIN)).toBeNull();
    expect(linkAction("/api/work-items/wi_1/file?path=a.txt", ORIGIN)).toBeNull();
    expect(linkAction("javascript:alert(1)", ORIGIN)).toBeNull();
  });
});

describe("installDesktopLinks", () => {
  let stop: (() => void) | undefined;
  afterEach(() => {
    stop?.();
    document.body.innerHTML = "";
  });

  it("intercepts external links and lets in-app ones through", () => {
    const send = vi.fn(() => Promise.resolve());
    stop = installDesktopLinks(document, () => "http://localhost:3000/", send);
    document.body.innerHTML = `<a id="out" href="https://example.com"><span id="inner">x</span></a><a id="in" href="/items">y</a>`;
    const outer = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
    document.getElementById("inner")!.dispatchEvent(outer);
    expect(outer.defaultPrevented).toBe(true);
    expect(send).toHaveBeenCalledWith({ kind: "external", url: "https://example.com/" });
    const inner = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 });
    document.getElementById("in")!.dispatchEvent(inner);
    expect(inner.defaultPrevented).toBe(false);
    expect(send).toHaveBeenCalledTimes(1);
  });
});
