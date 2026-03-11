import { create } from "zustand";
import type {
  LocalTool,
  WorkspaceDetail,
  QueueSnapshot,
  QueueEntry,
  TopicMessage,
  ManagerEvent,
} from "@/api/types";
import * as api from "@/api/client";
import { loadClientIdentity, updateClientDisplayName } from "@/api/auth";
import { connectAuthenticatedSocket } from "@/api/socket";

const EVENTS_RECONNECT_DELAY_MS = 3000;
const TOPIC_RECONNECT_DELAY_MS = 1500;

// ---------------------------------------------------------------------------
// Chat message (accumulated from WebSocket events)
// ---------------------------------------------------------------------------

export interface ChatMessage {
  id: string;
  kind: "system" | "user" | "assistant" | "error" | "tool" | "interrupted";
  label: string;
  body: string;
  ts: number;
}

let _msgSeq = 0;
function msgId(): string {
  return `m${++_msgSeq}`;
}

// ---------------------------------------------------------------------------
// Connection status
// ---------------------------------------------------------------------------

export type ConnectionStatus = "idle" | "connecting" | "connected" | "disconnected";

// ---------------------------------------------------------------------------
// Store shape
// ---------------------------------------------------------------------------

interface AppState {
  // --- Participant identity (localStorage-backed) ---
  participantName: string;
  participantSubject: string;
  setParticipantName: (name: string) => void;

  // --- Namespace (discovered from manager /health) ---
  namespace: string;
  namespaceLoaded: boolean;
  fetchHealth: () => Promise<void>;

  // --- Local tools catalog ---
  localTools: LocalTool[];
  localToolsLoaded: boolean;
  selectedLocalTools: Set<string>;
  fetchLocalTools: () => Promise<void>;
  toggleLocalTool: (name: string) => void;

  // --- Workspaces ---
  workspaces: WorkspaceDetail[];
  workspacesLoading: boolean;
  fetchWorkspaces: () => Promise<void>;
  fetchWorkspaceDetail: (name: string) => Promise<WorkspaceDetail>;
  deleteWorkspace: (name: string) => Promise<void>;
  deleteTopic: (workspace: string, topic: string) => Promise<void>;
  createTopic: (workspace: string, topic: string) => Promise<void>;

  // --- Manager events (RFC 0009) ---
  _eventsWs: WebSocket | null;
  eventsStatus: ConnectionStatus;
  connectManagerEvents: (namespace: string) => void;
  disconnectManagerEvents: () => void;

  // --- Topic connection ---
  topicConnection: {
    namespace: string;
    workspace: string;
    topic: string;
  } | null;
  connectionStatus: ConnectionStatus;
  turnActive: boolean;
  messages: ChatMessage[];
  queue: QueueSnapshot;
  _ws: WebSocket | null;
  _promptCounter: number;
  _injectCounter: number;

  connectTopic: (namespace: string, workspace: string, topic: string) => void;
  disconnectTopic: () => void;
  sendPrompt: (text: string) => void;
  sendInject: (text: string) => void;
  sendInterrupt: () => void;
  injectFromQueue: (promptId: string) => Promise<void>;
  pushMessage: (kind: ChatMessage["kind"], label: string, body: string) => void;
  refreshQueue: () => Promise<void>;
}

// ---------------------------------------------------------------------------
// Participant persistence
// ---------------------------------------------------------------------------

const initialIdentity = loadClientIdentity();

// ---------------------------------------------------------------------------
// Empty queue sentinel
// ---------------------------------------------------------------------------

const EMPTY_QUEUE: QueueSnapshot = { activePromptId: "", entries: [] };

