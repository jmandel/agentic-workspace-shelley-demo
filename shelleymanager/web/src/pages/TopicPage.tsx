import { useEffect, useState, useRef } from "react";
import { Link, useParams, useLocation } from "wouter";
import { useStore } from "@/store";
import { MessageList } from "@/components/MessageList";
import { QueuePanel } from "@/components/QueuePanel";
import { ParticipantNameInput } from "@/components/ParticipantNameInput";
import * as api from "@/api/client";

const CLI_REPO_URL =
  "https://github.com/agentic-workspaces/agentic-workspace.git";
const CLI_REPO_DIR = "agentic-workspace/reference-impl";

function escapeHTML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function buildCLIHelperHTML(args: {
  managerOrigin: string;
  workspace: string;
  topic: string;
}) {
  const connectCommand = `WS_MANAGER=${args.managerOrigin} bun run cli.ts connect ${args.workspace} ${args.topic}`;
  const fullSetupCommand = [
    `git clone ${CLI_REPO_URL}`,
    `cd ${CLI_REPO_DIR}`,
    "bun install",
    connectCommand,
  ].join("\n");

  return `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Open In CLI</title>
    <style>
      :root {
        color-scheme: light;
        --bg: #f6f7f9;
        --card: #ffffff;
        --ink: #111827;
        --muted: #6b7280;
        --line: #e5e7eb;
        --accent: #111827;
        --accent-ink: #ffffff;
      }
      * { box-sizing: border-box; }
      body {
        margin: 0;
        padding: 32px 20px;
        background: var(--bg);
        color: var(--ink);
        font: 15px/1.5 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }
      main {
        max-width: 760px;
        margin: 0 auto;
      }
      .card {
        background: var(--card);
        border: 1px solid var(--line);
        border-radius: 14px;
        padding: 20px;
        box-shadow: 0 1px 2px rgba(0, 0, 0, 0.04);
      }
      h1, h2 {
        margin: 0 0 10px;
        font-weight: 600;
        letter-spacing: -0.01em;
      }
      h1 { font-size: 26px; }
      h2 { font-size: 15px; margin-top: 20px; }
      p {
        margin: 0 0 12px;
        color: var(--muted);
      }
      .meta {
        margin-bottom: 18px;
        color: var(--muted);
        font-size: 13px;
      }
      .actions {
        display: flex;
        flex-wrap: wrap;
        gap: 8px;
        margin: 16px 0 18px;
      }
      button {
        appearance: none;
        border: 1px solid var(--line);
        border-radius: 9px;
        padding: 8px 12px;
        background: #fff;
        color: var(--ink);
        font: inherit;
        font-size: 13px;
        cursor: pointer;
      }
      button.primary {
        background: var(--accent);
        border-color: var(--accent);
        color: var(--accent-ink);
      }
      pre {
        margin: 0;
        padding: 12px 14px;
        overflow: auto;
        white-space: pre-wrap;
        border-radius: 10px;
        background: #f3f4f6;
        border: 1px solid var(--line);
        font: 12px/1.45 ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace;
      }
      .status {
        min-height: 20px;
        color: var(--muted);
        font-size: 13px;
      }
    </style>
  </head>
  <body>
    <main>
      <div class="card">
        <h1>Open In CLI</h1>
        <div class="meta">Workspace: ${escapeHTML(args.workspace)} | Topic: ${escapeHTML(args.topic)}</div>
        <p>Clone the upstream reference implementation, enter the CLI directory, and connect to this topic.</p>
        <div class="actions">
          <button class="primary" data-copy-target="setup">Copy Full Setup</button>
          <button data-copy-target="connect">Copy Connect Command</button>
        </div>
        <div class="status" id="status"></div>

        <h2>Full Setup</h2>
        <pre id="setup">${escapeHTML(fullSetupCommand)}</pre>

        <h2>Connect Only</h2>
        <pre id="connect">${escapeHTML(connectCommand)}</pre>
      </div>
    </main>
    <script>
      const status = document.getElementById("status");
      async function copyText(text) {
        if (navigator.clipboard && window.isSecureContext) {
          await navigator.clipboard.writeText(text);
          return;
        }
        const helper = document.createElement("textarea");
        helper.value = text;
        helper.setAttribute("readonly", "");
        helper.style.position = "absolute";
        helper.style.left = "-9999px";
        document.body.appendChild(helper);
        helper.select();
        document.execCommand("copy");
        document.body.removeChild(helper);
      }
      document.querySelectorAll("[data-copy-target]").forEach((button) => {
        button.addEventListener("click", async () => {
          const id = button.getAttribute("data-copy-target");
          const target = document.getElementById(id);
          if (!target) return;
          try {
            await copyText(target.textContent || "");
            status.textContent = id === "setup" ? "Copied full setup." : "Copied connect command.";
          } catch (error) {
            status.textContent = "Copy failed. Select the text manually.";
          }
        });
      });
    </script>
  </body>
</html>`;
}

