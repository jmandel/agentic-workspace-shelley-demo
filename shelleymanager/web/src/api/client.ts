import type {
  LocalTool,
  WorkspaceSummary,
  WorkspaceDetail,
  CreateWorkspaceRequest,
  PatchWorkspaceRequest,
  QueueSnapshot,
  RegisterToolRequest,
  GrantRequest,
} from "./types";

const BASE = "";

async function request<T>(
  url: string,
  init?: RequestInit,
  clientId?: string,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (init?.body && typeof init.body === "string") {
    headers["Content-Type"] = "application/json";
  }
  if (clientId) {
    headers["X-Workspace-Client-ID"] = clientId;
  }
  const res = await fetch(`${BASE}${url}`, {
    ...init,
    headers: { ...headers, ...(init?.headers as Record<string, string>) },
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status} ${text.trim() || res.statusText}`);
  }
  const text = await res.text();
  if (!text) return {} as T;
  return JSON.parse(text) as T;
}

function namespacedBase(namespace: string): string {
  return `/apis/v1/namespaces/${encodeURIComponent(namespace)}/workspaces`;
}

function workspaceBase(namespace: string, workspace: string): string {
  return `${namespacedBase(namespace)}/${encodeURIComponent(workspace)}`;
}

// --- Local tools ---

export async function fetchLocalTools(): Promise<LocalTool[]> {
  return request<LocalTool[]>("/apis/v1/local-tools");
}

// --- Workspaces ---

export async function listWorkspaces(
  namespace: string,
): Promise<WorkspaceSummary[]> {
  return request<WorkspaceSummary[]>(namespacedBase(namespace));
}

export async function getWorkspace(
  namespace: string,
  name: string,
): Promise<WorkspaceDetail> {
  return request<WorkspaceDetail>(workspaceBase(namespace, name));
}

export async function createWorkspace(
  namespace: string,
  req: CreateWorkspaceRequest,
): Promise<WorkspaceDetail> {
  return request<WorkspaceDetail>(namespacedBase(namespace), {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function patchWorkspace(
  namespace: string,
  name: string,
  req: PatchWorkspaceRequest,
): Promise<WorkspaceDetail> {
  return request<WorkspaceDetail>(workspaceBase(namespace, name), {
    method: "PATCH",
    body: JSON.stringify(req),
  });
}

export async function deleteWorkspace(
  namespace: string,
  name: string,
): Promise<void> {
  await request<unknown>(workspaceBase(namespace, name), { method: "DELETE" });
}

// --- Topics ---

export async function createTopic(
  namespace: string,
  workspace: string,
  topicName: string,
): Promise<unknown> {
  return request<unknown>(`${workspaceBase(namespace, workspace)}/topics`, {
    method: "POST",
    body: JSON.stringify({ name: topicName }),
  });
}

export async function deleteTopic(
  namespace: string,
  workspace: string,
  topicName: string,
): Promise<void> {
  await request<unknown>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topicName)}`,
    { method: "DELETE" },
  );
}

// --- Queue ---

export async function fetchQueue(
  namespace: string,
  workspace: string,
  topic: string,
  clientId?: string,
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue`,
    undefined,
    clientId,
  );
}

export async function cancelQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
  clientId?: string,
): Promise<void> {
  await request<unknown>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}`,
    { method: "DELETE" },
    clientId,
  );
}

export async function updateQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
  text: string,
  clientId?: string,
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}`,
    { method: "PATCH", body: JSON.stringify({ data: text }) },
    clientId,
  );
}

export async function moveQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
  direction: "up" | "down" | "top" | "bottom",
  clientId?: string,
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}/move`,
    { method: "POST", body: JSON.stringify({ direction }) },
    clientId,
  );
}

export async function clearMyQueue(
  namespace: string,
  workspace: string,
  topic: string,
  clientId?: string,
): Promise<{ removed: string[] }> {
  return request<{ removed: string[] }>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue:clear-mine`,
    { method: "POST" },
    clientId,
  );
}

// --- Managed tools ---

export interface ManagedToolDef {
  name: string;
  description?: string;
}

export interface ManagedTool {
  name: string;
  description?: string;
  protocol?: string;
  provider?: string;
  tools?: ManagedToolDef[];
}

export async function listTools(
  namespace: string,
  workspace: string,
): Promise<ManagedTool[]> {
  return request<ManagedTool[]>(
    `${workspaceBase(namespace, workspace)}/tools`,
  );
}

// --- Tool registration (for demo setup) ---

export async function registerTool(
  namespace: string,
  workspace: string,
  req: RegisterToolRequest,
): Promise<unknown> {
  return request<unknown>(`${workspaceBase(namespace, workspace)}/tools`, {
    method: "POST",
    body: JSON.stringify(req),
  });
}

export async function grantTool(
  namespace: string,
  workspace: string,
  toolName: string,
  req: GrantRequest,
): Promise<unknown> {
  return request<unknown>(
    `${workspaceBase(namespace, workspace)}/tools/${encodeURIComponent(toolName)}/grants`,
    { method: "POST", body: JSON.stringify(req) },
  );
}

// --- WebSocket URL builder ---

export function eventsWSURL(
  namespace: string,
  clientId: string,
): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/acp/${encodeURIComponent(namespace)}/events?client_id=${encodeURIComponent(clientId)}`;
}

export function topicWSURL(
  namespace: string,
  workspace: string,
  topic: string,
  clientId: string,
): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/acp/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}/topics/${encodeURIComponent(topic)}?client_id=${encodeURIComponent(clientId)}`;
}
