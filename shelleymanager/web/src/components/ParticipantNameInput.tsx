import { useState } from "react";
import { useStore } from "@/store";

export function ParticipantNameInput({
  compact,
  showLabel = true,
}: {
  compact?: boolean;
  showLabel?: boolean;
}) {
  const name = useStore((s) => s.participantName);
  const setName = useStore((s) => s.setParticipantName);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(name);

  const save = () => {
    setName(draft);
    setEditing(false);
  };

  const cancel = () => {
    setDraft(name);
    setEditing(false);
  };

  if (compact) {
    if (!editing) {
      return (
        <div className="row" style={{ gap: 6 }}>
          {showLabel && (
            <span className="muted" style={{ fontSize: 13 }}>
              Current participant:
            </span>
          )}
          <span style={{ fontSize: 13 }}>{name}</span>
          <button
            className="btn btn-secondary btn-sm"
            onClick={() => setEditing(true)}
          >
            Change
          </button>
        </div>
      );
    }
    return (
      <div className="row" style={{ gap: 6 }}>
        {showLabel && (
          <span className="muted" style={{ fontSize: 13 }}>
            Current participant:
          </span>
        )}
        <input
          type="text"
          className="input-inline"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") save();
            if (e.key === "Escape") cancel();
          }}
          autoFocus
          style={{ fontSize: 13, padding: "4px 8px" }}
        />
        <button className="btn btn-primary btn-sm" onClick={save}>
          Save
        </button>
        <button className="btn btn-secondary btn-sm" onClick={cancel}>
          Cancel
        </button>
      </div>
    );
  }

  if (!editing) {
    return (
      <div className="row" style={{ gap: 8 }}>
        <span className="muted" style={{ fontSize: 13 }}>Participant:</span>
        <span style={{ fontSize: 13, fontWeight: 500 }}>{name}</span>
        <button
          className="btn btn-secondary btn-sm"
          onClick={() => setEditing(true)}
        >
          Change
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="row" style={{ gap: 6 }}>
        <input
          type="text"
          className="input-inline"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") save();
            if (e.key === "Escape") cancel();
          }}
          autoFocus
          placeholder="Your name"
        />
        <button className="btn btn-primary btn-sm" onClick={save}>
          Save
        </button>
        <button className="btn btn-secondary btn-sm" onClick={cancel}>
          Cancel
        </button>
      </div>
    </div>
  );
}
