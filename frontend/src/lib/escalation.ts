import i18n from "../i18n/index.js";

/**
 * What an escalation asks the person. Stops the runtime writes (a spent budget,
 * a missing repository) are in Chinese, whatever the reader's language; their
 * `reason` is the fact, so the UI says it in the reader's language instead.
 * Questions an agent asked are shown as written.
 */
export function escalationText(
  payload: Record<string, unknown>,
  t: (
    key: "task.escalation.budget" | "task.escalation.repoMissing",
    values?: { name: string; path: string },
  ) => string = (key, values) => (values ? i18n.t(key, values) : i18n.t(key)),
): string {
  const reason = payload["reason"];
  if (reason === "budget") return t("task.escalation.budget");
  if (reason === "repo_missing" && typeof payload["name"] === "string" && typeof payload["repoPath"] === "string") {
    return t("task.escalation.repoMissing", { name: payload["name"], path: payload["repoPath"] });
  }
  const question = payload["question"];
  return typeof question === "string" ? question : "";
}
