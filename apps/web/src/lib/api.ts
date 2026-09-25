export interface HidaneEvent {
  seq: number;
  id: string;
  ts: string;
  source: string;
  kind: string;
  threadId: string | null;
  workItemId: string | null;
  executionId: string | null;
  payload: Record<string, unknown>;
  /** Agent loop the event was also a message to, if any. */
  mailbox?: string | null;
  causedBy?: string | null;
  hop?: number;
}

export type WorkItemStatus = "open" | "done" | "closed";

export interface EventsPage {
  events: HidaneEvent[];
  hasMore: boolean;
  /** Only false when the page reaches the live edge. */
  hasNewer?: boolean;
  oldestSeq: number | null;
  newestSeq?: number | null;
  /** Titles of the work items a conversation page mentions. */
  titles?: Record<string, string>;
}

export interface ConversationDay {
  day: string;
  count: number;
  firstId: string;
}

/** Base64 payload for the vision model — same shape the Feishu connector sends. */
export interface OutboundImage {
  data: string;
  mimeType: string;
}

export interface MemoryEntry {
  kind: "fact" | "preference" | "decision" | "lesson";
  content: string;
  date: string;
  id: string;
}

export interface WorkItem {
  id: string;
  title: string;
  status: WorkItemStatus;
  workspace: string;
  threadId: string;
  parentId: string | null;
  deadlineAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export type CardState = "waiting" | "running" | "queued" | "thinking" | "delegated" | "idle" | "done" | "closed";

/** One task card: the board projection's view of a work item. */
export interface BoardCard {
  item: WorkItem;
  state: CardState;
  understanding: string | null;
  lastReply: { text: string; ts: string; seq: number } | null;
  execution: {
    id: string;
    status: string;
    startedAt: string | null;
    instructions: string;
    toolCalls: number;
    lastTool: string | null;
  } | null;
  escalation: { id: string; question: string; reason: string; path: EscalationStep[]; ts: string } | null;
  lastPolicyBlock: { reason: string; ts: string } | null;
  anchor: string | null;
  childIds: string[];
  lastSeq: number;
}

export interface EscalationStep {
  workItemId: string;
  title: string;
  tried: string;
}

export interface Execution {
  id: string;
  workItemId: string;
  owner: string;
  status: "queued" | "running" | "done" | "failed" | "cancelled" | "lost";
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
}

export interface PolicyRule {
  id: string;
  pattern: string;
  reason: string;
  tools?: string[];
}

export interface Schedule {
  id: string;
  name: string;
  action: "http" | "prompt";
  spec: {
    url?: string;
    method?: string;
    body?: string;
    wake?: boolean;
    prompt?: string;
  };
  cron: string | null;
  intervalSec: number | null;
  timezone: string | null;
  enabled: boolean;
  nextRunAt: string | null;
  lastRunAt: string | null;
  lastStatus: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface ScheduleInput {
  name: string;
  action: "http" | "prompt";
  spec: Schedule["spec"];
  cron?: string;
  intervalSec?: number;
  timezone?: string;
  enabled?: boolean;
}

export interface ArtifactEntry {
  path: string;
  size: number;
  modifiedAt: string;
}

export interface ArtifactContent {
  path: string;
  size: number;
  text?: string;
  reason?: "binary" | "too-large";
}

export interface StatusInfo {
  latestSeq: number;
  triageCursor: number;
  triageLag: number;
  lastHeartbeatAt: string | null;
  openWorkItems: number;
  model?: string;
  runtime?: {
    up: boolean;
    activeTurns: string[];
    pendingMailboxes: number;
    pendingMessages: number;
    workers: { running: number; queued: number };
  };
}

const TOKEN_KEY = "hidane-token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

/**
 * A rejected token used to strand the app: every query 401'd and each page
 * rendered blank with no way back to the prompt short of clearing storage.
 * The token is dropped and listeners return the user to the gate instead.
 */
const unauthorizedListeners = new Set<() => void>();

export function onUnauthorized(fn: () => void): () => void {
  unauthorizedListeners.add(fn);
  return () => unauthorizedListeners.delete(fn);
}

export function authHeaders(token = getToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      ...init,
      headers: {
        "content-type": "application/json",
        ...authHeaders(),
        ...(init?.headers ?? {}),
      },
    });
  } catch (err) {
    // A dead server is a 0, not an HTTP status — surface it as one error type.
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    if (res.status === 401) {
      clearToken();
      for (const fn of unauthorizedListeners) fn();
    }
    throw new ApiError(res.status, `${res.status} ${await res.text()}`);
  }
  return (await res.json()) as T;
}

