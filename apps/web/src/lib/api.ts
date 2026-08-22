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

export interface WorkItem {
  id: string;
  title: string;
  status: "open" | "done" | "closed";
  workspace: string;
  threadId: string;
  createdAt: string;
  updatedAt: string;
}

export interface StatusInfo {
  latestSeq: number;
  triageCursor: number;
  triageLag: number;
  lastHeartbeatAt: string | null;
  openWorkItems: number;
}

const TOKEN_KEY = "hidane-token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export function authHeaders(token = getToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: {
      "content-type": "application/json",
      ...authHeaders(),
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
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
  workItems: (all = false) =>
    apiFetch<{ items: WorkItem[] }>(`/api/work-items${all ? "?all" : ""}`),
  workItem: (id: string) =>
    apiFetch<{ item: WorkItem; events: HidaneEvent[] }>(`/api/work-items/${id}`),
  chat: (text: string) =>
    apiFetch<{ ok: boolean }>(`/api/chat`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
  threadMessage: (id: string, text: string) =>
    apiFetch<{ ok: boolean }>(`/api/work-items/${id}/messages`, {
      method: "POST",
      body: JSON.stringify({ text }),
    }),
  worklog: (day: string) =>
    apiFetch<{ day: string; markdown: string }>(`/api/worklog/${day}`),
  status: () => apiFetch<StatusInfo>(`/api/status`),
};

export function eventStreamUrl(): string {
  const token = getToken();
  return `/api/events/stream${token ? `?token=${encodeURIComponent(token)}` : ""}`;
}
