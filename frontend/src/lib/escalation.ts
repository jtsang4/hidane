import i18n from "../i18n/index.js";

/**
 * What an escalation asks the person. Budget and deadline stops are written by
 * the runtime in Chinese, whatever the reader's language; their `reason` is
 * the fact, so the UI says it in the reader's language instead. Questions an
 * agent asked are shown as written.
 */
export function escalationText(payload: Record<string, unknown>, t: (key: "task.escalation.budget" | "task.escalation.deadline") => string = (key) => i18n.t(key)): string {
  const reason = payload["reason"];
  if (reason === "budget") return t("task.escalation.budget");
  if (reason === "deadline") return t("task.escalation.deadline");
  const question = payload["question"];
  return typeof question === "string" ? question : "";
}
