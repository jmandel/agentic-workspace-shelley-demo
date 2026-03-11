import { useState } from "react";
import { Link } from "wouter";
import { useStore } from "@/store";
import type { WorkspaceDetail } from "@/api/types";

interface Props {
  workspace: WorkspaceDetail;
}

export function WorkspaceCard({ workspace: ws }: Props) {
  const namespace = useStore((s) => s.namespace);
  const deleteWorkspace = useStore((s) => s.deleteWorkspace);
  const deleteTopic = useStore((s) => s.deleteTopic);
  const createTopic = useStore((s) => s.createTopic);
  const [newTopic, setNewTopic] = useState("");
  const [busy, setBusy] = useState(false);

  const ns = ws.namespace || namespace;
  const topics = ws.topics ?? [];
  const localToolNames =
    ws.runtime?.localTools
      ?.filter((t) => t.exposure !== "support_bundle")
      .map((t) => t.name)
      .join(", ") ?? "";

  const handleCreateTopic = async () => {
    const name = newTopic.trim();
    if (!name || busy) return;
    setBusy(true);
    try {
      await createTopic(ws.name, name);
      setNewTopic("");
    } finally {
      setBusy(false);
    }
  };

  const handleDeleteTopic = async (topicName: string) => {
    if (!confirm(`Delete topic "${topicName}"?`)) return;
    await deleteTopic(ws.name, topicName);
  };

  const handleDeleteWorkspace = async () => {
    if (!confirm(`Delete workspace "${ws.name}"?`)) return;
    await deleteWorkspace(ws.name);
  };

  return (
    <div className="workspace-card-body">
      <div className="workspace-card-header">
        <div className="stack-xs">
          <div className="workspace-card-title-row">
            <h3 style={{ margin: 0 }}>{ws.name}</h3>
            <span className="status-dot" data-status={ws.status} />
            <span className="muted" style={{ fontSize: 12 }}>
              {ws.status}
            </span>
          </div>
          {localToolNames && (
            <div className="muted" style={{ fontSize: 12 }}>
              Local tools: {localToolNames}
            </div>
          )}
        </div>
        <button className="btn btn-danger btn-sm" onClick={handleDeleteWorkspace}>
          Delete Workspace
        </button>
      </div>

      <div className="workspace-card-section">
        <div className="workspace-card-section-title">Topics</div>
        {topics.length > 0 ? (
          <div className="stack-xs">
            {topics.map((t) => (
              <div key={t.name} className="topic-row">
                <code style={{ fontSize: 12 }}>{t.name}</code>
                <div className="row" style={{ gap: 4 }}>
                  <Link
                    href={`/app/${encodeURIComponent(ns)}/${encodeURIComponent(ws.name)}/${encodeURIComponent(t.name)}`}
                    className="btn btn-primary btn-sm"
                  >
                    Open
                  </Link>
                  <a
                    href={`/shelley/${encodeURIComponent(ns)}/${encodeURIComponent(ws.name)}/${encodeURIComponent(t.name)}`}
                    className="btn btn-secondary btn-sm"
                  >
                    Shelley UI
                  </a>
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
        ) : (
          <p className="muted" style={{ fontSize: 12, margin: 0 }}>
            No topics yet.
          </p>
        )}
      </div>

      <div className="workspace-card-section">
        <div className="workspace-card-section-title">New Topic</div>
        <div className="row">
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
            Create Topic
          </button>
        </div>
      </div>
    </div>
  );
}
