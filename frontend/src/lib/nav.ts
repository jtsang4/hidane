import AlarmClock from "@lucide/svelte/icons/alarm-clock";
import Brain from "@lucide/svelte/icons/brain";
import Inbox from "@lucide/svelte/icons/inbox";
import ListTodo from "@lucide/svelte/icons/list-todo";
import MessageCircle from "@lucide/svelte/icons/message-circle";
import ScrollText from "@lucide/svelte/icons/scroll-text";
import type { Command } from "./commands.js";

/** The six pages of the main window, in sidebar and ⌘1–⌘6 order. */
export const MAIN_NAV = [
  { to: "/", key: "nav.chat", command: "go:chat", icon: MessageCircle },
  { to: "/inbox", key: "nav.inbox", command: "go:inbox", icon: Inbox },
  { to: "/items", key: "nav.items", command: "go:items", icon: ListTodo },
  { to: "/schedules", key: "nav.schedules", command: "go:schedules", icon: AlarmClock },
  { to: "/memory", key: "nav.memory", command: "go:memory", icon: Brain },
  { to: "/log", key: "nav.log", command: "go:log", icon: ScrollText },
] as const satisfies readonly { to: string; key: string; command: Command; icon: unknown }[];

export function navTarget(command: Command): string | null {
  return MAIN_NAV.find((item) => item.command === command)?.to ?? null;
}

/** Plain left clicks are handled in-app; modified clicks keep the browser's meaning. */
export function plainClick(event: MouseEvent): boolean {
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
}
