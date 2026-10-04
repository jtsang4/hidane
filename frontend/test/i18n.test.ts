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
    expect(i18n.t("status.logTitle")).toBe("事件日志与分诊");
  });

  it("switches to English and persists the choice", () => {
    switchLanguage("en");
    expect(i18n.t("nav.chat")).toBe("Chat");
    expect(i18n.t("items.updated")).toBe("updated");
    expect(localStorage.getItem("hidane-lang")).toBe("en");
    expect(storedLanguage()).toBe("en");
  });

  it("interpolates variables in both languages", async () => {
    expect(i18n.t("item.toolCalls", { count: 3 })).toBe("3 次工具调用");
    await i18n.changeLanguage("en");
    expect(i18n.t("item.toolCalls", { count: 3 })).toBe("3 tool calls");
  });

  it("uses the singular in English for one", async () => {
    await i18n.changeLanguage("en");
    expect(i18n.t("item.toolCalls", { count: 1 })).toBe("1 tool call");
    expect(i18n.t("task.progress", { count: 1 })).toBe("1 tool call");
    expect(i18n.t("notice.more", { count: 1 })).toBe("1 more update");
    expect(i18n.t("notice.more", { count: 2 })).toBe("2 more updates");
    await i18n.changeLanguage("zh");
    expect(i18n.t("task.progress", { count: 1 })).toBe("1 次工具调用");
  });

  it("falls back to Chinese for unknown stored values", () => {
    localStorage.setItem("hidane-lang", "fr");
    expect(storedLanguage()).toBe("zh");
  });
});
