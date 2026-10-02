import type { WorkItemStatus } from "./api.js";

/**
 * Which actions a menu offers, decided apart from rendering so both modes can
 * be checked without a browser. Labels and handlers belong to the caller.
 */

export type MessageAction = "copy-text" | "hide" | "copy-link";

export function messageActions(options: {
  desktop: boolean;
  /** The person's own message — the only kind that can be hidden. */
  own: boolean;
  redacted: boolean;
}): MessageAction[] {
  const actions: MessageAction[] = [];
  if (!options.redacted) actions.push("copy-text");
  if (options.own && !options.redacted) actions.push("hide");
  // A permalink needs an address bar to be pasted into; the desktop app has none.
  if (!options.desktop) actions.push("copy-link");
  return actions;
}

export type TaskAction = "open" | "stop" | "done" | "archive" | "reveal";

export function taskActions(options: { desktop: boolean; running: boolean; status: WorkItemStatus }): TaskAction[] {
  const actions: TaskAction[] = ["open"];
  if (options.running) actions.push("stop");
  if (options.status === "open") actions.push("done");
  if (options.status !== "closed") actions.push("archive");
  // Only the desktop host can show a folder in the file manager.
  if (options.desktop) actions.push("reveal");
  return actions;
}
