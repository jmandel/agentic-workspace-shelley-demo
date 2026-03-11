// --- Manager-level types ---

export interface LocalToolCommand {
  name: string;
  command: string;
}

export interface LocalTool {
  name: string;
  kind: string;
  exposure: string;
  description: string;
  commands: LocalToolCommand[];
  guidance?: string;
  requirements?: string[];
  version?: string;
}

// --- Workspace types ---

export interface WorkspaceSummary {
  id?: string;
  namespace?: string;
  name: string;
  status: string;
  api?: string;
  createdAt?: string;
}

export interface WorkspaceTopicRef {
  name: string;
  events?: string;
  shelley?: string;
}

export interface WorkspaceRuntimeInfo {
  localTools?: LocalTool[];
}

export interface WorkspaceDetail extends WorkspaceSummary {
  topics?: WorkspaceTopicRef[];
  runtime?: WorkspaceRuntimeInfo;
}

export interface CreateWorkspaceRequest {
  name: string;
  template?: string;
  topics?: { name: string }[];
  runtime?: {
    localTools?: string[];
  };
}

export interface PatchWorkspaceRequest {
  runtime?: {
    localTools?: string[];
  };
}

// --- Topic types ---

export interface TopicInfo {
  name: string;
  clients?: number;
  busy?: boolean;
  createdAt?: string;
}

// --- Queue types ---

export interface QueueSubmitter {
  id: string;
  displayName?: string;
}

export interface QueueEntry {
  promptId: string;
  text: string;
  status: string;
  position?: number;
  submittedBy?: QueueSubmitter;
}

export interface QueueSnapshot {
  activePromptId: string;
  entries: QueueEntry[];
}

// --- WebSocket message types ---

export type TopicMessageType =
  | "authenticated"
  | "connected"
  | "prompt_status"
  | "queue_snapshot"
  | "queue_entry_updated"
  | "queue_entry_moved"
  | "queue_entry_removed"
  | "queue_cleared"
  | "user"
  | "text"
  | "tool_call"
  | "tool_update"
  | "system"
  | "error"
  | "done"
  | "inject_status";

export interface TopicMessage {
  type: TopicMessageType;
  actor?: QueueSubmitter;
  data?: string;
  promptId?: string;
  status?: string;
  position?: number;
  title?: string;
  tool?: string;
  toolCallId?: string;
  kind?: string;
  submittedBy?: QueueSubmitter;
  // queue_snapshot fields
  activePromptId?: string;
  entries?: QueueEntry[];
  // tool_call raw input (ACP-aligned)
  rawInput?: Record<string, unknown>;
  // inject/interrupt fields (RFC 0007)
  injectId?: string;
  injected?: boolean;
  reason?: string;
  interruptedBy?: QueueSubmitter;
}

// --- Manager lifecycle event types (RFC 0009) ---

export type ManagerEventType =
  | "authenticated"
  | "connected"
  | "workspace_created"
  | "workspace_deleted"
  | "workspace_status_changed"
  | "topic_created"
  | "topic_deleted";

export interface ManagerEvent {
  type: ManagerEventType;
  actor?: QueueSubmitter;
  eventId?: string;
  timestamp?: string;
  replay?: boolean;
  protocolVersion?: string;
  namespace?: string;
  workspace?:
    | string
    | {
        name: string;
        status?: string;
        previousStatus?: string;
        template?: string;
        createdAt?: string;
        topics?: { name: string }[];
      };
  topic?: string | { name: string };
}

// --- Tool registration types ---

export interface RegisterToolRequest {
  name: string;
  description?: string;
  provider?: string;
  protocol?: string;
  transport: {
    type: string;
    command?: string;
    args?: string[];
    cwd?: string;
    env?: Record<string, string>;
    endpoint?: string;
    url?: string;
    headers?: Record<string, string>;
  };
  tools?: {
    name: string;
    title?: string;
    description?: string;
    inputSchema?: Record<string, unknown>;
  }[];
}

export interface GrantRequest {
  subject: string;
  tools: string[];
  access: string;
  approvers: string[];
  scope: Record<string, unknown>;
}
