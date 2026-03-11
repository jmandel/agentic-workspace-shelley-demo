import { useState } from "react";
import { useLocation } from "wouter";
import { useStore } from "@/store";
import * as api from "@/api/client";
import { registerDemoJiraTool } from "@/api/demo-tools";
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
      const runtimeLocalTools = [...selectedTools];
      if (jiraEnabled && !runtimeLocalTools.includes("hl7-jira-support")) {
        runtimeLocalTools.push("hl7-jira-support");
      }
      await api.createWorkspace(namespace, {
        name,
        template: template || undefined,
        topics: topic ? [{ name: topic }] : undefined,
        runtime: { localTools: runtimeLocalTools.sort() },
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

      <label htmlFor="ws-template">Workspace Template</label>
      <input
        id="ws-template"
        type="text"
        value={template}
        onChange={(e) => setTemplate(e.target.value)}
        placeholder="Optional built-in starter"
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
      <div className="tool-card tool-card-choice">
        <div className="tool-card-head">
          <input
            id="mcp-tool-hl7-jira"
            className="tool-card-input"
            type="checkbox"
            checked={jiraEnabled}
            onChange={(e) => setJiraEnabled(e.target.checked)}
          />
          <label htmlFor="mcp-tool-hl7-jira" className="tool-card-title">
            hl7-jira
          </label>
        </div>
        <div className="tool-card-copy">
          <div className="tool-card-description">
            Provides access to HL7 Jira issues from the real SQLite snapshot.
          </div>
        </div>
      </div>

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
