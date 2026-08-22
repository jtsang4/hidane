import { beforeEach, describe, expect, it } from "vitest";
import i18n, { storedLanguage, switchLanguage } from "../src/i18n/index.js";

beforeEach(async () => {
  localStorage.clear();
  await i18n.changeLanguage("zh");
});

describe("i18n", () => {
  it("defaults to Chinese", () => {
    expect(storedLanguage()).toBe("zh");
    expect(i18n.t("nav.chat")).toBe("会话");
    expect(i18n.t("status.title")).toBe("运行状态");
  });

  it("switches to English and persists the choice", () => {
    switchLanguage("en");
    expect(i18n.t("nav.chat")).toBe("Chat");
    expect(i18n.t("items.updatedAt", { time: "T" })).toBe("updated T");
    expect(localStorage.getItem("hidane-lang")).toBe("en");
    expect(storedLanguage()).toBe("en");
  });

  it("interpolates variables in both languages", async () => {
    expect(i18n.t("item.toolCalls", { n: 3 })).toBe("3 次工具调用");
    await i18n.changeLanguage("en");
    expect(i18n.t("item.toolCalls", { n: 3 })).toBe("3 tool calls");
  });

  it("falls back to Chinese for unknown stored values", () => {
    localStorage.setItem("hidane-lang", "fr");
    expect(storedLanguage()).toBe("zh");
  });
});
