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
import { authorizationHeader } from "./auth";

const BASE = "";

async function request<T>(
  url: string,
  init?: RequestInit,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (init?.body && typeof init.body === "string") {
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(`${BASE}${url}`, {
    ...init,
    headers: { ...authorizationHeader(), ...headers, ...(init?.headers as Record<string, string>) },
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
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue`,
  );
}

export async function cancelQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
): Promise<void> {
  await request<unknown>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}`,
    { method: "DELETE" },
  );
}

export async function updateQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
  text: string,
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}`,
    { method: "PATCH", body: JSON.stringify({ data: text }) },
  );
}

export async function moveQueuedPrompt(
  namespace: string,
  workspace: string,
  topic: string,
  promptId: string,
  direction: "up" | "down" | "top" | "bottom",
): Promise<QueueSnapshot> {
  return request<QueueSnapshot>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue/${encodeURIComponent(promptId)}/move`,
    { method: "POST", body: JSON.stringify({ direction }) },
  );
}

export async function clearMyQueue(
  namespace: string,
  workspace: string,
  topic: string,
): Promise<{ removed: string[] }> {
  return request<{ removed: string[] }>(
    `${workspaceBase(namespace, workspace)}/topics/${encodeURIComponent(topic)}/queue:clear-mine`,
    { method: "POST" },
  );
}

// --- Managed tools ---

export interface EnabledTool {
  kind: "local" | "mcp";
  name: string;
  description?: string;
}

export async function listTools(
  namespace: string,
  workspace: string,
): Promise<EnabledTool[]> {
  return request<EnabledTool[]>(
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
): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/apis/v1/namespaces/${encodeURIComponent(namespace)}/events`;
}

export function topicWSURL(
  namespace: string,
  workspace: string,
  topic: string,
): string {
  const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${window.location.host}/apis/v1/namespaces/${encodeURIComponent(namespace)}/workspaces/${encodeURIComponent(workspace)}/topics/${encodeURIComponent(topic)}/events`;
}