export const api = {
  events: (params: Record<string, string | number | undefined>) => {
    const q = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== "") q.set(k, String(v));
    }
    return apiFetch<{ events: HidaneEvent[] }>(`/api/events?${q}`);
  },
  /** Cursor page walking backwards; omit `before` for the newest page.
   *  `around` opens a window centred on one event and `after` walks forwards
   *  from a seq — both for reading history far from the live edge.
   *  `kind` accepts a comma-separated list to fetch several kinds at once. */
  eventsPage: (params: {
    kind?: string;
    item?: string;
    thread?: string;
    /** Everything said on the main thread plus every answer, wherever written. */
    conversation?: boolean;
    /** With `conversation`: leave out answers to webhooks and schedules. */
    personOnly?: boolean;
    before?: number;
    after?: number;
    around?: string;
    limit?: number;
  }) => {
    const q = new URLSearchParams({ page: "1" });
    if (params.conversation) q.set("conversation", "1");
    if (params.personOnly) q.set("origin", "person");
    if (params.kind) q.set("kind", params.kind);
    if (params.item) q.set("item", params.item);
    if (params.thread) q.set("thread", params.thread);
    if (params.before !== undefined) q.set("before", String(params.before));
    if (params.after !== undefined) q.set("after", String(params.after));
    if (params.around !== undefined) q.set("around", params.around);
    q.set("limit", String(params.limit ?? 50));
    return apiFetch<EventsPage>(`/api/events?${q}`);
  },
  /** Everything ever said, newest first — not only what has been loaded. */
  searchConversation: (query: string, before?: number, limit = 20) => {
    const q = new URLSearchParams({ q: query, limit: String(limit) });
    if (before !== undefined) q.set("before", String(before));
    return apiFetch<{
      events: HidaneEvent[];
      hasMore: boolean;
      items: WorkItem[];
      titles: Record<string, string>;
    }>(`/api/conversation/search?${q}`);
  },
  /** Days anything was said, newest first, in the reader's timezone. */
  conversationDays: (timeZone: string) =>
    apiFetch<{ days: ConversationDay[] }>(
      `/api/conversation/days?tz=${encodeURIComponent(timeZone)}`,
    ),
  /** Where the Primary's view of the conversation currently begins. */
  conversationContext: () =>
    apiFetch<{ fromId: string | null; turns: number }>(`/api/conversation/context`),
  /** Hide one of the person's messages from every reader; the log keeps the row. */
  redactMessage: (messageId: string) =>
    apiFetch<{ ok: boolean; eventId: string | null }>(`/api/messages/${messageId}/redact`, {
      method: "POST",
    }),
  workItems: (all = false) =>
    apiFetch<{ items: WorkItem[]; running: string[] }>(
      `/api/work-items${all ? "?all" : ""}`,
    ),
  workItem: (id: string, limit?: number) =>
    apiFetch<{
      item: WorkItem;
      events: HidaneEvent[];
      hasMore: boolean;
      running: boolean;
      execution: Execution | null;
      children: WorkItem[];
    }>(`/api/work-items/${id}${limit !== undefined ? `?limit=${limit}` : ""}`),
  /**
   * The one message door. `target` addresses a work item directly (no routing
   * guess); `replyTo` answers a specific event, e.g. an escalated question.
   */
  chat: (
    text: string,
    images: OutboundImage[] = [],
    opts: { target?: string; replyTo?: string; focus?: boolean } = {},
  ) =>
    apiFetch<{ ok: boolean; messageId: string }>(`/api/chat`, {
      method: "POST",
      body: JSON.stringify({ text, ...(images.length > 0 ? { images } : {}), ...opts }),
    }),
  /** Move a message to another work item, or answer "which one?". `"new"` makes one. */
  routeMessage: (messageId: string, workItemId: string) =>
    apiFetch<{ ok: boolean; workItemId: string }>(`/api/messages/${messageId}/route`, {
      method: "POST",
      body: JSON.stringify({ workItemId }),
    }),
  board: () => apiFetch<{ cards: BoardCard[] }>(`/api/board`),
  policies: () => apiFetch<{ path: string; rules: PolicyRule[] }>(`/api/policies`),
  addPolicy: (input: { pattern: string; reason: string; tools?: string[] }) =>
    apiFetch<{ ok: boolean; rule: PolicyRule }>(`/api/policies`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  deletePolicy: (id: string) =>
    apiFetch<{ ok: boolean }>(`/api/policies/${id}`, { method: "DELETE" }),
  worklog: (day: string) =>
    apiFetch<{ day: string; markdown: string; eventCount: number }>(`/api/worklog/${day}`),
  status: () => apiFetch<StatusInfo>(`/api/status`),
  setWorkItemStatus: (id: string, status: WorkItemStatus) =>
    apiFetch<{ ok: boolean; item: WorkItem }>(`/api/work-items/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    }),
  schedules: () => apiFetch<{ schedules: Schedule[] }>(`/api/schedules`),
  createSchedule: (input: ScheduleInput) =>
    apiFetch<{ ok: boolean; schedule: Schedule }>(`/api/schedules`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  updateSchedule: (id: string, patch: Partial<ScheduleInput>) =>
    apiFetch<{ ok: boolean; schedule: Schedule }>(`/api/schedules/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  deleteSchedule: (id: string) =>
    apiFetch<{ ok: boolean }>(`/api/schedules/${id}`, { method: "DELETE" }),
  runSchedule: (id: string) =>
    apiFetch<{ ok: boolean; status: string; schedule: Schedule }>(
      `/api/schedules/${id}/run`,
      { method: "POST" },
    ),
  createWorkItem: (input: { title: string; brief?: string; repo?: string; parentId?: string }) =>
    apiFetch<{ ok: boolean; item: WorkItem; dispatched: boolean }>(`/api/work-items`, {
      method: "POST",
      body: JSON.stringify(input),
    }),
  scheduleRuns: (id: string) =>
    apiFetch<{ runs: HidaneEvent[] }>(`/api/schedules/${id}/runs`),
  workItemFiles: (id: string) =>
    apiFetch<{ workspace: string; files: ArtifactEntry[] }>(`/api/work-items/${id}/files`),
  workItemFile: (id: string, path: string) =>
    apiFetch<ArtifactContent>(
      `/api/work-items/${id}/file?path=${encodeURIComponent(path)}`,
    ),
  cancelExecution: (id: string) =>
    apiFetch<{ ok: boolean; cancelled: string[] }>(`/api/work-items/${id}/cancel`, {
      method: "POST",
    }),
  addMemory: (kind: MemoryEntry["kind"], content: string) =>
    apiFetch<{ ok: boolean; entry: MemoryEntry }>(`/api/memories`, {
      method: "POST",
      body: JSON.stringify({ kind, content }),
    }),
  memories: () =>
    apiFetch<{ path: string; entries: MemoryEntry[]; markdown: string }>(`/api/memories`),
  forgetMemory: (id: string) =>
    apiFetch<{ ok: boolean }>(`/api/memories/${id}`, { method: "DELETE" }),
};

export function eventStreamUrl(): string {
  const token = getToken();
  return `/api/events/stream${token ? `?token=${encodeURIComponent(token)}` : ""}`;
}