export function TopicPage() {
  const params = useParams<{
    namespace: string;
    workspace: string;
    topic: string;
  }>();
  const namespace = params.namespace ?? "";
  const workspace = params.workspace ?? "";
  const topic = params.topic ?? "";
  const [, navigate] = useLocation();

  const connectionStatus = useStore((s) => s.connectionStatus);
  const turnActive = useStore((s) => s.turnActive);
  const messages = useStore((s) => s.messages);
  const queue = useStore((s) => s.queue);
  const sendPrompt = useStore((s) => s.sendPrompt);
  const sendInterrupt = useStore((s) => s.sendInterrupt);
  const workspaces = useStore((s) => s.workspaces);
  const fetchWorkspaceDetail = useStore((s) => s.fetchWorkspaceDetail);

  const [prompt, setPrompt] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    const { connectTopic, disconnectTopic } = useStore.getState();
    connectTopic(namespace, workspace, topic);
    return () => disconnectTopic();
  }, [namespace, workspace, topic]);

  useEffect(() => {
    if (!workspace) return;
    void fetchWorkspaceDetail(workspace, namespace);
  }, [fetchWorkspaceDetail, namespace, workspace]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const text = prompt.trim();
    if (!text || connectionStatus !== "connected") return;
    sendPrompt(text);
    setPrompt("");
    textareaRef.current?.focus();
  };

  const handleDeleteTopic = async () => {
    if (!confirm(`Delete topic "${topic}"?`)) return;
    await api.deleteTopic(namespace, workspace, topic);
    navigate("/");
  };

  const workspaceDetail = workspaces.find(
    (ws) => ws.namespace === namespace && ws.name === workspace,
  );
  const topicRef = workspaceDetail?.topics?.find((item) => item.name === topic);
  const shelleyHref = topicRef?.shelley;

  const handleOpenCLI = () => {
    const html = buildCLIHelperHTML({
      managerOrigin: window.location.origin,
      workspace,
      topic,
    });
    const blob = new Blob([html], { type: "text/html" });
    const blobURL = URL.createObjectURL(blob);
    const popup = window.open(blobURL, "_blank", "noopener,noreferrer");
    if (popup === null) {
      URL.revokeObjectURL(blobURL);
      return;
    }
    window.setTimeout(() => URL.revokeObjectURL(blobURL), 60_000);
  };

  return (
    <div className="page-topic">
      {/* Header */}
      <div className="card" style={{ flexShrink: 0 }}>
        <div className="row row-between" style={{ gap: 12 }}>
          <div className="topic-breadcrumbs">
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
            <Link
              href={`/app/${encodeURIComponent(namespace)}/${encodeURIComponent(workspace)}`}
              style={{
                fontSize: 13,
                fontWeight: 500,
                textDecoration: "none",
                color: "inherit",
              }}
            >
              {workspace}
            </Link>
            <span className="muted" style={{ fontSize: 13 }}>
              /
            </span>
            <span className="topic-breadcrumb-current">{topic}</span>
          </div>
          <div className="row" style={{ gap: 6 }}>
            <span
              className="status-dot"
              data-status={connectionStatus}
              title={connectionStatus}
            />
            <span className="muted" style={{ fontSize: 12 }}>
              {connectionStatus}
            </span>
          </div>
        </div>
        <div className="topic-header-body">
          <div className="topic-controls">
            <section className="topic-control-block">
              <div className="topic-toolbar-label">Participant</div>
              <ParticipantNameInput compact showLabel={false} />
            </section>

            <section className="topic-control-block">
              <div className="topic-toolbar-label">Topic Actions</div>
              <div className="row" style={{ gap: 6 }}>
                <Link href="/ws-language" className="btn btn-secondary btn-sm">
                  WS Reference
                </Link>
                {shelleyHref && (
                  <a
                    href={shelleyHref}
                    className="btn btn-secondary btn-sm"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    Open in Shelley
                  </a>
                )}
                <button
                  className="btn btn-secondary btn-sm"
                  type="button"
                  onClick={handleOpenCLI}
                >
                  Open in CLI
                </button>
                <button
                  className="btn btn-danger btn-sm"
                  onClick={handleDeleteTopic}
                >
                  Delete Topic
                </button>
              </div>
            </section>
          </div>
        </div>
      </div>

      {/* Messages — fills remaining space, scrolls */}
      <div className="card messages-scroll" style={{ padding: 12 }}>
        <MessageList messages={messages} />
      </div>

      {/* Active turn status bar */}
      {turnActive && (
        <div className="row row-between turn-bar" style={{ flexShrink: 0 }}>
          <span className="muted" style={{ fontSize: 13 }}>
            <span
              className="status-dot"
              data-status="running"
              style={{ marginRight: 6 }}
            />
            Agent is working
            {queue.activePromptId ? ` (${queue.activePromptId})` : ""}
          </span>
          <button
            className="btn-stop btn-sm"
            onClick={sendInterrupt}
            disabled={connectionStatus !== "connected"}
          >
            Stop
          </button>
        </div>
      )}

      {/* Queue (only shown when entries exist) */}
      {queue.entries.length > 0 && (
        <div className="card" style={{ flexShrink: 0 }}>
          <QueuePanel />
        </div>
      )}

      {/* Composer — pinned at bottom */}
      <div className="card" style={{ flexShrink: 0 }}>
        <form onSubmit={handleSubmit}>
          <textarea
            ref={textareaRef}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSubmit(e);
              }
            }}
            placeholder="Enter a prompt..."
            style={{ minHeight: 48 }}
          />
          <div className="row" style={{ marginTop: 6, gap: 6 }}>
            <button
              className="btn btn-primary"
              type="submit"
              disabled={connectionStatus !== "connected" || !prompt.trim()}
            >
              Send
            </button>
            {connectionStatus !== "connected" && (
              <span className="muted" style={{ fontSize: 12 }}>
                {connectionStatus === "connecting"
                  ? "Connecting..."
                  : "Disconnected"}
              </span>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}
