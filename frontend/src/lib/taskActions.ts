import type { QueryClient } from "@tanstack/svelte-query";
import Archive from "@lucide/svelte/icons/archive";
import Check from "@lucide/svelte/icons/check";
import FolderOpen from "@lucide/svelte/icons/folder-open";
import Maximize2 from "@lucide/svelte/icons/maximize-2";
import Square from "@lucide/svelte/icons/square";
import i18n from "../i18n/index.js";
import { api, type WorkItemStatus } from "./api.js";
import { boot } from "./boot.js";
import { confirmAction } from "./confirm.svelte.js";
import { openMenu, type MenuEntry, type MenuPlacement } from "./contextMenu.svelte.js";
import { taskActions, type TaskAction } from "./menus.js";
import { focusHref, navigate } from "./router.svelte.js";
import { pushToast, toastError } from "./toast.js";

/** Work-item actions shared by cards, the sidebar, the task list and the focus panel. */

function refresh(queryClient: QueryClient, id: string): void {
  void queryClient.invalidateQueries({ queryKey: ["board"] });
  void queryClient.invalidateQueries({ queryKey: ["items"] });
  void queryClient.invalidateQueries({ queryKey: ["item", id] });
}

export async function stopTask(queryClient: QueryClient, id: string): Promise<void> {
  const confirmed = await confirmAction({
    title: i18n.t("task.confirmStopTitle"),
    body: i18n.t("task.confirmStop"),
    confirmLabel: i18n.t("task.stop"),
    destructive: true,
  });
  if (!confirmed) return;
  try {
    await api.cancelExecution(id);
    pushToast(i18n.t("task.stopped"), "default");
    refresh(queryClient, id);
  } catch (error) {
    toastError(error);
  }
}

export async function setTaskStatus(queryClient: QueryClient, id: string, status: WorkItemStatus): Promise<boolean> {
  if (status === "closed") {
    const confirmed = await confirmAction({
      title: i18n.t("item.confirmArchiveTitle"),
      body: i18n.t("item.confirmArchive"),
      confirmLabel: i18n.t("item.archive"),
    });
    if (!confirmed) return false;
  }
  try {
    await api.setWorkItemStatus(id, status);
    refresh(queryClient, id);
    return true;
  } catch (error) {
    toastError(error);
    return false;
  }
}

export async function revealWorkspace(id: string): Promise<void> {
  try {
    await api.revealArtifact(id, "");
  } catch (error) {
    toastError(error);
  }
}

export interface TaskTarget {
  id: string;
  title: string;
  running: boolean;
  status: WorkItemStatus;
}

const TASK_ICONS: Record<TaskAction, MenuEntry["icon"]> = {
  open: Maximize2,
  stop: Square,
  done: Check,
  archive: Archive,
  reveal: FolderOpen,
};

const TASK_LABELS = {
  open: "task.open",
  stop: "task.stop",
  done: "item.markDone",
  archive: "item.archive",
  reveal: "menu.revealWorkspace",
} as const satisfies Record<TaskAction, string>;

export function taskMenuEntries(target: TaskTarget, desktop = boot().desktop): MenuEntry[] {
  return taskActions({ desktop, running: target.running, status: target.status }).map((action) => ({
    id: action,
    label: i18n.t(TASK_LABELS[action]),
    icon: TASK_ICONS[action],
    danger: action === "stop",
  }));
}

/** The task menu, from a right click or a "⋯" button. `onopen` defaults to the focus panel. */
export function openTaskMenu(
  queryClient: QueryClient,
  target: TaskTarget,
  placement: MenuPlacement,
  onopen: (id: string) => void = (id) => navigate(focusHref(id)),
): void {
  openMenu(placement, i18n.t("menu.taskLabel", { title: target.title }), taskMenuEntries(target), (choice) => {
    const action = choice as TaskAction;
    if (action === "open") onopen(target.id);
    else if (action === "stop") void stopTask(queryClient, target.id);
    else if (action === "done") void setTaskStatus(queryClient, target.id, "done");
    else if (action === "archive") void setTaskStatus(queryClient, target.id, "closed");
    else void revealWorkspace(target.id);
  });
}
