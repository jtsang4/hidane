import { get } from "svelte/store";
import { describe, expect, it } from "vitest";
import { pushToast, dismissToast, clearToasts, toastStore } from "../src/lib/toast.js";

describe("toast store", () => {
  it("does not stack an identical repeated message", () => {
    clearToasts();
    pushToast("boom");
    pushToast("boom");
    expect(get(toastStore)).toHaveLength(1);
  });

  it("dismisses by id", () => {
    clearToasts();
    const id = pushToast("one");
    pushToast("two");
    dismissToast(id);
    expect(get(toastStore).map((t) => t.message)).toEqual(["two"]);
  });
});
