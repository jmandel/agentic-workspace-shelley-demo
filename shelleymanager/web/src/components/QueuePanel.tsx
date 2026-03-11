import { useState } from "react";
import { useStore } from "@/store";
import type { QueueEntry } from "@/api/types";
import * as api from "@/api/client";

export function QueuePanel() {
  const conn = useStore((s) => s.topicConnection);
  const clientId = useStore((s) => s.participantName);
  const queue = useStore((s) => s.queue);
  const turnActive = useStore((s) => s.turnActive);
  const connectionStatus = useStore((s) => s.connectionStatus);

  if (!conn || queue.entries.length === 0) return null;

  const { namespace, workspace, topic } = conn;
  const queuedCount = queue.entries.length;
  const connected = connectionStatus === "connected";

  return (
    <div>
      <div className="row row-between" style={{ marginBottom: 6 }}>
        <h3 style={{ margin: 0 }}>
          Prompt Queue{" "}
          <span className="muted" style={{ fontWeight: 400, fontSize: 12 }}>
            {queuedCount} pending
          </span>
        </h3>
      </div>
      {queue.activePromptId && (
        <div className="queue-active">
          <span className="muted" style={{ fontSize: 12 }}>
            Active prompt {queue.activePromptId}
          </span>
        </div>
      )}

      <div className="stack-sm">
        {queue.entries.map((entry) => (
          <QueueEntryCard
            key={entry.promptId}
            entry={entry}
            isOwn={entry.submittedBy?.id === clientId}
            isFirst={entry.position === 1}
            isLast={entry.position === queuedCount}
            turnActive={turnActive}
            connected={connected}
            namespace={namespace}
            workspace={workspace}
            topic={topic}
            clientId={clientId}
          />
        ))}
      </div>
    </div>
  );
}

function QueueEntryCard({
  entry,
  isOwn,
  isFirst,
  isLast,
  turnActive,
  connected,
  namespace,
  workspace,
  topic,
  clientId,
}: {
  entry: QueueEntry;
  isOwn: boolean;
  isFirst: boolean;
  isLast: boolean;
  turnActive: boolean;
  connected: boolean;
  namespace: string;
  workspace: string;
  topic: string;
  clientId: string;
}) {
  const refreshQueue = useStore((s) => s.refreshQueue);
  const injectFromQueue = useStore((s) => s.injectFromQueue);
  const [editText, setEditText] = useState(entry.text);
  const [saving, setSaving] = useState(false);
  const owner = entry.submittedBy?.id ?? "unknown";

  const save = async () => {
    if (saving || !editText.trim()) return;
    setSaving(true);
    try {
      await api.updateQueuedPrompt(
        namespace, workspace, topic, entry.promptId, editText.trim(), clientId,
      );
      refreshQueue();
    } finally {
      setSaving(false);
    }
  };

  const move = async (direction: "up" | "down" | "top" | "bottom") => {
    await api.moveQueuedPrompt(
      namespace, workspace, topic, entry.promptId, direction, clientId,
    );
    refreshQueue();
  };

  const cancel = async () => {
    await api.cancelQueuedPrompt(
      namespace, workspace, topic, entry.promptId, clientId,
    );
    refreshQueue();
  };

  return (
    <div className={`queue-entry ${isOwn ? "queue-entry-own" : ""}`}>
      <div className="row row-between" style={{ marginBottom: 4 }}>
        <span className="muted" style={{ fontSize: 11 }}>
          #{entry.position} {entry.status} by {owner}
        </span>
        <span className="mono muted">{entry.promptId}</span>
      </div>
      <textarea
        value={editText}
        onChange={(e) => setEditText(e.target.value)}
        style={{ minHeight: 48, fontSize: 12 }}
      />
      <div className="row row-end" style={{ marginTop: 4 }}>
        {turnActive && (
          <button
            className="btn btn-primary btn-sm"
            onClick={() => injectFromQueue(entry.promptId)}
            disabled={!connected}
            title="Cancel this entry and inject its text into the active turn"
          >
            Inject
          </button>
        )}
        <button
          className="btn btn-secondary btn-sm"
          onClick={save}
          disabled={saving || editText.trim() === entry.text}
        >
          Save
        </button>
        <button className="btn btn-secondary btn-sm" onClick={() => move("top")} disabled={isFirst}>
          Top
        </button>
        <button className="btn btn-secondary btn-sm" onClick={() => move("up")} disabled={isFirst}>
          Up
        </button>
        <button className="btn btn-secondary btn-sm" onClick={() => move("down")} disabled={isLast}>
          Down
        </button>
        <button className="btn btn-secondary btn-sm" onClick={() => move("bottom")} disabled={isLast}>
          Bottom
        </button>
        <button className="btn btn-danger btn-sm" onClick={cancel}>
          Delete
        </button>
      </div>
    </div>
  );
}
