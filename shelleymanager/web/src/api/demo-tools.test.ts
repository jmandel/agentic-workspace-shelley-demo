import { describe, expect, test } from "bun:test";
import type {
  PatchWorkspaceRequest,
  RegisterToolRequest,
  WorkspaceDetail,
} from "./types";
import { buildHL7JiraToolRequest, registerDemoJiraToolWithAPI } from "./demo-tools";

function workspaceDetail(localTools: string[]): WorkspaceDetail {
  return {
    name: "bp-ig-fix",
    status: "running",
    runtime: {
      localTools: localTools.map((name) => ({
        name,
        kind: "local_tool",
        exposure: name === "hl7-jira-support" ? "support_bundle" : "bash_only",
        description: name,
        commands: [],
      })),
    },
  };
}

describe("buildHL7JiraToolRequest", () => {
  test("uses manager-brokered mounted support paths without preregistering MCP sub-tools", () => {
    const request = buildHL7JiraToolRequest();
    expect(request.transport.type).toBe("stdio");
    expect(request.transport.args).toEqual([
      "/tools/hl7-jira-support/bin/hl7-jira-mcp.js",
    ]);
    expect(request.transport.cwd).toBe("/tools/hl7-jira-support");
    expect(request.transport.env?.HL7_JIRA_DB).toBe(
      "/tools/hl7-jira-support/data/jira-data.db",
    );
    expect(request.tools).toBeUndefined();
  });
});

describe("registerDemoJiraToolWithAPI", () => {
  test("adds the Jira support bundle before registering the tool", async () => {
    const calls: {
      patch?: PatchWorkspaceRequest;
      register?: RegisterToolRequest;
      grantTools?: string[];
      granted?: boolean;
    } = {};

    await registerDemoJiraToolWithAPI(
      {
        async getWorkspace() {
          return workspaceDetail(["fhir-validator"]);
        },
        async patchWorkspace(_namespace, _workspace, req) {
          calls.patch = req;
          return workspaceDetail(["fhir-validator", "hl7-jira-support"]);
        },
        async registerTool(_namespace, _workspace, req) {
          calls.register = req;
        },
        async grantTool(_namespace, _workspace, _toolName, req) {
          calls.grantTools = req.tools;
          calls.granted = true;
        },
      },
      "acme",
      "bp-ig-fix",
    );

    expect(calls.patch).toEqual({
      runtime: { localTools: ["fhir-validator", "hl7-jira-support"] },
    });
    expect(calls.register?.transport.args).toEqual([
      "/tools/hl7-jira-support/bin/hl7-jira-mcp.js",
    ]);
    expect(calls.register?.transport.env?.HL7_JIRA_DB).toBe(
      "/tools/hl7-jira-support/data/jira-data.db",
    );
    expect(calls.register?.tools).toBeUndefined();
    expect(calls.grantTools).toEqual(["*"]);
    expect(calls.granted).toBeTrue();
  });

  test("does not patch the workspace when the support bundle is already present", async () => {
    let patched = false;

    await registerDemoJiraToolWithAPI(
      {
        async getWorkspace() {
          return workspaceDetail(["fhir-validator", "hl7-jira-support"]);
        },
        async patchWorkspace() {
          patched = true;
          return workspaceDetail(["fhir-validator", "hl7-jira-support"]);
        },
        async registerTool() {},
        async grantTool() {},
      },
      "acme",
      "bp-ig-fix",
    );

    expect(patched).toBeFalse();
  });
});
