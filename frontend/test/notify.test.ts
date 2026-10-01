import { describe, expect, it } from "vitest";
import { badgeTitle, completionFrom } from "../src/lib/notify.js";
import { matchesItem } from "../src/lib/search.js";

const event = (over: Record<string, unknown>) =>
  JSON.stringify({ kind: "execution.finished", workItemId: "wi_a", payload: {}, ...over });

describe("completion alerts", () => {
  it("announces finished executions, with the outcome", () => {
    const ok = completionFrom(event({ payload: { ok: true, summary: "done\n  fine" } }));
    expect(ok).toMatchObject({ ok: true, workItemId: "wi_a", summary: "done fine" });

    const failed = completionFrom(event({ payload: { ok: false, error: "boom" } }));
    expect(failed).toMatchObject({ ok: false, summary: "boom" });
  });

  it("ignores the noise that makes up most of the stream", () => {
    // Alerting on every tool call would be worse than not alerting at all.
    expect(completionFrom(event({ kind: "side_effect.intent" }))).toBeNull();
    expect(completionFrom(event({ kind: "connector.heartbeat" }))).toBeNull();
    expect(completionFrom(event({ kind: "triage.decision" }))).toBeNull();
    expect(completionFrom("not json")).toBeNull();
  });

  it("treats an escalation as news worth surfacing", () => {
    const e = completionFrom(event({ kind: "escalation", payload: { note: "wi_a finished a run" } }));
    expect(e?.ok).toBe(true);
    expect(e?.summary).toContain("finished a run");
  });

  it("caps the summary so a notification body stays readable", () => {
    const long = completionFrom(event({ payload: { ok: true, summary: "x".repeat(500) } }));
    expect(long!.summary.length).toBe(120);
  });
});

describe("badgeTitle", () => {
  it("shows the unread count and disappears at zero", () => {
    expect(badgeTitle("hidane", 0)).toBe("hidane");
    expect(badgeTitle("hidane", 3)).toBe("(3) hidane");
  });
});

describe("matchesItem", () => {
  const item = { id: "wi_d2sbax", title: "讲解 arXiv 论文 2608.13120", status: "open" };
  it("matches id, title and status, ANDing terms", () => {
    expect(matchesItem(item, "arXiv")).toBe(true);
    expect(matchesItem(item, "wi_d2s")).toBe(true);
    expect(matchesItem(item, "论文 open")).toBe(true);
    expect(matchesItem(item, "论文 closed")).toBe(false);
    expect(matchesItem(item, "  ")).toBe(true);
  });
});
