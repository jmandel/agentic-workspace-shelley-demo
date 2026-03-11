import { useStore } from "@/store";

export function SettingsModal() {
  const settingsOpen = useStore((s) => s.settingsOpen);
  const participantName = useStore((s) => s.participantName);
  const participantSubject = useStore((s) => s.participantSubject);
  const settingsDraftName = useStore((s) => s.settingsDraftName);
  const closeSettings = useStore((s) => s.closeSettings);
  const setSettingsDraftName = useStore((s) => s.setSettingsDraftName);
  const randomizeSettingsDraftName = useStore((s) => s.randomizeSettingsDraftName);
  const saveSettings = useStore((s) => s.saveSettings);

  if (!settingsOpen) {
    return null;
  }

  return (
    <div
      className="settings-modal-backdrop"
      onClick={(event) => {
        if (event.target === event.currentTarget) {
          closeSettings();
        }
      }}
    >
      <div className="settings-modal card">
        <div className="row row-between" style={{ gap: 12 }}>
          <div>
            <h2 style={{ margin: 0 }}>Settings</h2>
            <p className="muted" style={{ margin: "6px 0 0" }}>
              This name is used for browser participation across the manager and
              topic views.
            </p>
          </div>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={closeSettings}
          >
            Close
          </button>
        </div>

        <div className="stack-sm" style={{ marginTop: 16 }}>
          <div className="settings-modal-meta">
            <span className="settings-modal-label">Current name</span>
            <span>{participantName}</span>
            <span className="settings-modal-label">Participant id</span>
            <code>{participantSubject}</code>
          </div>

          <div>
            <label htmlFor="settings-name-input">Display Name</label>
            <input
              id="settings-name-input"
              type="text"
              value={settingsDraftName}
              onChange={(event) => setSettingsDraftName(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  saveSettings();
                }
              }}
              autoFocus
              placeholder="Display name"
            />
          </div>

          <div className="row" style={{ gap: 6 }}>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={randomizeSettingsDraftName}
            >
              Pick Another
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => setSettingsDraftName(participantName)}
            >
              Revert
            </button>
          </div>
        </div>

        <div className="row row-end" style={{ marginTop: 16, gap: 6 }}>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={closeSettings}
          >
            Cancel
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            onClick={saveSettings}
          >
            Save
          </button>
        </div>
      </div>
    </div>
  );
}
