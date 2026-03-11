import { useEffect, useState } from "react";
import { Link, useParams, useLocation } from "wouter";
import { useStore } from "@/store";
import { AboutLink } from "@/components/AboutLink";
import { ParticipantNameInput } from "@/components/ParticipantNameInput";
import { WorkspaceFileBrowser } from "@/components/WorkspaceFileBrowser";
import type { WorkspaceDetail } from "@/api/types";
import * as api from "@/api/client";
import { registerDemoJiraTool } from "@/api/demo-tools";

export function WorkspacePage() {
  const params = useParams<{ namespace: string; workspace: string }>();
  const namespace = params.namespace ?? "";
  const workspace = params.workspace ?? "";
  const [, navigate] = useLocation();

  const storeWs = useStore((s) =>
    s.workspaces.find((w) => w.name === workspace),
  );
  const deleteTopic = useStore((s) => s.deleteTopic);
  const createTopic = useStore((s) => s.createTopic);

  const [detail, setDetail] = useState<WorkspaceDetail | null>(null);
  const [enabledTools, setEnabledTools] = useState<api.EnabledTool[]>([]);
  const [newTopic, setNewTopic] = useState("");
  const [busy, setBusy] = useState(false);
  const [toolStatus, setToolStatus] = useState("");

  const refreshTools = () =>
    api.listTools(namespace, workspace).then(setEnabledTools).catch(() => {});

  // Fetch workspace detail and registered tools on mount.
  useEffect(() => {
    api.getWorkspace(namespace, workspace).then(setDetail).catch(() => {});
    refreshTools();
  }, [namespace, workspace]);

  // Prefer store data when available (kept fresh by events WS).
  const ws = storeWs ?? detail;

  const topics = ws?.topics ?? [];

  const handleDeleteWorkspace = async () => {
    if (!confirm(`Delete workspace "${workspace}"?`)) return;
    await useStore.getState().deleteWorkspace(workspace);
    navigate("/");
  };

  const handleDeleteTopic = async (topicName: string) => {
    if (!confirm(`Delete topic "${topicName}"?`)) return;
    await deleteTopic(workspace, topicName);
  };

  const handleCreateTopic = async () => {
    const name = newTopic.trim();
    if (!name || busy) return;
    setBusy(true);
    try {
      await createTopic(workspace, name);
      setNewTopic("");
    } finally {
      setBusy(false);
    }
  };

  const handleRegisterJira = async () => {
    if (busy) return;
    setBusy(true);
    setToolStatus("Registering hl7-jira MCP tool...");
    try {
      await registerDemoJiraTool(namespace, workspace);
      await refreshTools();
      setToolStatus("hl7-jira registered.");
    } catch (err) {
      setToolStatus(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="page">
      {/* Header */}
      <div className="card">
        <div className="row row-between">
          <div className="row" style={{ gap: 6 }}>
            <Link
              href="/"
              style={{
                color: "var(--muted)",
                textDecoration: "none",
                fontSize: 13,
              }}
            >
              Workspaces
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>
              /
            </span>
            <span style={{ fontSize: 13, fontWeight: 500 }}>{workspace}</span>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <AboutLink />
            <ParticipantNameInput compact />
            {ws && (
              <>
                <span className="status-dot" data-status={ws.status} />
                <span className="muted" style={{ fontSize: 12 }}>
                  {ws.status}
                </span>
              </>
            )}
          </div>
        </div>
      </div>

      <div className="grid-2">
        {/* Workspace info + MCP tools */}
        <section className="card">
          <div className="row row-between" style={{ marginBottom: 8 }}>
            <h2 style={{ margin: 0 }}>Workspace</h2>
            <button
              className="btn btn-danger btn-sm"
              onClick={handleDeleteWorkspace}
            >
              Delete Workspace
            </button>
          </div>
          {ws?.createdAt && (
            <p className="muted" style={{ fontSize: 12, margin: "4px 0" }}>
              Created {new Date(ws.createdAt).toLocaleString()}
            </p>
          )}
          <h3 style={{ margin: "12px 0 6px", fontSize: 13 }}>Enabled Tools</h3>
          {enabledTools.length > 0 ? (
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {enabledTools.map((tool) => (
                <div key={tool.name} className="tool-card">
                  <div className="tool-card-copy">
                    <div className="tool-card-title">
                      {tool.name}
                      <span className="tool-card-meta" style={{ marginLeft: 8 }}>
                        {tool.kind}
                      </span>
                    </div>
                    {tool.description && (
                      <div className="tool-card-description">{tool.description}</div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p className="muted" style={{ fontSize: 12, margin: 0 }}>
              No tools enabled.
            </p>
          )}
          {!enabledTools.some((t) => t.name === "hl7-jira") && (
            <div style={{ marginTop: 8 }}>
              <button
                className="btn btn-secondary btn-sm"
                onClick={handleRegisterJira}
                disabled={busy}
              >
                Add hl7-jira
              </button>
            </div>
          )}
          {toolStatus && (
            <p className="muted" style={{ fontSize: 12, marginTop: 4 }}>
              {toolStatus}
            </p>
          )}
        </section>

        {/* Topics */}
        <section className="card">
          <h2 style={{ margin: "0 0 8px" }}>Topics</h2>
          {topics.length === 0 ? (
            <p className="muted" style={{ fontSize: 13, margin: 0 }}>
              No topics yet.
            </p>
          ) : (
            <div className="ws-list">
              {topics.map((t) => (
                <div key={t.name} className="topic-row">
                  <code style={{ fontSize: 12 }}>{t.name}</code>
                  <div className="row" style={{ gap: 4 }}>
                    <Link
                      href={`/app/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}/${encodeURIComponent(t.name)}`}
                      className="btn btn-primary btn-sm"
                    >
                      Open
                    </Link>
                    <button
                      className="btn btn-secondary btn-sm"
                      onClick={() => handleDeleteTopic(t.name)}
                    >
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
          <div className="row" style={{ marginTop: 8 }}>
            <input
              type="text"
              value={newTopic}
              onChange={(e) => setNewTopic(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleCreateTopic()}
              placeholder="New topic name"
              style={{ flex: 1, fontSize: 12, padding: "4px 8px" }}
            />
            <button
              className="btn btn-secondary btn-sm"
              onClick={handleCreateTopic}
              disabled={busy || !newTopic.trim()}
            >
              Add Topic
            </button>
          </div>
        </section>
      </div>

      <div style={{ marginTop: 16 }}>
        <WorkspaceFileBrowser
          namespace={namespace}
          workspace={workspace}
          browserId={`workspace-page:${namespace}:${workspace}`}
          title="Workspace Files"
        />
      </div>
    </div>
  );
}
