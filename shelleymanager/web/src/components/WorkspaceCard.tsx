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
    ws.runtime?.localTools?.map((t) => t.name).join(", ") ?? "";

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
    <div>
      {/* Workspace header */}
      <div className="row row-between">
        <div className="row" style={{ gap: 6 }}>
          <span className="status-dot" data-status={ws.status} />
          <h3 style={{ margin: 0 }}>{ws.name}</h3>
          {localToolNames && (
            <span className="muted" style={{ fontSize: 12 }}>
              ({localToolNames})
            </span>
          )}
        </div>
        <button
          className="btn btn-danger btn-sm"
          onClick={handleDeleteWorkspace}
        >
          Delete
        </button>
      </div>

      {/* Topics */}
      {topics.length > 0 && (
        <div style={{ marginTop: 6 }}>
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

      {/* Add topic */}
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
    </div>
  );
}
