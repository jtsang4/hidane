import { expect, test, turn, unique, waitForEvent } from "./fixtures.js";

test("webhook: a signed delivery is captured, triaged and answered", async ({ page, api }) => {
  await api.setAllRoles("claude");
  const marker = unique("e2e-hook");
  const body = JSON.stringify({ event: "deploy", id: marker });

  const accepted = await api.webhook("e2e", body);
  expect(accepted.status()).toBe(200);
  const { eventId } = (await accepted.json()) as { ok: boolean; eventId: string };

  const captured = await api.event(eventId);
  expect(captured).toMatchObject({
    kind: "connector.webhook",
    source: "connector:webhook:e2e",
    payload: { name: "e2e", body: { event: "deploy", id: marker } },
  });
  const triage = await waitForEvent(
    api,
    `after=${captured.seq}`,
    (e) => e.kind === "triage.decision" && e.payload["of"] === eventId,
    "triage.decision for the webhook",
    5_000,
  );
  expect(triage.payload).toMatchObject({ action: "wake_primary", ofKind: "connector.webhook" });
  const reply = await waitForEvent(
    api,
    `after=${triage.seq}`,
    (e) => e.kind === "agent.reply" && e.payload["root"] === triage.id,
    "agent.reply to the external event",
    5_000,
  );
  expect(reply.payload["rootKind"]).toBe("external");
  expect(String(reply.payload["rootText"])).toContain(marker);
  expect(String(reply.payload["text"])).toMatch(/^收到外部事件：/);

  // The answer is in the conversation, marked as a reply to an external event.
  await page.goto("/");
  const external = turn(page, triage.id);
  await expect(external).toContainText("外部事件");
  await expect(external).toContainText("收到外部事件：");
});
