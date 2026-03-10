import { useState } from "react";
import { useLocation } from "wouter";
import { useStore } from "@/store";
import * as api from "@/api/client";
import { LocalToolsPicker } from "./LocalToolsPicker";

interface Props {
  onCreated: () => void;
}

export function CreateWorkspaceForm({ onCreated }: Props) {
  const namespace = useStore((s) => s.namespace);
  const localTools = useStore((s) => s.localTools);
  const selectedTools = useStore((s) => s.selectedLocalTools);
  const toggleTool = useStore((s) => s.toggleLocalTool);

  const [, navigate] = useLocation();
  const [name, setName] = useState("");
  const [topic, setTopic] = useState("");
  const [template, setTemplate] = useState("");
  const [jiraEnabled, setJiraEnabled] = useState(false);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setStatus("Creating workspace...");

    try {
      await api.createWorkspace(namespace, {
        name,
        template: template || undefined,
        topics: topic ? [{ name: topic }] : undefined,
        runtime: { localTools: [...selectedTools] },
      });

      if (jiraEnabled) {
        setStatus("Registering HL7 Jira MCP tool...");
        await registerDemoJiraTool(namespace, name);
      }

      setStatus("Workspace created.");
      onCreated();
      if (topic) {
        navigate(
          `/app/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/${encodeURIComponent(topic)}`,
        );
      }
    } catch (err) {
      setStatus(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={handleSubmit}>
      <label htmlFor="ws-name">Workspace Name</label>
      <input
        id="ws-name"
        type="text"
        value={name}
        onChange={(e) => setName(e.target.value)}
        placeholder="my-workspace"
        required
      />

      <label htmlFor="ws-topic">Initial Topic</label>
      <input
        id="ws-topic"
        type="text"
        value={topic}
        onChange={(e) => setTopic(e.target.value)}
        placeholder="Optional"
      />

      <label htmlFor="ws-template">Template / Repo Label</label>
      <input
        id="ws-template"
        type="text"
        value={template}
        onChange={(e) => setTemplate(e.target.value)}
        placeholder="Optional"
      />

      {localTools.length > 0 && (
        <>
          <label>Local Tools</label>
          <LocalToolsPicker
            tools={localTools}
            selected={selectedTools}
            onToggle={toggleTool}
          />
        </>
      )}

      <label>MCP Tools</label>
      <label className="tool-card row" style={{ margin: 0, gap: 10 }}>
        <input
          type="checkbox"
          checked={jiraEnabled}
          onChange={(e) => setJiraEnabled(e.target.checked)}
        />
        <div>
          <div style={{ fontWeight: 500 }}>
            Register <code>hl7-jira</code> MCP tool
          </div>
          <div className="muted" style={{ fontSize: 12 }}>
            Writes a Bun MCP fixture into the workspace and registers it via the
            tools API.
          </div>
        </div>
      </label>

      <div className="row" style={{ marginTop: 14 }}>
        <button className="btn btn-primary" type="submit" disabled={busy}>
          {busy ? "Creating..." : "Create Workspace"}
        </button>
      </div>
      {status && (
        <p className="muted" style={{ marginTop: 6, fontSize: 12 }}>
          {status}
        </p>
      )}
    </form>
  );
}

async function registerDemoJiraTool(namespace: string, workspace: string) {
  const fixtureScript = await api.fetchDemoJiraScript();
  await api.writeFile(namespace, workspace, ".demo/hl7-jira-mcp.js", fixtureScript);

  try {
    await api.registerTool(namespace, workspace, {
      name: "hl7-jira",
      description: "Search realistic HL7 Jira fixture data",
      provider: "demo@acme.example",
      protocol: "mcp",
      transport: {
        type: "stdio",
        command: "bun",
        args: ["./.demo/hl7-jira-mcp.js"],
        cwd: ".",
      },
      tools: [
        {
          name: "jira.search",
          title: "Search HL7 Jira",
          description:
            "Search realistic HL7 Jira issues related to validation and FHIRPath behavior",
          inputSchema: {
            type: "object",
            properties: { query: { type: "string" } },
            required: ["query"],
            additionalProperties: false,
          },
        },
      ],
    });
  } catch (err) {
    if (!(err instanceof Error) || !err.message.includes("409")) throw err;
  }

  try {
    await api.grantTool(namespace, workspace, "hl7-jira", {
      subject: "agent:*",
      tools: ["jira.search"],
      access: "allowed",
      approvers: [],
      scope: {},
    });
  } catch (err) {
    if (!(err instanceof Error) || !err.message.includes("409")) throw err;
  }
}
