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
}

export type WorkItemStatus = "open" | "done" | "closed";

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
  createdAt: string;
  updatedAt: string;
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
  /** Cursor page walking backwards; omit `before` for the newest page. */
  eventsPage: (params: {
    kind?: string;
    item?: string;
    before?: number;
    limit?: number;
  }) => {
    const q = new URLSearchParams({ page: "1" });
    if (params.kind) q.set("kind", params.kind);
    if (params.item) q.set("item", params.item);
    if (params.before !== undefined) q.set("before", String(params.before));
    q.set("limit", String(params.limit ?? 50));
    return apiFetch<{ events: HidaneEvent[]; hasMore: boolean; oldestSeq: number | null }>(
      `/api/events?${q}`,
    );
  },
  workItems: (all = false) =>
    apiFetch<{ items: WorkItem[]; running: string[] }>(
      `/api/work-items${all ? "?all" : ""}`,
    ),
  workItem: (id: string) =>
    apiFetch<{ item: WorkItem; events: HidaneEvent[] }>(`/api/work-items/${id}`),
  chat: (text: string, images: OutboundImage[] = []) =>
    apiFetch<{ ok: boolean }>(`/api/chat`, {
      method: "POST",
      body: JSON.stringify(images.length > 0 ? { text, images } : { text }),
    }),
  threadMessage: (id: string, text: string) =>
    apiFetch<{ ok: boolean }>(`/api/work-items/${id}/messages`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
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
  createWorkItem: (input: { title: string; brief?: string; repo?: string }) =>
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
    apiFetch<{ ok: boolean; executionId: string | null }>(`/api/work-items/${id}/cancel`, {
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
