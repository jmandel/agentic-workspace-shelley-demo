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

export interface TopicActor {
  id: string;
  displayName?: string;
}

export interface TopicRun {
  runId: string;
  state: "queued" | "running" | "completed" | "cancelled" | "failed";
  text?: string;
  createdAt?: string;
  position?: number;
  reason?: string;
  interruptible?: boolean;
  submittedBy?: TopicActor;
  interruptedBy?: TopicActor;
}

export interface WorkspaceTopicRef {
  name: string;
  activeRun?: TopicRun;
  queuedCount?: number;
  createdAt?: string;
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

export interface WorkspaceFileNode {
  path: string;
  name: string;
  kind: "file" | "directory";
  size: number;
  modifiedAt: string;
  mimeType?: string;
}

export interface WorkspaceFileListing {
  node: WorkspaceFileNode;
  entries?: WorkspaceFileNode[];
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
  activeRun?: TopicRun;
  queuedCount?: number;
  createdAt?: string;
}

export interface TopicState {
  name: string;
  activeRun?: TopicRun;
  queue: TopicRun[];
  createdAt?: string;
  events?: string;
}

// --- WebSocket message types ---

export type TopicMessageType =
  | "authenticated"
  | "connected"
  | "topic_state"
  | "run_updated"
  | "message"
  | "tool_call"
  | "tool_update"
  | "approval_request"
  | "error"
  | "inject_status"
  | "interrupt_status";

export interface TopicMessage {
  type: TopicMessageType;
  replay?: boolean;
  eventId?: string;
  timestamp?: string;
  actor?: TopicActor;
  data?: string;
  text?: string;
  role?: "user" | "assistant";
  runId?: string;
  state?: TopicRun["state"] | "accepted" | "rejected";
  status?: string;
  position?: number;
  title?: string;
  tool?: string;
  toolCallId?: string;
  kind?: string;
  submittedBy?: TopicActor;
  interruptedBy?: TopicActor;
  activeRun?: TopicRun;
  queue?: TopicRun[];
  interruptible?: boolean;
  rawInput?: Record<string, unknown>;
  injectId?: string;
  reason?: string;
  approvers?: string[];
  action?: string;
  approved?: boolean;
  approver?: string;
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
  actor?: TopicActor;
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
