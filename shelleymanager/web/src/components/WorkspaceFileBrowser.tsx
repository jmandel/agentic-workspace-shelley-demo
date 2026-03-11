import { useEffect, useRef, type ChangeEvent } from "react";
import { useStore, type WorkspaceFileBrowserState } from "@/store";
import type { WorkspaceFileNode } from "@/api/types";
import {
  formatWorkspaceFileSize,
  parentWorkspacePath,
  workspacePathSegments,
} from "./workspaceFileBrowserModel";

interface Props {
  namespace: string;
  workspace: string;
  browserId?: string;
  initialPath?: string;
  title?: string;
  className?: string;
}

const EMPTY_BROWSER: WorkspaceFileBrowserState = {
  namespace: "",
  workspace: "",
  currentPath: "",
  listing: null,
  loading: false,
  error: "",
  selectedFilePath: "",
  previewKind: null,
  previewText: "",
  previewMimeType: "",
  previewLoading: false,
  previewError: "",
  uploadBusy: false,
  uploadError: "",
};

function selectedNode(
  browser: WorkspaceFileBrowserState,
): WorkspaceFileNode | null {
  return (
    browser.listing?.entries?.find(
      (entry) => entry.path === browser.selectedFilePath,
    ) ?? null
  );
}

export function WorkspaceFileBrowser({
  namespace,
  workspace,
  browserId = `workspace:${namespace}:${workspace}`,
  initialPath = "",
  title = "Files",
  className = "",
}: Props) {
  const browser = useStore(
    (s) => s.fileBrowsers[browserId] ?? EMPTY_BROWSER,
  );
  const ensureWorkspaceFileBrowser = useStore(
    (s) => s.ensureWorkspaceFileBrowser,
  );
  const browseWorkspaceDirectory = useStore((s) => s.browseWorkspaceDirectory);
  const previewWorkspaceFile = useStore((s) => s.previewWorkspaceFile);
  const refreshWorkspaceFileBrowser = useStore(
    (s) => s.refreshWorkspaceFileBrowser,
  );
  const uploadWorkspaceFiles = useStore((s) => s.uploadWorkspaceFiles);
  const uploadInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    void ensureWorkspaceFileBrowser(browserId, namespace, workspace, initialPath);
  }, [
    browserId,
    ensureWorkspaceFileBrowser,
    initialPath,
    namespace,
    workspace,
  ]);

  const entries = browser.listing?.entries ?? [];
  const activeNode = selectedNode(browser);
  const segments = workspacePathSegments(browser.currentPath);

  const handleOpen = (entry: WorkspaceFileNode) => {
    if (entry.kind === "directory") {
      void browseWorkspaceDirectory(browserId, entry.path);
      return;
    }
    void previewWorkspaceFile(browserId, entry);
  };

  const handleUpload = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.currentTarget.files ?? []);
    if (files.length === 0) return;
    void uploadWorkspaceFiles(browserId, files);
    event.currentTarget.value = "";
  };

  return (
    <section className={`workspace-file-browser ${className}`.trim()}>
      <div className="workspace-file-browser-header">
        <div className="workspace-file-browser-heading">
          <h2 style={{ margin: 0 }}>{title}</h2>
          <div className="workspace-file-browser-path">
            <button
              type="button"
              className="workspace-file-browser-crumb"
              onClick={() => void browseWorkspaceDirectory(browserId, "")}
            >
              Root
            </button>
            {segments.map((segment, index) => {
              const path = segments.slice(0, index + 1).join("/");
              return (
                <span key={path} className="workspace-file-browser-crumb-wrap">
                  <span className="muted">/</span>
                  <button
                    type="button"
                    className="workspace-file-browser-crumb"
                    onClick={() => void browseWorkspaceDirectory(browserId, path)}
                  >
                    {segment}
                  </button>
                </span>
              );
            })}
          </div>
        </div>
        <div className="row">
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={!browser.currentPath || browser.loading}
            onClick={() =>
              void browseWorkspaceDirectory(
                browserId,
                parentWorkspacePath(browser.currentPath),
              )
            }
          >
            Up
          </button>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={browser.loading}
            onClick={() => void refreshWorkspaceFileBrowser(browserId)}
          >
            Refresh
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={browser.uploadBusy}
            onClick={() => uploadInputRef.current?.click()}
          >
            {browser.uploadBusy ? "Uploading..." : "Upload"}
          </button>
          <input
            ref={uploadInputRef}
            type="file"
            className="workspace-file-browser-input"
            onChange={handleUpload}
          />
        </div>
      </div>

      {browser.uploadError && (
        <div className="workspace-file-browser-banner workspace-file-browser-banner-error">
          {browser.uploadError}
        </div>
      )}
      {browser.error && (
        <div className="workspace-file-browser-banner workspace-file-browser-banner-error">
          {browser.error}
        </div>
      )}

      <div className="workspace-file-browser-body">
        <div className="workspace-file-browser-pane">
          <div className="workspace-file-browser-pane-head">
            <span className="workspace-file-browser-pane-title">Explorer</span>
            <span className="muted" style={{ fontSize: 12 }}>
              {browser.currentPath || "root"}
            </span>
          </div>
          {browser.loading ? (
            <p className="muted" style={{ margin: 0 }}>
              Loading files...
            </p>
          ) : entries.length === 0 ? (
            <p className="muted" style={{ margin: 0 }}>
              This directory is empty.
            </p>
          ) : (
            <div className="workspace-file-browser-list">
              {entries.map((entry) => {
                const isSelected = entry.path === browser.selectedFilePath;
                return (
                  <button
                    key={entry.path}
                    type="button"
                    className={`workspace-file-browser-entry${isSelected ? " is-selected" : ""}`}
                    onClick={() => handleOpen(entry)}
                  >
                    <div className="workspace-file-browser-entry-main">
                      <span className="workspace-file-browser-entry-icon">
                        {entry.kind === "directory" ? "DIR" : "FILE"}
                      </span>
                      <span className="workspace-file-browser-entry-name">
                        {entry.name}
                      </span>
                    </div>
                    <div className="workspace-file-browser-entry-meta">
                      <span>{formatWorkspaceFileSize(entry.size)}</span>
                      <span>{new Date(entry.modifiedAt).toLocaleString()}</span>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </div>

        <div className="workspace-file-browser-pane">
          <div className="workspace-file-browser-pane-head">
            <span className="workspace-file-browser-pane-title">Preview</span>
            <span className="muted" style={{ fontSize: 12 }}>
              {activeNode?.path || "No file selected"}
            </span>
          </div>

          {!activeNode ? (
            <p className="muted" style={{ margin: 0 }}>
              Select a file to preview it.
            </p>
          ) : browser.previewLoading ? (
            <p className="muted" style={{ margin: 0 }}>
              Loading preview...
            </p>
          ) : browser.previewError ? (
            <div className="workspace-file-browser-banner workspace-file-browser-banner-error">
              {browser.previewError}
            </div>
          ) : browser.previewKind === "binary" ? (
            <div className="stack-sm">
              <p className="muted" style={{ margin: 0 }}>
                Preview is not available for this file type.
              </p>
              <div className="workspace-file-browser-meta-grid">
                <span>Type</span>
                <span>{activeNode.mimeType || "binary"}</span>
                <span>Size</span>
                <span>{formatWorkspaceFileSize(activeNode.size)}</span>
              </div>
            </div>
          ) : browser.previewKind === "too_large" ? (
            <div className="stack-sm">
              <p className="muted" style={{ margin: 0 }}>
                Preview skipped because this file is too large.
              </p>
              <div className="workspace-file-browser-meta-grid">
                <span>Type</span>
                <span>{activeNode.mimeType || "text"}</span>
                <span>Size</span>
                <span>{formatWorkspaceFileSize(activeNode.size)}</span>
              </div>
            </div>
          ) : (
            <pre className="workspace-file-browser-preview">
              <code>{browser.previewText}</code>
            </pre>
          )}
        </div>
      </div>
    </section>
  );
}
