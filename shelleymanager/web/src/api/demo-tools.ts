import type {
  PatchWorkspaceRequest,
  RegisterToolRequest,
  WorkspaceDetail,
} from "./types";
import * as api from "./client";

const jiraSupportBundle = "hl7-jira-support";

export interface DemoToolsAPI {
  getWorkspace: (namespace: string, workspace: string) => Promise<WorkspaceDetail>;
  patchWorkspace: (
    namespace: string,
    workspace: string,
    req: PatchWorkspaceRequest,
  ) => Promise<WorkspaceDetail>;
  registerTool: (
    namespace: string,
    workspace: string,
    req: RegisterToolRequest,
  ) => Promise<unknown>;
  grantTool: (
    namespace: string,
    workspace: string,
    toolName: string,
    req: {
      subject: string;
      tools: string[];
      access: string;
      approvers: string[];
      scope: Record<string, unknown>;
    },
  ) => Promise<unknown>;
}

export function buildHL7JiraToolRequest(): RegisterToolRequest {
  return {
    name: "hl7-jira",
    description: "Search and inspect issues from the real HL7 Jira SQLite snapshot",
    provider: "demo@acme.example",
    protocol: "mcp",
    transport: {
      type: "stdio",
      command: "bun",
      args: ["/tools/hl7-jira-support/bin/hl7-jira-mcp.js"],
      cwd: "/tools/hl7-jira-support",
      env: {
        HL7_JIRA_DB: "/tools/hl7-jira-support/data/jira-data.db",
      },
    },
  };
}

async function ensureHL7JiraSupportBundle(
  apiClient: DemoToolsAPI,
  namespace: string,
  workspace: string,
): Promise<WorkspaceDetail> {
  const detail = await apiClient.getWorkspace(namespace, workspace);
  const selected = new Set(
    detail.runtime?.localTools?.map((tool) => tool.name).filter(Boolean) ?? [],
  );
  if (selected.has(jiraSupportBundle)) {
    return detail;
  }
  selected.add(jiraSupportBundle);
  return apiClient.patchWorkspace(namespace, workspace, {
    runtime: { localTools: [...selected].sort() },
  });
}

export async function registerDemoJiraToolWithAPI(
  apiClient: DemoToolsAPI,
  namespace: string,
  workspace: string,
) {
  await ensureHL7JiraSupportBundle(apiClient, namespace, workspace);

  try {
    await apiClient.registerTool(namespace, workspace, buildHL7JiraToolRequest());
  } catch (err) {
    if (!(err instanceof Error) || !err.message.includes("409")) throw err;
  }

  try {
    await apiClient.grantTool(namespace, workspace, "hl7-jira", {
      subject: "agent:*",
      tools: ["*"],
      access: "allowed",
      approvers: [],
      scope: {},
    });
  } catch (err) {
    if (!(err instanceof Error) || !err.message.includes("409")) throw err;
  }
}

export async function registerDemoJiraTool(
  namespace: string,
  workspace: string,
) {
  return registerDemoJiraToolWithAPI(api, namespace, workspace);
}
