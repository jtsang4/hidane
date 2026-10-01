import { expect, sign, test, turn, unique, waitForEvent } from "./fixtures.js";
import { WEBHOOK_SECRET } from "./env.js";

test("webhook: a signed delivery is captured, triaged and answered; a bad signature is refused", async ({ page, api }) => {
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

  // A wrong signature is refused and leaves nothing in the log.
  const forgedId = unique("e2e-forged");
  const forged = JSON.stringify({ event: "deploy", id: forgedId });
  const before = (await api.events("tail=1"))[0]?.seq ?? 0;
  const refused = await api.webhook("e2e", forged, "sha256=0000");
  expect(refused.status()).toBe(401);
  const unsigned = await api.request.post("/webhook/e2e", { headers: { "content-type": "application/json" }, data: forged });
  expect(unsigned.status()).toBe(401);
  // Signed for a different body: also refused.
  const replayed = await api.webhook("e2e", forged, await sign(body, WEBHOOK_SECRET));
  expect(replayed.status()).toBe(401);
  await page.waitForTimeout(1_000);
  const after = await api.events(`after=${before}`);
  expect(after.filter((e) => e.kind === "connector.webhook")).toEqual([]);
  expect(JSON.stringify(after)).not.toContain(forgedId);
});