function normalizeQueue(raw: Partial<QueueSnapshot> | undefined): QueueSnapshot {
  return {
    activePromptId: raw?.activePromptId ?? "",
    entries: Array.isArray(raw?.entries) ? (raw.entries as QueueEntry[]) : [],
  };
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

export const useStore = create<AppState>((set, get) => ({
  // --- Participant ---
  participantName: initialIdentity.displayName,
  participantSubject: initialIdentity.subject,
  setParticipantName: (raw: string) => {
    const identity = updateClientDisplayName(raw);
    set({ participantName: identity.displayName, participantSubject: identity.subject });
  },

  // --- Namespace ---
  namespace: "default",
  namespaceLoaded: false,
  fetchHealth: async () => {
    if (get().namespaceLoaded) return;
    try {
      const res = await fetch("/health");
      if (res.ok) {
        const data = await res.json();
        if (data.namespace) {
          set({ namespace: data.namespace, namespaceLoaded: true });
        }
      }
    } catch {
      // Fall back to "default"
    }
    set({ namespaceLoaded: true });
  },

  // --- Local tools ---
  localTools: [],
  localToolsLoaded: false,
  selectedLocalTools: new Set<string>(),
  fetchLocalTools: async () => {
    if (get().localToolsLoaded) return;
    const tools = await api.fetchLocalTools();
    const preferred = tools?.find((tool) => tool.exposure !== "support_bundle");
    const selected = preferred ? new Set([preferred.name]) : new Set<string>();
    set({ localTools: tools ?? [], localToolsLoaded: true, selectedLocalTools: selected });
  },
  toggleLocalTool: (name: string) => {
    const prev = get().selectedLocalTools;
    const next = new Set(prev);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    set({ selectedLocalTools: next });
  },

  // --- Workspaces ---
  workspaces: [],
  workspacesLoading: false,
  fetchWorkspaces: async () => {
    const { namespace } = get();
    set({ workspacesLoading: true });
    try {
      const list = await api.listWorkspaces(namespace);
      const details = await Promise.all(
        (list ?? []).map(async (ws) => {
          try {
            return await api.getWorkspace(namespace, ws.name);
          } catch {
            return ws as WorkspaceDetail;
          }
        }),
      );
      set({ workspaces: details });
    } finally {
      set({ workspacesLoading: false });
    }
  },
  fetchWorkspaceDetail: async (name: string) => {
    const { namespace } = get();
    const detail = await api.getWorkspace(namespace, name);
    set((state) => ({
      workspaces: state.workspaces.map((ws) =>
        ws.name === name ? detail : ws,
      ),
    }));
    return detail;
  },
  deleteWorkspace: async (name: string) => {
    const { namespace } = get();
    await api.deleteWorkspace(namespace, name);
    set((state) => ({
      workspaces: state.workspaces.filter((ws) => ws.name !== name),
    }));
  },
  deleteTopic: async (workspace: string, topic: string) => {
    const { namespace } = get();
    await api.deleteTopic(namespace, workspace, topic);
    set((state) => ({
      workspaces: state.workspaces.map((ws) =>
        ws.name === workspace
          ? { ...ws, topics: ws.topics?.filter((t) => t.name !== topic) }
          : ws,
      ),
    }));
  },
  createTopic: async (workspace: string, topic: string) => {
    const { namespace } = get();
    await api.createTopic(namespace, workspace, topic);
    // Refresh the workspace detail to get updated topics list
    await get().fetchWorkspaceDetail(workspace);
  },

  // --- Manager events (RFC 0009) ---
  _eventsWs: null,
  eventsStatus: "idle" as ConnectionStatus,

  connectManagerEvents: (namespace: string) => {
    const prev = get()._eventsWs;
    if (prev) prev.close();

    const url = api.eventsWSURL(namespace);
    connectAuthenticatedSocket<ManagerEvent>({
      url,
      reconnectDelayMs: EVENTS_RECONNECT_DELAY_MS,
      getActiveSocket: () => get()._eventsWs,
      setActiveSocket: (ws) => set({ _eventsWs: ws }),
      setStatus: (eventsStatus) => set({ eventsStatus }),
      shouldReconnect: () => get().namespaceLoaded,
      reconnect: () => get().connectManagerEvents(get().namespace),
      onConnected: () => {
        get().fetchWorkspaces();
      },
      onMessage: (msg) => {
      // Skip replay events — we already fetched the full list above.
        if (msg.replay) return;

        const wsName = typeof msg.workspace === "string"
          ? msg.workspace
          : msg.workspace?.name;
        const topicName = typeof msg.topic === "string"
          ? msg.topic
          : msg.topic?.name;

        switch (msg.type) {
          case "workspace_created": {
            if (!wsName) break;
            const exists = get().workspaces.some((w) => w.name === wsName);
            if (!exists) {
              const wsObj = typeof msg.workspace === "object" ? msg.workspace : undefined;
              const stub: WorkspaceDetail = {
                name: wsName,
                status: wsObj?.status ?? "running",
                createdAt: wsObj?.createdAt,
                topics: wsObj?.topics?.map((t) => ({ name: t.name })),
              };
              set((state) => ({ workspaces: [...state.workspaces, stub] }));
            }
            // Fetch full detail (has runtime info, etc.).
            get().fetchWorkspaceDetail(wsName).catch(() => {});
            break;
          }

          case "workspace_deleted":
            if (wsName) {
              set((state) => ({
                workspaces: state.workspaces.filter((w) => w.name !== wsName),
              }));
            }
            break;

          case "workspace_status_changed": {
            if (!wsName) break;
            const wsObj = typeof msg.workspace === "object" ? msg.workspace : undefined;
            const status = wsObj?.status;
            if (status) {
              set((state) => ({
                workspaces: state.workspaces.map((w) =>
                  w.name === wsName ? { ...w, status } : w,
                ),
              }));
            }
            break;
          }

          case "topic_created":
            if (wsName && topicName) {
              set((state) => ({
                workspaces: state.workspaces.map((w) =>
                  w.name === wsName
                    ? {
                        ...w,
                        topics: w.topics?.some((t) => t.name === topicName)
                          ? w.topics
                          : [...(w.topics ?? []), { name: topicName }],
                      }
                    : w,
                ),
              }));
            }
            break;

          case "topic_deleted":
            if (wsName && topicName) {
              set((state) => ({
                workspaces: state.workspaces.map((w) =>
                  w.name === wsName
                    ? { ...w, topics: w.topics?.filter((t) => t.name !== topicName) }
                    : w,
                ),
              }));
            }
            break;
        }
      },
    });
  },

  disconnectManagerEvents: () => {
    const ws = get()._eventsWs;
    if (ws) ws.close();
    set({ _eventsWs: null, eventsStatus: "idle" });
  },

  // --- Topic connection ---
  topicConnection: null,
  connectionStatus: "idle",
  turnActive: false,
  messages: [],
  queue: EMPTY_QUEUE,
  _ws: null,
  _promptCounter: 0,
  _injectCounter: 0,

  connectTopic: (namespace: string, workspace: string, topic: string) => {
    // Close any existing connection
    const prev = get()._ws;
    if (prev) {
      prev.close();
    }

    set({
      topicConnection: { namespace, workspace, topic },
      connectionStatus: "connecting",
      turnActive: false,
      messages: [],
      queue: EMPTY_QUEUE,
      _promptCounter: 0,
      _injectCounter: 0,
    });

    const url = api.topicWSURL(namespace, workspace, topic);
    connectAuthenticatedSocket<TopicMessage>({
      url,
      reconnectDelayMs: TOPIC_RECONNECT_DELAY_MS,
      getActiveSocket: () => get()._ws,
      setActiveSocket: (_ws) => set({ _ws }),
      setStatus: (connectionStatus) => set({ connectionStatus }),
      shouldReconnect: () => {
        const current = get().topicConnection;
        return current?.namespace === namespace &&
          current?.workspace === workspace &&
          current?.topic === topic;
      },
      reconnect: () => get().connectTopic(namespace, workspace, topic),
      onConnected: () => {
        get().refreshQueue();
      },
      onMessage: (msg) => {
        const { pushMessage } = get();
        switch (msg.type) {
        case "prompt_status":
          if (msg.status === "started") set({ turnActive: true });
          if (msg.status === "completed" || msg.status === "failed" || msg.status === "cancelled") {
            set({ turnActive: false });
          }
          get().refreshQueue();
          break;
        case "queue_snapshot": {
          const q = normalizeQueue(msg);
          set({ queue: q, turnActive: !!msg.activePromptId });
          break;
        }
        case "queue_entry_updated":
        case "queue_entry_moved":
        case "queue_entry_removed":
        case "queue_cleared":
          get().refreshQueue();
          break;
        case "user":
          pushMessage(
            "user",
            msg.submittedBy?.displayName ?? msg.submittedBy?.id ?? "User",
            msg.data ?? "",
          );
          break;
        case "text":
          // Append to the last assistant message if streaming, otherwise create new
          set((state) => {
            const last = state.messages[state.messages.length - 1];
            if (last?.kind === "assistant") {
              return {
                messages: [
                  ...state.messages.slice(0, -1),
                  { ...last, body: last.body + (msg.data ?? "") },
                ],
              };
            }
            return {
              messages: [
                ...state.messages,
                { id: msgId(), kind: "assistant" as const, label: "Shelley", body: msg.data ?? "", ts: Date.now() },
              ],
            };
          });
          break;
        case "tool_call": {
          let toolBody = msg.status ?? "pending";
          if (msg.rawInput) {
            try {
              const input = msg.rawInput;
              // For bash-like tools, show the command directly
              if (typeof input.command === "string") {
                toolBody = input.command;
              } else {
                toolBody = JSON.stringify(input);
              }
            } catch { /* fall back to status */ }
          }
          pushMessage("tool", msg.title ?? msg.tool ?? "Tool", toolBody);
          break;
        }
        case "tool_update":
          pushMessage(
            "tool",
            "Tool Update",
            `${msg.title ?? msg.tool ?? ""} · ${msg.status ?? ""}${msg.data ? `\n\n${msg.data}` : ""}`,
          );
          break;
        case "system":
          // System events (e.g. "thinking...") are reflected by turnActive state
          break;
        case "error":
          pushMessage("error", "Error", msg.data ?? "Unknown error");
          break;
        case "done":
          if (msg.status === "interrupted") {
            const who = msg.interruptedBy?.displayName ?? msg.interruptedBy?.id ?? "someone";
            pushMessage("interrupted", "Interrupted", msg.reason ? `${who}: ${msg.reason}` : `Stopped by ${who}`);
          }
          // Normal turn completion is reflected by turnActive state
          set({ turnActive: false });
          get().refreshQueue();
          break;
        case "inject_status":
          // Inject failures surface as errors; success is silent
          if (msg.status === "rejected") {
            pushMessage("error", "Inject", `Injection rejected (no active turn)`);
          }
          break;
        default:
          // Unknown event types — ignore silently
          break;
        }
      },
    });
  },

  disconnectTopic: () => {
    const ws = get()._ws;
    if (ws) ws.close();
    set({
      topicConnection: null,
      connectionStatus: "idle",
      turnActive: false,
      messages: [],
      queue: EMPTY_QUEUE,
      _ws: null,
    });
  },

  sendPrompt: (text: string) => {
    const ws = get()._ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const counter = get()._promptCounter + 1;
    set({ _promptCounter: counter });
    ws.send(
      JSON.stringify({
        type: "prompt",
        data: text,
      }),
    );
  },

  sendInject: (text: string) => {
    const ws = get()._ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    const counter = get()._injectCounter + 1;
    set({ _injectCounter: counter });
    ws.send(
      JSON.stringify({
        type: "inject",
        data: text,
      }),
    );
  },

  sendInterrupt: () => {
    const ws = get()._ws;
    if (!ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(
      JSON.stringify({
        type: "interrupt",
        reason: "Stopped by user",
      }),
    );
  },

  injectFromQueue: async (promptId: string) => {
    const conn = get().topicConnection;
    if (!conn) return;
    // Find the entry text before cancelling
    const entry = get().queue.entries.find((e) => e.promptId === promptId);
    const text = entry?.text ?? "";
    // Cancel the queue entry
    await api.cancelQueuedPrompt(
      conn.namespace, conn.workspace, conn.topic, promptId,
    );
    // Inject its text into the active turn
    if (text) {
      get().sendInject(text);
    }
    get().refreshQueue();
  },

  pushMessage: (kind, label, body) => {
    set((state) => ({
      messages: [
        ...state.messages,
        { id: msgId(), kind, label, body, ts: Date.now() },
      ],
    }));
  },

  refreshQueue: async () => {
    const conn = get().topicConnection;
    if (!conn) return;
    try {
      const snapshot = await api.fetchQueue(
        conn.namespace,
        conn.workspace,
        conn.topic,
      );
      const q = normalizeQueue(snapshot);
      set({ queue: q, turnActive: !!snapshot?.activePromptId });
    } catch {
      // Queue refresh failures are non-fatal
    }
  },
}));
