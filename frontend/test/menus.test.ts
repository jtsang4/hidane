import { beforeEach, describe, expect, it } from "vitest";
import i18n from "../src/i18n/index.js";
import { escalationText } from "../src/lib/escalation.js";
import { messageActions, taskActions } from "../src/lib/menus.js";
import { followAfterScroll } from "../src/lib/scroll.js";
import { taskMenuEntries } from "../src/lib/taskActions.js";

beforeEach(async () => {
  await i18n.changeLanguage("zh");
});

describe("message menu", () => {
  it("offers copying the text everywhere and a permalink only where there is an address bar", () => {
    expect(messageActions({ desktop: true, own: true, redacted: false })).toEqual(["copy-text", "hide"]);
    expect(messageActions({ desktop: false, own: true, redacted: false })).toEqual(["copy-text", "copy-link", "hide"]);
  });

  it("hides only the person's own messages, and nothing is left to copy once hidden", () => {
    expect(messageActions({ desktop: true, own: false, redacted: false })).toEqual(["copy-text"]);
    expect(messageActions({ desktop: false, own: true, redacted: true })).toEqual(["copy-link"]);
  });

  it("offers making a task of a Primary answer, before anything destructive", () => {
    expect(messageActions({ desktop: true, own: false, redacted: false, promotable: true })).toEqual(["copy-text", "promote"]);
  });
});

describe("task menu", () => {
  it("offers stop only while running and reveal only in the desktop app", () => {
    expect(taskActions({ desktop: true, running: true, status: "open" })).toEqual(["open", "stop", "done", "archive", "reveal"]);
    expect(taskActions({ desktop: false, running: false, status: "open" })).toEqual(["open", "done", "archive"]);
    expect(taskActions({ desktop: false, running: false, status: "done" })).toEqual(["open", "archive"]);
    expect(taskActions({ desktop: true, running: false, status: "closed" })).toEqual(["open", "reveal"]);
  });

  it("labels the entries in the reader's language", () => {
    const entries = taskMenuEntries({ id: "wi_a", title: "A", running: true, status: "open" }, true);
    expect(entries.map((entry) => entry.label)).toEqual(["展开", "停止", "标记完成", "归档", "在访达中显示工作区"]);
    expect(entries.find((entry) => entry.id === "stop")?.danger).toBe(true);
  });
});

describe("following the live edge", () => {
  it("only the reader's own scroll leaves it; layout growth is followed", () => {
    expect(followAfterScroll({ live: true, pinned: false, userInitiated: false, following: true })).toBe("repin");
    expect(followAfterScroll({ live: true, pinned: false, userInitiated: true, following: true })).toBe("unfollow");
    expect(followAfterScroll({ live: true, pinned: true, userInitiated: true, following: false })).toBe("follow");
    expect(followAfterScroll({ live: true, pinned: false, userInitiated: false, following: false })).toBe("keep");
    expect(followAfterScroll({ live: false, pinned: true, userInitiated: false, following: true })).toBe("unfollow");
  });
});

describe("escalation text", () => {
  it("says a budget stop in the reader's language, not the runtime's Chinese", async () => {
    await i18n.changeLanguage("en");
    const budget = escalationText({ reason: "budget", question: "「x」已经执行了 8 次，已暂停。" });
    expect(budget).toMatch(/^Paused after reaching its limit/);
    expect(escalationText({ reason: "repo_missing", name: "blog", repoPath: "/code/blog", question: "仓库「blog」不在 /code/blog 了。" })).toBe(
      "Repository “blog” is no longer at /code/blog. If it moved, tell me where; if it is gone for good, remove it under Tasks → Worktrees.",
    );
    // An agent's own question is shown as asked.
    expect(escalationText({ reason: "question", question: "Which branch?" })).toBe("Which branch?");
    expect(escalationText({})).toBe("");
  });
});
